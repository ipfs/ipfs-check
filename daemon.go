package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	vole "github.com/ipfs-shipyard/vole/lib"
	"github.com/ipfs/boxo/ipns"
	"github.com/ipfs/boxo/routing/http/client"
	"github.com/ipfs/boxo/routing/http/contentrouter"
	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p-kad-dht/fullrt"
	dhtpb "github.com/libp2p/go-libp2p-kad-dht/pb"
	mplex "github.com/libp2p/go-libp2p-mplex"
	record "github.com/libp2p/go-libp2p-record"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
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
}

const (
	// number of providers at which to stop looking for providers in the DHT
	// When doing a check only with a CID
	maxProvidersCount = 10

	ipniSource = "IPNI"
	dhtSource  = "Amino DHT"
)

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
		libp2p.DefaultMuxers,
		libp2p.Muxer(mplex.ID, mplex.DefaultTransport),
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
				libp2p.DefaultMuxers,
				libp2p.Muxer("/mplex/6.7.0", mplex.DefaultTransport),
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
	Source                   string
}

// runCidCheck finds providers of a given CID, using the DHT and IPNI
// concurrently. A check of connectivity and Bitswap availability is performed
// for each provider found.
func (d *daemon) runCidCheck(ctx context.Context, cidKey cid.Cid, ipniURL string) (cidCheckOutput, error) {
	crClient, err := client.New(ipniURL, client.WithStreamResultsRequired())
	if err != nil {
		return nil, fmt.Errorf("failed to creat content router client: %w", err)
	}
	routerClient := contentrouter.NewContentRoutingClient(crClient)

	queryCtx, cancelQuery := context.WithCancel(ctx)
	defer cancelQuery()

	// Find providers with DHT and IPNI concurrently.
	provsCh := d.dht.FindProvidersAsync(queryCtx, cidKey, maxProvidersCount)
	ipniProvsCh := routerClient.FindProvidersAsync(queryCtx, cidKey, maxProvidersCount)

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
		case provider, open = <-provsCh:
			if !open {
				provsCh = nil
				if ipniProvsCh == nil {
					done = true
				}
				continue
			}
			source = dhtSource
		case provider, open = <-ipniProvsCh:
			if !open {
				ipniProvsCh = nil
				if provsCh == nil {
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

			provOutput := providerOutput{
				ID:                       provider.ID.String(),
				Addrs:                    outputAddrs,
				DataAvailableOverBitswap: BitswapCheckOutput{},
				Source:                   src,
			}

			testHost, err := d.createTestHost()
			if err != nil {
				log.Printf("Error creating test host: %v\n", err)
				return
			}
			defer testHost.Close()

			// Test Is the target connectable
			dialCtx, dialCancel := context.WithTimeout(ctx, time.Second*15)
			defer dialCancel()

			_ = testHost.Connect(dialCtx, provider)
			// Call NewStream to force NAT hole punching. see https://github.com/libp2p/go-libp2p/issues/2714
			_, connErr := testHost.NewStream(dialCtx, provider.ID, "/ipfs/bitswap/1.2.0", "/ipfs/bitswap/1.1.0", "/ipfs/bitswap/1.0.0", "/ipfs/bitswap")

			if connErr != nil {
				provOutput.ConnectionError = connErr.Error()
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
}

// runPeerCheck checks the connectivity and Bitswap availability of a CID from a given peer (either with just peer ID or specific multiaddr)
func (d *daemon) runPeerCheck(ctx context.Context, ma multiaddr.Multiaddr, ai *peer.AddrInfo, c cid.Cid, ipniURL string) (*peerCheckOutput, error) {
	addrMap, peerAddrDHTErr := peerAddrsInDHT(ctx, d.dht, d.dhtMessenger, ai.ID)

	var inDHT, inIPNI bool
	var wg sync.WaitGroup
	wg.Add(2)
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

	var connectionFailed bool

	// If peerID given,but no addresses check the DHT
	if len(ai.Addrs) == 0 {
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
			ai.Addrs = append(ai.Addrs, ma)
		}
	}

	testHost, err := d.createTestHost()
	if err != nil {
		return nil, fmt.Errorf("server error: %w", err)
	}
	defer testHost.Close()

	if !connectionFailed {
		// Test Is the target connectable
		dialCtx, dialCancel := context.WithTimeout(ctx, time.Second*120)

		_ = testHost.Connect(dialCtx, *ai)
		// Call NewStream to force NAT hole punching. see https://github.com/libp2p/go-libp2p/issues/2714
		_, connErr := testHost.NewStream(dialCtx, ai.ID, "/ipfs/bitswap/1.2.0", "/ipfs/bitswap/1.1.0", "/ipfs/bitswap/1.0.0", "/ipfs/bitswap")
		dialCancel()
		if connErr != nil {
			out.ConnectionError = connErr.Error()
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
	Duration  time.Duration
	Found     bool
	Responded bool
	Error     string
}

func checkBitswapCID(ctx context.Context, host host.Host, c cid.Cid, ma multiaddr.Multiaddr) BitswapCheckOutput {
	log.Printf("Start of Bitswap check for cid %s by attempting to connect to ma: %v with the peer: %s", c, ma, host.ID())
	out := BitswapCheckOutput{}
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

func peerAddrsInDHT(ctx context.Context, d kademlia, messenger *dhtpb.ProtocolMessenger, p peer.ID) (map[string]int, error) {
	closestPeers, err := d.GetClosestPeers(ctx, string(p))
	if err != nil {
		return nil, err
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
		return nil, fmt.Errorf("host had trouble querying the DHT")
	}

	addrMap := make(map[string]int)
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
	crClient, err := client.New(ipniURL, client.WithStreamResultsRequired())
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
