package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	vole "github.com/ipfs-shipyard/vole/lib"
	bsmsg "github.com/ipfs/boxo/bitswap/message"
	"github.com/ipfs/boxo/bitswap/message/pb"
	"github.com/ipfs/boxo/bitswap/network"
	"github.com/ipfs/boxo/bitswap/network/httpnet"
	"github.com/ipfs/boxo/ipns"
	"github.com/ipfs/boxo/routing/http/client"
	"github.com/ipfs/boxo/routing/http/contentrouter"
	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p-kad-dht/fullrt"
	dhtpb "github.com/libp2p/go-libp2p-kad-dht/pb"
	record "github.com/libp2p/go-libp2p-record"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/core/routing"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/prometheus/client_golang/prometheus"
)

type kademlia interface {
	routing.Routing
	GetClosestPeers(ctx context.Context, key string) ([]peer.ID, error)
}

type daemon struct {
	h              host.Host
	dht            kademlia
	dhtMessenger   *dhtpb.ProtocolMessenger
	createTestHost func() (host.Host, error)
	promRegistry   *prometheus.Registry
	httpSkipVerify bool
}

const (
	// number of providers at which to stop looking for providers in the DHT
	// When doing a check only with a CID
	maxProvidersCount = 10

	ipniSource = "IPNI"
	dhtSource  = "Amino DHT"
)

var defaultProtocolFilter = []string{"transport-bitswap", "unknown"}

func newDaemon(ctx context.Context, acceleratedDHT bool) (*daemon, error) {
	rm, err := NewResourceManager()
	if err != nil {
		return nil, err
	}

	c, err := connmgr.NewConnManager(100, 900, connmgr.WithGracePeriod(time.Second*30))
	if err != nil {
		return nil, err
	}

	// Create a custom registry for all prometheus metrics
	promRegistry := prometheus.NewRegistry()

	h, err := libp2p.New(
		libp2p.ConnectionManager(c),
		libp2p.ConnectionGater(&privateAddrFilterConnectionGater{}),
		libp2p.ResourceManager(rm),
		libp2p.EnableHolePunching(),
		libp2p.PrometheusRegisterer(promRegistry),
		libp2p.UserAgent(userAgent),
	)
	if err != nil {
		return nil, err
	}

	var d kademlia
	if acceleratedDHT {
		d, err = fullrt.NewFullRT(h, "/ipfs",
			fullrt.DHTOption(
				dht.BucketSize(20),
				dht.Validator(record.NamespacedValidator{
					"pk":   record.PublicKeyValidator{},
					"ipns": ipns.Validator{},
				}),
				dht.BootstrapPeers(dht.GetDefaultBootstrapPeerAddrInfos()...),
				dht.Mode(dht.ModeClient),
			))

	} else {
		d, err = dht.New(ctx, h, dht.Mode(dht.ModeClient), dht.BootstrapPeers(dht.GetDefaultBootstrapPeerAddrInfos()...))
	}

	if err != nil {
		return nil, err
	}

	pm, err := dhtProtocolMessenger("/ipfs/kad/1.0.0", h)
	if err != nil {
		return nil, err
	}

	return &daemon{
		h:            h,
		dht:          d,
		dhtMessenger: pm,
		promRegistry: promRegistry,
		createTestHost: func() (host.Host, error) {
			// TODO: when behind NAT, this will fail to determine its own public addresses which will block it from running dctur and hole punching
			// See https://github.com/libp2p/go-libp2p/issues/2941
			return libp2p.New(
				libp2p.ConnectionGater(&privateAddrFilterConnectionGater{}),
				libp2p.EnableHolePunching(),
				libp2p.UserAgent(userAgent),
			)
		}}, nil
}

func (d *daemon) mustStart() {
	// Wait for the DHT to be ready
	if frt, ok := d.dht.(*fullrt.FullRT); ok {
		if !frt.Ready() {
			log.Printf("Please wait, initializing accelerated-dht client.. (mapping Amino DHT takes 5 mins or more)")
		}
		for !frt.Ready() {
			time.Sleep(time.Second * 1)
		}
		log.Printf("Accelerated DHT client is ready")
	}
}

type cidCheckOutput *[]providerOutput

type providerOutput struct {
	ID                       string
	ConnectionError          string
	Addrs                    []string
	ConnectionMaddrs         []string
	DataAvailableOverBitswap BitswapCheckOutput
	DataAvailableOverHTTP    HTTPCheckOutput
	Source                   string
}

// runCidCheck finds providers of a given CID, using the DHT and IPNI
// concurrently. A check of connectivity and Bitswap availability is performed
// for each provider found.
func (d *daemon) runCidCheck(ctx context.Context, cidKey cid.Cid, ipniURL string, httpRetrieval bool) (cidCheckOutput, error) {
	protocols := defaultProtocolFilter
	if httpRetrieval {
		protocols = append(protocols, "transport-ipfs-gateway-http")
	}

	crClient, err := client.New(ipniURL,
		client.WithStreamResultsRequired(),       // // https://specs.ipfs.tech/routing/http-routing-v1/#streaming
		client.WithProtocolFilter(protocols),     // IPIP-484
		client.WithDisabledLocalFiltering(false), // force local filtering in case remote server does not support IPIP-484
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create content router client: %w", err)
	}
	routerClient := contentrouter.NewContentRoutingClient(crClient)

	queryCtx, cancelQuery := context.WithCancel(ctx)
	defer cancelQuery()

	// half of the max providers count per source
	providersPerSource := maxProvidersCount >> 1
	if maxProvidersCount == 1 {
		// Ensure at least one provider from each source when maxProvidersCount is 1
		providersPerSource = 1
	}

	// Find providers with DHT and IPNI concurrently (each half of the max providers count)
	dhtProvsCh := d.dht.FindProvidersAsync(queryCtx, cidKey, providersPerSource)
	ipniProvsCh := routerClient.FindProvidersAsync(queryCtx, cidKey, providersPerSource)

	out := make([]providerOutput, 0, maxProvidersCount)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var providersCount int
	var done bool

	for !done {
		var provider peer.AddrInfo
		var open bool
		var source string

		select {
		case provider, open = <-dhtProvsCh:
			if !open {
				dhtProvsCh = nil
				if ipniProvsCh == nil {
					done = true
				}
				continue
			}
			source = dhtSource
		case provider, open = <-ipniProvsCh:
			if !open {
				ipniProvsCh = nil
				if dhtProvsCh == nil {
					done = true
				}
				continue
			}
			source = ipniSource
		}
		providersCount++
		if providersCount == maxProvidersCount {
			done = true
		}

		wg.Add(1)
		go func(provider peer.AddrInfo, src string) {
			defer wg.Done()

			provOutput := providerOutput{
				ID:                       provider.ID.String(),
				Source:                   src,
				DataAvailableOverBitswap: BitswapCheckOutput{},
				DataAvailableOverHTTP:    HTTPCheckOutput{},
			}

			testHost, err := d.createTestHost()
			if err != nil {
				log.Printf("Error creating test host: %v\n", err)
				return
			}
			defer testHost.Close()

			// Get http retrieval out of the way if this is such
			// provider.
			httpInfo, libp2pInfo := network.SplitHTTPAddrs(provider)
			if len(httpInfo.Addrs) > 0 && httpRetrieval {
				provOutput.DataAvailableOverHTTP.Enabled = true

				for _, ma := range httpInfo.Addrs {
					provOutput.Addrs = append(provOutput.Addrs, ma.String())
				}

				testHost, err := d.createTestHost()
				if err != nil {
					log.Printf("Error creating test host: %v\n", err)
					return
				}
				defer testHost.Close()
				httpCheck := checkHTTPRetrieval(ctx, testHost, cidKey, httpInfo, d.httpSkipVerify)
				provOutput.DataAvailableOverHTTP = httpCheck
				if !httpCheck.Connected {
					provOutput.ConnectionError = httpCheck.Error
				}
				for _, ma := range httpCheck.Endpoints {
					provOutput.ConnectionMaddrs = append(provOutput.ConnectionMaddrs, ma.String())
				}

				// Do not continue processing if there are no
				// other addresses as we would trigger dht
				// lookups etc.
				if len(libp2pInfo.Addrs) == 0 {
					provOutput.DataAvailableOverBitswap.Enabled = false
					mu.Lock()
					out = append(out, provOutput)
					mu.Unlock()

					return
				}
			}

			// process non-http providers addresses.
			provider = libp2pInfo

			outputAddrs := []string{}
			if len(provider.Addrs) > 0 {
				for _, addr := range provider.Addrs {
					if manet.IsPublicAddr(addr) { // only return public addrs
						outputAddrs = append(outputAddrs, addr.String())
					}
				}
			} else {
				// If no maddrs were returned from the FindProvider rpc call, try to get them from the DHT
				peerAddrs, err := d.dht.FindPeer(ctx, provider.ID)
				if err == nil {
					for _, addr := range peerAddrs.Addrs {
						if manet.IsPublicAddr(addr) { // only return public addrs
							// Add to both output and to provider addrs for the check
							outputAddrs = append(outputAddrs, addr.String())
							provider.Addrs = append(provider.Addrs, addr)
						}
					}
				}
			}

			provOutput.Addrs = append(provOutput.Addrs, outputAddrs...)
			provOutput.DataAvailableOverBitswap.Enabled = true

			// Test Is the target connectable
			dialCtx, dialCancel := context.WithTimeout(ctx, time.Second*15)
			defer dialCancel()

			_ = testHost.Connect(dialCtx, provider)
			// Call NewStream to force NAT hole punching. see https://github.com/libp2p/go-libp2p/issues/2714
			_, connErr := testHost.NewStream(dialCtx, provider.ID, "/ipfs/bitswap/1.2.0", "/ipfs/bitswap/1.1.0", "/ipfs/bitswap/1.0.0", "/ipfs/bitswap")

			if connErr != nil {
				provOutput.ConnectionError = formatConnectionError(connErr, provider.Addrs)
			} else {
				// since we pass a libp2p host that's already connected to the peer the actual connection maddr we pass in doesn't matter
				p2pAddr, _ := multiaddr.NewMultiaddr("/p2p/" + provider.ID.String())
				provOutput.DataAvailableOverBitswap = checkBitswapCID(ctx, testHost, cidKey, p2pAddr)

				for _, c := range testHost.Network().ConnsToPeer(provider.ID) {
					provOutput.ConnectionMaddrs = append(provOutput.ConnectionMaddrs, c.RemoteMultiaddr().String())
				}
			}

			mu.Lock()
			out = append(out, provOutput)
			mu.Unlock()
		}(provider, source)
	}
	cancelQuery()

	// Wait for all goroutines to finish
	wg.Wait()

	return &out, nil
}

type peerCheckOutput struct {
	ConnectionError              string
	PeerFoundInDHT               map[string]int
	ProviderRecordFromPeerInDHT  bool
	ProviderRecordFromPeerInIPNI bool
	ConnectionMaddrs             []string
	DataAvailableOverBitswap     BitswapCheckOutput
	DataAvailableOverHTTP        HTTPCheckOutput
}

// runPeerCheck checks the connectivity and Bitswap/HTTP availability of a CID from a given peer (either with just peer ID or specific multiaddr)
func (d *daemon) runPeerCheck(ctx context.Context, ma multiaddr.Multiaddr, ai peer.AddrInfo, c cid.Cid, ipniURL string, httpRetrieval bool) (*peerCheckOutput, error) {
	testHost, err := d.createTestHost()
	if err != nil {
		return nil, fmt.Errorf("server error: %w", err)
	}
	defer testHost.Close()

	var addrMap map[string]int
	var peerAddrDHTErr error
	var inDHT, inIPNI bool
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		// 15 seconds is the timeout for the peer routing query so we can continue with the check
		// if the peer is not found in the DHT within a reasonable time
		timeoutCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		addrMap, peerAddrDHTErr = peerAddrsInDHT(timeoutCtx, d.dht, d.dhtMessenger, ai.ID)
		wg.Done()
	}()
	go func() {
		inDHT = providerRecordFromPeerInDHT(ctx, d.dht, c, ai.ID)
		wg.Done()
	}()
	go func() {
		inIPNI = providerRecordFromPeerInIPNI(ctx, ipniURL, c, ai.ID)
		wg.Done()
	}()

	wg.Wait()

	out := &peerCheckOutput{
		ProviderRecordFromPeerInDHT:  inDHT,
		ProviderRecordFromPeerInIPNI: inIPNI,
		PeerFoundInDHT:               addrMap,
	}

	// FIXME: without AcceleratedDHT client, we usually timeout the full
	// operation context in the DHT steps. Provide early exit in that
	// case.
	if err := ctx.Err(); err != nil {
		return out, err
	}

	httpInfo, libp2pInfo := network.SplitHTTPAddrs(ai)

	// If they provided an http address and enabled retrieval, try that.
	if len(httpInfo.Addrs) > 0 && httpRetrieval {
		httpCheck := checkHTTPRetrieval(ctx, testHost, c, httpInfo, d.httpSkipVerify)
		out.DataAvailableOverHTTP = httpCheck
		if !httpCheck.Connected {
			out.ConnectionError = httpCheck.Error
		}
		for _, ma := range httpCheck.Endpoints {
			out.ConnectionMaddrs = append(out.ConnectionMaddrs, ma.String())
		}
		out.DataAvailableOverBitswap = BitswapCheckOutput{
			Enabled: false,
		}
		return out, nil
	}

	out.DataAvailableOverHTTP = HTTPCheckOutput{
		Enabled: false,
	}

	// non-http peers. Try to connect via p2p etc.
	var connectionFailed bool

	// If this is a non-HTTP peer, try with DHT addresses
	if len(libp2pInfo.Addrs) == 0 {
		if peerAddrDHTErr != nil {
			// PeerID is not resolvable via the DHT
			connectionFailed = true
			out.ConnectionError = peerAddrDHTErr.Error()
		}

		for a := range addrMap {
			ma, err := multiaddr.NewMultiaddr(a)
			if err != nil {
				log.Println(fmt.Errorf("error parsing multiaddr %s: %w", a, err))
				continue
			}
			libp2pInfo.Addrs = append(libp2pInfo.Addrs, ma)
		}
	}

	if !connectionFailed {
		// Test Is the target connectable
		dialCtx, dialCancel := context.WithTimeout(ctx, time.Second*120)

		_ = testHost.Connect(dialCtx, libp2pInfo)
		// Call NewStream to force NAT hole punching. see https://github.com/libp2p/go-libp2p/issues/2714
		_, connErr := testHost.NewStream(dialCtx, libp2pInfo.ID, "/ipfs/bitswap/1.2.0", "/ipfs/bitswap/1.1.0", "/ipfs/bitswap/1.0.0", "/ipfs/bitswap")
		dialCancel()
		if connErr != nil {
			out.ConnectionError = formatConnectionError(connErr, libp2pInfo.Addrs)
			return out, nil
		}
	}

	// If so is the data available over Bitswap?
	out.DataAvailableOverBitswap = checkBitswapCID(ctx, testHost, c, ma)

	// Get all connection maddrs to the peer (in case we hole punched, there will usually be two: limited relay and direct)
	for _, c := range testHost.Network().ConnsToPeer(ai.ID) {
		out.ConnectionMaddrs = append(out.ConnectionMaddrs, c.RemoteMultiaddr().String())
	}

	return out, nil
}

type BitswapCheckOutput struct {
	Enabled   bool
	Duration  time.Duration
	Found     bool
	Responded bool
	Error     string
}

func checkBitswapCID(ctx context.Context, host host.Host, c cid.Cid, ma multiaddr.Multiaddr) BitswapCheckOutput {
	log.Printf("Start of Bitswap check for cid %s by attempting to connect to ma: %v with the peer: %s", c, ma, host.ID())
	out := BitswapCheckOutput{
		Enabled: true,
	}
	start := time.Now()

	bsOut, err := vole.CheckBitswapCID(ctx, host, c, ma, false)
	if err != nil {
		out.Error = err.Error()
	} else {
		out.Found = bsOut.Found
		out.Responded = bsOut.Responded
		if bsOut.Error != nil {
			out.Error = bsOut.Error.Error()
		}
	}

	log.Printf("End of Bitswap check for %s by attempting to connect to ma: %v", c, ma)
	out.Duration = time.Since(start)
	return out
}

type HTTPCheckOutput struct {
	Enabled   bool
	Duration  time.Duration
	Endpoints []multiaddr.Multiaddr
	Connected bool
	Requested bool
	Found     bool
	Error     string
}

type httpReceiver struct {
	msgCh   chan bsmsg.BitSwapMessage
	errorCh chan error
}

func (recv *httpReceiver) ReceiveMessage(ctx context.Context, sender peer.ID, incoming bsmsg.BitSwapMessage) {
	recv.msgCh <- incoming
}

func (recv *httpReceiver) ReceiveError(err error) {
	recv.errorCh <- err
}

func (recv *httpReceiver) PeerConnected(p peer.ID) { // nop
}

func (recv *httpReceiver) PeerDisconnected(p peer.ID) { // nop
}

// FIXME: could expose this directly in Boxo.
func supportsHEAD(pstore peerstore.Peerstore, p peer.ID) bool {
	v, err := pstore.Get(p, "http-retrieval-head-support")
	if err != nil {
		return false
	}

	b, ok := v.(bool)
	return ok && b
}

func checkHTTPRetrieval(ctx context.Context, host host.Host, c cid.Cid, pinfo peer.AddrInfo, skipVerify bool) HTTPCheckOutput {
	log.Printf("Start of HTTP check for cid %s by attempting to connect to %s", c, pinfo)

	out := HTTPCheckOutput{
		Enabled: true,
	}

	htnet := httpnet.New(host,
		httpnet.WithUserAgent(userAgent),
		httpnet.WithResponseHeaderTimeout(5*time.Second), // default: 10
		httpnet.WithInsecureSkipVerify(skipVerify),
		httpnet.WithHTTPWorkers(1),
	)
	defer htnet.Stop()

	recv := httpReceiver{
		msgCh:   make(chan bsmsg.BitSwapMessage),
		errorCh: make(chan error),
	}
	htnet.Start(&recv)

	pid := pinfo.ID
	err := htnet.Connect(ctx, pinfo)
	defer htnet.DisconnectFrom(ctx, pid)
	if err != nil {
		log.Printf("End of HTTP check for %s: %s", c, err)
		out.Error = err.Error()
		return out
	}
	out.Connected = true
	out.Endpoints = host.Peerstore().Addrs(pid)

	if !supportsHEAD(host.Peerstore(), pid) {
		log.Printf("End of HTTP check for %s at %s: no support for HEAD requests", c, pinfo)
		out.Error = "HTTP endpoint does not support HEAD requests"
		return out
	}

	// Now we are in a position of sending a HEAD request.
	msg := bsmsg.New(true)
	msg.AddEntry(c, 0, pb.Message_Wantlist_Have, true)
	start := time.Now()
	err = htnet.SendMessage(ctx, pid, msg)
	out.Requested = true
	if err != nil {
		log.Printf("End of HTTP check for %s at . Connected: true. Error: %s", c, err)
		out.Error = err.Error()
		return out
	}

	waitCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	select {
	case <-waitCtx.Done():
	case msg := <-recv.msgCh:
		if len(msg.Haves()) > 0 {
			out.Found = true
		}

	case err = <-recv.errorCh:
		out.Error = err.Error()
	}

	out.Duration = time.Since(start)
	log.Printf("End of HTTP check for %s at %s. Connected: true. Requested: true. Found: %t. Error: %s", c, pinfo, out.Found, out.Error)
	return out
}

func peerAddrsInDHT(ctx context.Context, d kademlia, messenger *dhtpb.ProtocolMessenger, p peer.ID) (map[string]int, error) {
	addrMap := make(map[string]int)

	closestPeers, err := d.GetClosestPeers(ctx, string(p))
	if err != nil {
		return addrMap, err
	}
	resCh := make(chan *peer.AddrInfo, len(closestPeers))

	numSuccessfulResponses := execOnMany(ctx, 0.3, time.Second*3, func(ctx context.Context, peerToQuery peer.ID) error {
		endResults, err := messenger.GetClosestPeers(ctx, peerToQuery, p)
		if err == nil {
			for _, r := range endResults {
				if r.ID == p {
					resCh <- r
					return nil
				}
			}
			resCh <- nil
		}
		return err
	}, closestPeers, false)
	close(resCh)

	if numSuccessfulResponses == 0 {
		return addrMap, fmt.Errorf("host had trouble querying the DHT")
	}

	for r := range resCh {
		if r == nil {
			continue
		}
		for _, addr := range r.Addrs {
			addrMap[addr.String()]++
		}
	}

	return addrMap, nil
}

func providerRecordFromPeerInDHT(ctx context.Context, d kademlia, c cid.Cid, p peer.ID) bool {
	queryCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	provsCh := d.FindProvidersAsync(queryCtx, c, 0)
	for {
		select {
		case prov, ok := <-provsCh:
			if !ok {
				return false
			}
			if prov.ID == p {
				return true
			}
		case <-ctx.Done():
			return false
		}
	}
}

func providerRecordFromPeerInIPNI(ctx context.Context, ipniURL string, c cid.Cid, p peer.ID) bool {
	// Since we match on the peer ID, we should also check for HTTP peers
	protocols := append(defaultProtocolFilter, "transport-ipfs-gateway-http")
	crClient, err := client.New(ipniURL, client.WithStreamResultsRequired(), client.WithProtocolFilter(protocols))
	if err != nil {
		log.Printf("failed to creat content router client: %s\n", err)
		return false
	}
	routerClient := contentrouter.NewContentRoutingClient(crClient)

	queryCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	provsCh := routerClient.FindProvidersAsync(queryCtx, c, 0)
	for {
		select {
		case prov, ok := <-provsCh:
			if !ok {
				return false
			}
			if prov.ID == p {
				return true
			}
		case <-ctx.Done():
			return false
		}
	}
}

// Taken from the FullRT DHT client implementation
//
// execOnMany executes the given function on each of the peers, although it may only wait for a certain chunk of peers
// to respond before considering the results "good enough" and returning.
//
// If sloppyExit is true then this function will return without waiting for all of its internal goroutines to close.
// If sloppyExit is true then the passed in function MUST be able to safely complete an arbitrary amount of time after
// execOnMany has returned (e.g. do not write to resources that might get closed or set to nil and therefore result in
// a panic instead of just returning an error).
func execOnMany(ctx context.Context, waitFrac float64, timeoutPerOp time.Duration, fn func(context.Context, peer.ID) error, peers []peer.ID, sloppyExit bool) int {
	if len(peers) == 0 {
		return 0
	}

	// having a buffer that can take all of the elements is basically a hack to allow for sloppy exits that clean up
	// the goroutines after the function is done rather than before
	errCh := make(chan error, len(peers))
	numSuccessfulToWaitFor := int(float64(len(peers)) * waitFrac)

	putctx, cancel := context.WithTimeout(ctx, timeoutPerOp)
	defer cancel()

	for _, p := range peers {
		go func(p peer.ID) {
			errCh <- fn(putctx, p)
		}(p)
	}

	var numDone, numSuccess, successSinceLastTick int
	var ticker *time.Ticker
	var tickChan <-chan time.Time

	for numDone < len(peers) {
		select {
		case err := <-errCh:
			numDone++
			if err == nil {
				numSuccess++
				if numSuccess >= numSuccessfulToWaitFor && ticker == nil {
					// Once there are enough successes, wait a little longer
					ticker = time.NewTicker(time.Millisecond * 500)
					defer ticker.Stop()
					tickChan = ticker.C
					successSinceLastTick = numSuccess
				}
				// This is equivalent to numSuccess * 2 + numFailures >= len(peers) and is a heuristic that seems to be
				// performing reasonably.
				// TODO: Make this metric more configurable
				// TODO: Have better heuristics in this function whether determined from observing static network
				// properties or dynamically calculating them
				if numSuccess+numDone >= len(peers) {
					cancel()
					if sloppyExit {
						return numSuccess
					}
				}
			}
		case <-tickChan:
			if numSuccess > successSinceLastTick {
				// If there were additional successes, then wait another tick
				successSinceLastTick = numSuccess
			} else {
				cancel()
				if sloppyExit {
					return numSuccess
				}
			}
		}
	}
	return numSuccess
}

// formatConnectionError provides a more user-friendly error message for connection failures,
// particularly when the connection gater blocks private addresses
func formatConnectionError(err error, addrs []multiaddr.Multiaddr) string {
	errStr := err.Error()

	// Check if this looks like a gater blocking private addresses error
	if strings.Contains(errStr, "gater disallows connection to peer") {
		var privateCount, publicCount int

		// Count private vs public addresses
		for _, addr := range addrs {
			if manet.IsPublicAddr(addr) {
				publicCount++
			} else {
				privateCount++
			}
		}

		// If we have private addresses being blocked, provide a cleaner message
		if privateCount > 0 {
			if publicCount == 0 {
				return fmt.Sprintf("failed to dial: all addresses (%d) are private/local and cannot be reached from the public internet", privateCount)
			}

			return fmt.Sprintf("failed to dial: no good addresses (%d private addresses filtered out)", privateCount)
		}
	}

	// For other errors, return the original error message
	return errStr
}
