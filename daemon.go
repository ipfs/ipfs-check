package main

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	bsmsg "github.com/ipfs/boxo/bitswap/message"
	"github.com/ipfs/boxo/bitswap/message/pb"
	"github.com/ipfs/boxo/bitswap/network"
	"github.com/ipfs/boxo/bitswap/network/httpnet"
	"github.com/ipfs/boxo/gateway"
	"github.com/ipfs/boxo/namesys"
	"github.com/ipfs/boxo/routing/http/client"
	"github.com/ipfs/boxo/routing/http/contentrouter"
	"github.com/ipfs/go-cid"
	vole "github.com/ipshipyard/vole/lib"
	doh "github.com/libp2p/go-doh-resolver"
	"github.com/libp2p/go-libp2p"
	dhtpb "github.com/libp2p/go-libp2p-kad-dht/pb"
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
	ns             namesys.NameSystem
	createTestHost func() (host.Host, error)
	promRegistry   *prometheus.Registry
	httpSkipVerify bool
}

const (
	// Soft cap on providers with at least one usable multiaddr. Stale
	// (address-less) records do not consume a slot. Sized larger than the
	// 10 that Kubo's bitswap requests per provider-search round, because
	// ipfs-check probes once and cannot repeat the round as a long-lived
	// bitswap session would.
	maxProvidersCount = 20

	// Hard cap on processed records per request. Bounds fan-out when every
	// record is stale.
	maxAttemptedProviders = 2 * maxProvidersCount

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

	// Setup DHT (standard or bundled with accelerated)
	d, err := setupDHT(h, acceleratedDHT)
	if err != nil {
		return nil, err
	}

	pm, err := dhtProtocolMessenger("/ipfs/kad/1.0.0", h)
	if err != nil {
		return nil, err
	}

	// Create DNS resolver with delegated-ipfs.dev DoH endpoint to match IPFS Mainnet behavior.
	// This endpoint is used by the Helia ecosystem in browsers and ensures consistent
	// DNSLink resolution without requiring local DNS resolver configuration.
	// DNS caching is disabled (doh.WithCacheDisabled) because this is a diagnostic tool
	// that should always query current DNS state rather than serve potentially stale cached results.
	dnsResolver, err := gateway.NewDNSResolver(
		map[string]string{
			".": "https://delegated-ipfs.dev/dns-query",
		},
		doh.WithCacheDisabled(),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create DNS resolver: %w", err)
	}

	// Create namesys without caching (no WithCache option) to ensure fresh IPNS resolution.
	// Diagnostic tools should not cache IPNS records as users expect to see current network state
	// when running checks, not cached data from previous resolutions.
	ns, err := namesys.NewNameSystem(d, namesys.WithDNSResolver(dnsResolver))
	if err != nil {
		return nil, err
	}

	return &daemon{
		h:            h,
		dht:          d,
		dhtMessenger: pm,
		ns:           ns,
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

type MutableResolution struct {
	InputPath      string `json:"InputPath,omitempty"`
	ResolvedPath   string `json:"ResolvedPath,omitempty"`
	DiagnosticURL  string `json:"DiagnosticURL,omitempty"`
	Error          string `json:"Error,omitempty"`
	IsMutableInput bool   `json:"IsMutableInput,omitempty"`
}

type cidCheckOutput *[]providerOutput

type providerOutput struct {
	ID                       string
	ConnectionError          string
	Addrs                    []string
	ConnectionMaddrs         []string
	DataAvailableOverBitswap BitswapCheckOutput
	DataAvailableOverHTTP    HTTPCheckOutput
	BrowserCheck             BrowserCheckOutput
	Source                   string
	AgentVersion             string
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

	// count=0 streams unbounded; we cap termination on results below so
	// address-less records do not exhaust the budget.
	dhtProvsCh := d.dht.FindProvidersAsync(queryCtx, cidKey, maxAttemptedProviders)
	ipniProvsCh := routerClient.FindProvidersAsync(queryCtx, cidKey, maxAttemptedProviders)

	out := make([]providerOutput, 0, maxAttemptedProviders)
	var wg sync.WaitGroup
	var mu sync.Mutex
	// withAddrsCount feeds the soft cap; dispatchedCount feeds the hard cap.
	// Counting only address-resolved providers against the soft cap prevents
	// stale-record bursts from cancelling the lookup early.
	var dispatchedCount, withAddrsCount int
	var done bool

	for !done {
		var provider peer.AddrInfo
		var open bool
		var source string

		select {
		case <-ctx.Done():
			// Respect the timeout from the URL/context
			done = true
			continue
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

		mu.Lock()
		if withAddrsCount >= maxProvidersCount || dispatchedCount >= maxAttemptedProviders {
			done = true
			mu.Unlock()
			continue
		}
		dispatchedCount++
		mu.Unlock()

		wg.Add(1)
		go func(provider peer.AddrInfo, src string) {
			defer wg.Done()

			provOutput, ok := d.checkProvider(ctx, cidKey, provider, src, httpRetrieval)
			if !ok {
				return
			}

			mu.Lock()
			out = append(out, provOutput)
			if len(provOutput.Addrs) > 0 {
				withAddrsCount++
			}
			mu.Unlock()
		}(provider, source)
	}
	cancelQuery()

	// Wait for all goroutines to finish
	wg.Wait()

	return &out, nil
}

// checkProvider probes a single provider over HTTP and/or Bitswap.
//
// Each probe builds its own libp2p host. Sharing one host across the
// request's goroutines is unsafe because vole.CheckBitswapCID installs a
// bitswap stream handler via host.SetStreamHandler, which replaces any
// prior handler; concurrent probes would then deliver responses to the
// wrong receiver. httpnet has the same shape of issue with connection
// notifiers, so the HTTP path also uses its own host.
//
// Returns false when a probe host cannot be constructed; the caller
// should drop the provider rather than append a stub.
func (d *daemon) checkProvider(ctx context.Context, cidKey cid.Cid, provider peer.AddrInfo, src string, httpRetrieval bool) (provOutput providerOutput, ok bool) {
	provOutput = providerOutput{
		ID:                       provider.ID.String(),
		Source:                   src,
		DataAvailableOverBitswap: BitswapCheckOutput{},
		DataAvailableOverHTTP:    HTTPCheckOutput{},
	}

	// Whether a browser could use this provider is answered on a separate host,
	// dialing only browser-dialable addresses, so it runs alongside the probes
	// below rather than after them. Started once the candidate addresses are
	// known, which happens at a different point for HTTP-only providers.
	var browserWG sync.WaitGroup
	var browserOut BrowserCheckOutput
	peerID := provider.ID
	startBrowserCheck := func(addrs []multiaddr.Multiaddr) {
		browserWG.Go(func() {
			browserOut = checkBrowserCompat(ctx, d.createTestHost, peerID, cidKey, addrs)
		})
	}
	defer func() {
		browserWG.Wait()
		provOutput.BrowserCheck = browserOut
	}()

	httpInfo, libp2pInfo := network.SplitHTTPAddrs(provider)
	if len(httpInfo.Addrs) > 0 && httpRetrieval {
		provOutput.DataAvailableOverHTTP.Enabled = true

		for _, ma := range httpInfo.Addrs {
			provOutput.Addrs = append(provOutput.Addrs, ma.String())
		}

		httpHost, err := d.createTestHost()
		if err != nil {
			log.Printf("Error creating test host: %v\n", err)
			return provOutput, false
		}
		defer httpHost.Close()
		httpCheck := checkHTTPRetrieval(ctx, httpHost, cidKey, httpInfo, d.httpSkipVerify)
		provOutput.DataAvailableOverHTTP = httpCheck
		if !httpCheck.Connected {
			provOutput.ConnectionError = httpCheck.Error
		}
		for _, ma := range httpCheck.Endpoints {
			provOutput.ConnectionMaddrs = append(provOutput.ConnectionMaddrs, ma.String())
		}

		if len(libp2pInfo.Addrs) == 0 {
			provOutput.DataAvailableOverBitswap.Enabled = false
			startBrowserCheck(httpInfo.Addrs)
			return provOutput, true
		}
	}

	provider = libp2pInfo

	outputAddrs := []string{}
	publicAddrs := []multiaddr.Multiaddr{}
	seen := map[string]struct{}{}
	addPublicAddr := func(addr multiaddr.Multiaddr, alsoDial bool) {
		if !manet.IsPublicAddr(addr) {
			return
		}
		s := addr.String()
		if _, ok := seen[s]; ok {
			return
		}
		seen[s] = struct{}{}
		outputAddrs = append(outputAddrs, s)
		publicAddrs = append(publicAddrs, addr)
		if alsoDial {
			provider.Addrs = append(provider.Addrs, addr)
		}
	}

	// Record addresses: only the public ones surface in the output. Private
	// entries remain in provider.Addrs for libp2p, which the gater rejects.
	for _, addr := range provider.Addrs {
		addPublicAddr(addr, false)
	}

	// FindPeer when the record had no public addrs. Full ctx: the user's
	// timeout bounds the wait.
	if len(outputAddrs) == 0 {
		peerAddrs, err := d.dht.FindPeer(ctx, provider.ID)
		if err == nil {
			for _, addr := range peerAddrs.Addrs {
				addPublicAddr(addr, true)
			}
		}
	}

	// Merge addrs the daemon's peerstore already holds from prior DHT
	// lookups, identifies, and earlier probes. Mirrors Kubo's dialing.
	for _, addr := range d.h.Peerstore().Addrs(provider.ID) {
		addPublicAddr(addr, true)
	}

	provOutput.Addrs = append(provOutput.Addrs, outputAddrs...)
	provOutput.DataAvailableOverBitswap.Enabled = true

	// HTTP endpoints count as browser-usable too, so a provider offering both
	// is judged on everything it announces.
	browserAddrs := make([]multiaddr.Multiaddr, 0, len(httpInfo.Addrs)+len(publicAddrs))
	browserAddrs = append(browserAddrs, httpInfo.Addrs...)
	browserAddrs = append(browserAddrs, publicAddrs...)
	startBrowserCheck(browserAddrs)

	testHost, err := d.createTestHost()
	if err != nil {
		log.Printf("Error creating test host: %v\n", err)
		return provOutput, false
	}
	defer testHost.Close()

	dialCtx, dialCancel := context.WithTimeout(ctx, time.Second*30)
	defer dialCancel()

	_ = testHost.Connect(dialCtx, provider)
	// Call NewStream to force NAT hole punching. see https://github.com/libp2p/go-libp2p/issues/2714
	_, connErr := testHost.NewStream(dialCtx, provider.ID, "/ipfs/bitswap/1.2.0", "/ipfs/bitswap/1.1.0", "/ipfs/bitswap/1.0.0", "/ipfs/bitswap")

	if connErr != nil {
		provOutput.ConnectionError = formatConnectionError(connErr, provider.Addrs)
		return provOutput, true
	}
	if agent, err := testHost.Peerstore().Get(provider.ID, "AgentVersion"); err == nil {
		if agentStr, ok := agent.(string); ok {
			provOutput.AgentVersion = sanitizeAgentVersion(agentStr)
		}
	}
	p2pAddr, _ := multiaddr.NewMultiaddr("/p2p/" + provider.ID.String())
	provOutput.DataAvailableOverBitswap = checkBitswapCID(ctx, testHost, cidKey, p2pAddr)

	for _, c := range testHost.Network().ConnsToPeer(provider.ID) {
		provOutput.ConnectionMaddrs = append(provOutput.ConnectionMaddrs, c.RemoteMultiaddr().String())
	}

	return provOutput, true
}

type peerCheckOutput struct {
	ConnectionError              string
	PeerFoundInDHT               map[string]int
	ProviderRecordFromPeerInDHT  bool
	ProviderRecordFromPeerInIPNI bool
	ConnectionMaddrs             []string
	DataAvailableOverBitswap     BitswapCheckOutput
	DataAvailableOverHTTP        HTTPCheckOutput
	BrowserCheck                 BrowserCheckOutput
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

	// Same as in checkProvider: the browser-only dial gets its own host and
	// runs alongside the probes below. The deferred wait covers the early
	// returns further down.
	var browserWG sync.WaitGroup
	var browserOut BrowserCheckOutput
	startBrowserCheck := func(addrs []multiaddr.Multiaddr) {
		browserWG.Go(func() {
			browserOut = checkBrowserCompat(ctx, d.createTestHost, ai.ID, c, addrs)
		})
	}
	defer func() {
		browserWG.Wait()
		out.BrowserCheck = browserOut
	}()

	// If they provided an http address and enabled retrieval, try that.
	if len(httpInfo.Addrs) > 0 && httpRetrieval {
		startBrowserCheck(httpInfo.Addrs)
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

	// Started here rather than above, so it sees the addresses the DHT
	// contributed when the caller passed a bare peer ID.
	startBrowserCheck(libp2pInfo.Addrs)

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

	bsOut, err := vole.CheckBitswapCID(ctx, host, c, ma, true)
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

	// Use a private cooldown registry, not the process-wide default. With the
	// shared one, a failed probe answers the next check of the same host from
	// memory for a minute. Someone who just fixed their gateway would then be
	// told it is unreachable, with nothing dialed. Each check must dial.
	htnet := httpnet.New(host,
		httpnet.WithUserAgent(userAgent),
		httpnet.WithResponseHeaderTimeout(5*time.Second), // default: 10
		httpnet.WithInsecureSkipVerify(skipVerify),
		httpnet.WithHTTPWorkers(1),
		httpnet.WithCooldownTracker(httpnet.NewCooldownTracker()),
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

	// Now we are in a position of sending a GET request.
	msg := bsmsg.New(true)
	msg.AddEntry(c, 0, pb.Message_Wantlist_Block, true)
	start := time.Now()
	err = htnet.SendMessage(ctx, pid, msg)
	out.Requested = true
	if err != nil {
		log.Printf("End of HTTP check for %s at %s. Connected: true. Error: %s", c, pinfo, err)
		out.Error = err.Error()
		return out
	}

	waitCtx, cancel := context.WithTimeout(ctx, httpnet.DefaultResponseHeaderTimeout)
	defer cancel()
	select {
	case <-waitCtx.Done():
	case msg := <-recv.msgCh:
		if len(msg.Blocks()) > 0 {
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
