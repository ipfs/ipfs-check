package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"sync"
	"time"

	vole "github.com/ipfs-shipyard/vole/lib"
	"github.com/ipfs/boxo/ipns"
	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p-kad-dht/fullrt"
	dhtpb "github.com/libp2p/go-libp2p-kad-dht/pb"
	mplex "github.com/libp2p/go-libp2p-mplex"
	record "github.com/libp2p/go-libp2p-record"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/peerstore"
	"github.com/libp2p/go-libp2p/core/routing"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/multiformats/go-multiaddr"
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
}

func newDaemon(ctx context.Context, acceleratedDHT bool) (*daemon, error) {
	rm, err := NewResourceManager()
	if err != nil {
		return nil, err
	}

	c, err := connmgr.NewConnManager(600, 900, connmgr.WithGracePeriod(time.Second*30))
	if err != nil {
		return nil, err
	}

	h, err := libp2p.New(
		libp2p.DefaultMuxers,
		libp2p.Muxer(mplex.ID, mplex.DefaultTransport),
		libp2p.ConnectionManager(c),
		libp2p.ConnectionGater(&privateAddrFilterConnectionGater{}),
		libp2p.ResourceManager(rm),
		libp2p.EnableHolePunching(),
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

	return &daemon{h: h, dht: d, dhtMessenger: pm, createTestHost: func() (host.Host, error) {
		return libp2p.New(
			libp2p.ConnectionGater(&privateAddrFilterConnectionGater{}),
			libp2p.DefaultMuxers,
			libp2p.Muxer("/mplex/6.7.0", mplex.DefaultTransport),
			libp2p.EnableHolePunching(),
		)
	}}, nil
}

func (d *daemon) mustStart() {
	// Wait for the DHT to be ready
	if frt, ok := d.dht.(*fullrt.FullRT); ok {
		for !frt.Ready() {
			time.Sleep(time.Second * 10)
		}
	}

}

func (d *daemon) runCheck(query url.Values) (*output, error) {
	maStr := query.Get("multiaddr")
	cidStr := query.Get("cid")

	if maStr == "" {
		return nil, errors.New("missing 'multiaddr' argument")
	}

	if cidStr == "" {
		return nil, errors.New("missing 'cid' argument")
	}

	ma, err := multiaddr.NewMultiaddr(maStr)
	if err != nil {
		return nil, err
	}

	ai, err := peer.AddrInfoFromP2pAddr(ma)
	if err != nil {
		return nil, err
	}

	// User has only passed a PeerID without any maddrs
	onlyPeerID := len(ai.Addrs) == 0

	c, err := cid.Decode(cidStr)
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	out := &output{}

	connectionFailed := false

	out.CidInDHT = providerRecordInDHT(ctx, d.dht, c, ai.ID)

	addrMap, peerAddrDHTErr := peerAddrsInDHT(ctx, d.dht, d.dhtMessenger, ai.ID)
	out.PeerFoundInDHT = addrMap

	// If peerID given,but no addresses check the DHT
	if onlyPeerID {
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
		dialCtx, dialCancel := context.WithTimeout(ctx, time.Second*15)

		// we call NewStream instead of Connect to force NAT hole punching
		// See https://github.com/libp2p/go-libp2p/issues/2714
		testHost.Peerstore().AddAddrs(ai.ID, ai.Addrs, peerstore.RecentlyConnectedAddrTTL)
		_, connErr := testHost.NewStream(dialCtx, ai.ID, "/ipfs/bitswap/1.2.0", "/ipfs/bitswap/1.1.0", "/ipfs/bitswap/1.0.0", "/ipfs/bitswap")
		dialCancel()
		if connErr != nil {
			out.ConnectionError = fmt.Sprintf("error dialing to peer: %s", connErr.Error())
			connectionFailed = true
		}
	}

	if connectionFailed {
		out.DataAvailableOverBitswap.Error = "could not connect to peer"
	} else {
		// If so is the data available over Bitswap?
		out.DataAvailableOverBitswap = checkBitswapCID(ctx, testHost, c, ma)
		conns := testHost.Network().ConnsToPeer(ai.ID)
		if len(conns) > 0 {
			maddr := conns[0].RemoteMultiaddr()
			addrWithPeerID := maddr.Encapsulate(multiaddr.StringCast("/p2p/" + ai.ID.String()))
			out.ConnectionMaddr = addrWithPeerID.String()
		}
	}

	return out, nil
}

func checkBitswapCID(ctx context.Context, host host.Host, c cid.Cid, ma multiaddr.Multiaddr) BitswapCheckOutput {
	log.Printf("Start of Bitswap check for cid %s by attempting to connect to ma: %v with the temporary peer: %s", c, ma, host.ID())
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

type BitswapCheckOutput struct {
	Duration  time.Duration
	Found     bool
	Responded bool
	Error     string
}

type output struct {
	ConnectionError          string
	PeerFoundInDHT           map[string]int
	CidInDHT                 bool
	ConnectionMaddr          string
	DataAvailableOverBitswap BitswapCheckOutput
}

func peerAddrsInDHT(ctx context.Context, d kademlia, messenger *dhtpb.ProtocolMessenger, p peer.ID) (map[string]int, error) {
	closestPeers, err := d.GetClosestPeers(ctx, string(p))
	if err != nil {
		return nil, err
	}

	wg := sync.WaitGroup{}
	wg.Add(len(closestPeers))

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

func providerRecordInDHT(ctx context.Context, d kademlia, c cid.Cid, p peer.ID) bool {
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
