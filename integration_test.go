package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	bsnet "github.com/ipfs/boxo/bitswap/network/bsnet"
	bsserver "github.com/ipfs/boxo/bitswap/server"
	"github.com/ipfs/boxo/blockstore"
	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
	ipld "github.com/ipfs/go-ipld-format"
	"github.com/ipfs/ipfs-check/test"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	multiaddr "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/multiformats/go-multihash"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/require"
)

type trustlessGateway struct {
	bstore blockstore.Blockstore
}

// A HTTP server for blocks in a blockstore.
func (h *trustlessGateway) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	_, cidstr, ok := strings.Cut(path, "/ipfs/")
	if !ok {
		rw.WriteHeader(http.StatusBadRequest)
		return
	}

	if cidstr == "bafkqaaa" {
		rw.WriteHeader(http.StatusOK)
		return
	}

	c, err := cid.Parse(cidstr)
	if err != nil {
		rw.WriteHeader(http.StatusBadRequest)
		return
	}

	b, err := h.bstore.Get(r.Context(), c)
	if errors.Is(err, ipld.ErrNotFound{}) {
		rw.WriteHeader(http.StatusNotFound)
		return
	}
	if err != nil {
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}

	rw.WriteHeader(http.StatusOK)
	if r.Method == "HEAD" {
		return
	}

	rw.Write(b.RawData())
}

type httpRouting struct {
	bstore blockstore.Blockstore
	addrs  []multiaddr.Multiaddr
	id     peer.ID
}

// A HTTP server for blocks in a blockstore.
func (h *httpRouting) ServeHTTP(rw http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	_, cidstr, ok := strings.Cut(path, "/routing/v1/providers/")
	if !ok {
		rw.WriteHeader(http.StatusBadRequest)
		return
	}

	c, err := cid.Parse(cidstr)
	if err != nil {
		rw.WriteHeader(http.StatusBadRequest)
		return
	}

	_, err = h.bstore.Get(r.Context(), c)
	if errors.Is(err, ipld.ErrNotFound{}) {
		rw.WriteHeader(http.StatusNotFound)
		return
	}
	if err != nil {
		rw.WriteHeader(http.StatusInternalServerError)
		return
	}
	rw.Header().Set("Content-Type", "application/x-ndjson")
	rw.WriteHeader(http.StatusOK)
	jaddrs, _ := json.Marshal(h.addrs)
	resp := `
{
  "Schema": "peer",
  "ID": "` + h.id.String() + `",
  "Addrs": ` + string(jaddrs) + `,
  "Protocols": ["transport-ipfs-gateway-http"]
}
`
	fmt.Println(resp)
	rw.Write([]byte(resp))
}

func TestBasicIntegration(t *testing.T) {
	ctx := t.Context()

	testDHTPrefix := protocol.TestingID
	testDHTID := protocol.TestingID + "/kad/1.0.0"

	dhtHost, err := libp2p.New()
	require.NoError(t, err)
	defer dhtHost.Close()
	dhtServer, err := dht.New(dhtHost, dht.Mode(dht.ModeServer), dht.ProtocolPrefix(testDHTPrefix))
	require.NoError(t, err)
	defer dhtServer.Close()

	go func() {
		rm, err := NewResourceManager()
		require.NoError(t, err)

		c, err := connmgr.NewConnManager(600, 900, connmgr.WithGracePeriod(time.Second*30))
		require.NoError(t, err)

		queryHost, err := libp2p.New(
			libp2p.ConnectionManager(c),
			libp2p.ResourceManager(rm),
			libp2p.EnableHolePunching(),
		)
		require.NoError(t, err)

		pm, err := dhtProtocolMessenger(testDHTID, queryHost)
		require.NoError(t, err)
		queryDHT, err := dht.New(queryHost, dht.ProtocolPrefix(testDHTPrefix), dht.BootstrapPeers(peer.AddrInfo{ID: dhtHost.ID(), Addrs: dhtHost.Addrs()}))
		require.NoError(t, err)

		d := &daemon{
			promRegistry: prometheus.NewRegistry(),
			h:            queryHost,
			dht:          queryDHT,
			dhtMessenger: pm,
			createTestHost: func() (host.Host, error) {
				return libp2p.New(libp2p.EnableHolePunching())
			},
			httpSkipVerify: true,
		}
		_ = startServer(ctx, d, ":1234", "", "")
	}()

	h, err := libp2p.New()
	require.NoError(t, err)
	defer h.Close()
	bn := bsnet.NewFromIpfsHost(h)
	bstore := blockstore.NewBlockstore(dssync.MutexWrap(datastore.NewMapDatastore()))
	bswap := bsserver.New(ctx, bn, bstore)
	bn.Start(bswap)
	defer bswap.Close()
	dhtClient, err := dht.New(h, dht.ProtocolPrefix(testDHTPrefix), dht.Mode(dht.ModeClient), dht.BootstrapPeers(peer.AddrInfo{ID: dhtHost.ID(), Addrs: dhtHost.Addrs()}))
	require.NoError(t, err)
	defer dhtClient.Close()
	err = dhtClient.Bootstrap(ctx)
	require.NoError(t, err)
	for dhtClient.RoutingTable().Size() == 0 {
		select {
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		case <-time.After(time.Millisecond * 5):
		}
	}

	mas, err := peer.AddrInfoToP2pAddrs(&peer.AddrInfo{ID: h.ID(), Addrs: h.Addrs()})
	require.NoError(t, err)
	hostAddr := mas[0]

	gw := &trustlessGateway{
		bstore: bstore,
	}
	httpServer := httptest.NewUnstartedServer(gw)
	httpServer.EnableHTTP2 = true
	httpServer.StartTLS()
	maddr, err := manet.FromNetAddr(httpServer.Listener.Addr())
	require.NoError(t, err)
	httpAddr, err := multiaddr.NewMultiaddr("/https")
	require.NoError(t, err)
	peerAddr, err := multiaddr.NewMultiaddr("/p2p/" + h.ID().String())
	require.NoError(t, err)
	httpPeerAddr := maddr.Encapsulate(httpAddr).Encapsulate(peerAddr)

	rt := &httpRouting{
		bstore: bstore,
		addrs:  []multiaddr.Multiaddr{maddr.Encapsulate(httpAddr)},
		id:     h.ID(),
	}

	routingServer := httptest.NewUnstartedServer(rt)
	routingServer.Start()
	ipniAddr := "http://" + routingServer.Listener.Addr().String()

	t.Run("Data on reachable peer that's advertised", func(t *testing.T) {
		testData := []byte(t.Name())
		mh, err := multihash.Sum(testData, multihash.SHA2_256, -1)
		require.NoError(t, err)
		testCid := cid.NewCidV1(cid.Raw, mh)
		testBlock, err := blocks.NewBlockWithCid(testData, testCid)
		require.NoError(t, err)
		err = bstore.Put(ctx, testBlock)
		require.NoError(t, err)
		err = dhtClient.Provide(ctx, testCid, true)
		require.NoError(t, err)

		obj := test.Query(t, "http://localhost:1234", testCid.String(), hostAddr.String())

		obj.Value("ProviderRecordFromPeerInDHT").Boolean().IsTrue()
		obj.Value("ConnectionError").String().IsEmpty()
		obj.Value("ConnectionMaddrs").Array().ContainsAll(h.Addrs()[0])
		obj.Value("DataAvailableOverBitswap").Object().Value("Error").String().IsEmpty()
		obj.Value("DataAvailableOverBitswap").Object().Value("Found").Boolean().IsTrue()
		obj.Value("DataAvailableOverBitswap").Object().Value("Responded").Boolean().IsTrue()
	})

	t.Run("Data on reachable peer that's not advertised", func(t *testing.T) {
		testData := []byte(t.Name())
		mh, err := multihash.Sum(testData, multihash.SHA2_256, -1)
		require.NoError(t, err)
		testCid := cid.NewCidV1(cid.Raw, mh)
		testBlock, err := blocks.NewBlockWithCid(testData, testCid)
		require.NoError(t, err)
		err = bstore.Put(ctx, testBlock)
		require.NoError(t, err)

		obj := test.Query(t, "http://localhost:1234", testCid.String(), hostAddr.String())

		obj.Value("ProviderRecordFromPeerInDHT").Boolean().IsFalse()
		obj.Value("ConnectionError").String().IsEmpty()
		obj.Value("ConnectionMaddrs").Array().ContainsAll(h.Addrs()[0])
		obj.Value("DataAvailableOverBitswap").Object().Value("Error").String().IsEmpty()
		obj.Value("DataAvailableOverBitswap").Object().Value("Found").Boolean().IsTrue()
		obj.Value("DataAvailableOverBitswap").Object().Value("Responded").Boolean().IsTrue()
	})

	t.Run("Data that's advertised but not served", func(t *testing.T) {
		testData := []byte(t.Name())
		mh, err := multihash.Sum(testData, multihash.SHA2_256, -1)
		require.NoError(t, err)
		testCid := cid.NewCidV1(cid.Raw, mh)
		err = dhtClient.Provide(ctx, testCid, true)
		require.NoError(t, err)

		obj := test.Query(t, "http://localhost:1234", testCid.String(), hostAddr.String())

		obj.Value("ProviderRecordFromPeerInDHT").Boolean().IsTrue()
		obj.Value("ConnectionError").String().IsEmpty()
		obj.Value("ConnectionMaddrs").Array().ContainsAll(h.Addrs()[0])
		obj.Value("DataAvailableOverBitswap").Object().Value("Error").String().IsEmpty()
		obj.Value("DataAvailableOverBitswap").Object().Value("Found").Boolean().IsFalse()
		obj.Value("DataAvailableOverBitswap").Object().Value("Responded").Boolean().IsTrue()
	})

	t.Run("Data found on reachable peer with just cid", func(t *testing.T) {
		testData := []byte(t.Name())
		mh, err := multihash.Sum(testData, multihash.SHA2_256, -1)
		require.NoError(t, err)
		testCid := cid.NewCidV1(cid.Raw, mh)
		testBlock, err := blocks.NewBlockWithCid(testData, testCid)
		require.NoError(t, err)
		err = bstore.Put(ctx, testBlock)
		require.NoError(t, err)
		err = dhtClient.Provide(ctx, testCid, true)
		require.NoError(t, err)

		res := test.QueryCid(t, "http://localhost:1234", testCid.String())

		res.Length().IsEqual(1)
		res.Value(0).Object().Value("ID").String().IsEqual(h.ID().String())
		res.Value(0).Object().Value("ConnectionError").String().IsEmpty()
		testHostAddrs := h.Addrs()
		for _, addr := range testHostAddrs {
			if manet.IsPublicAddr(addr) {
				res.Value(0).Object().Value("Addrs").Array().ContainsAny(addr.String())
			}
		}

		res.Value(0).Object().Value("ConnectionMaddrs").Array()
		res.Value(0).Object().Value("DataAvailableOverBitswap").Object().Value("Error").String().IsEmpty()
		res.Value(0).Object().Value("DataAvailableOverBitswap").Object().Value("Found").Boolean().IsTrue()
		res.Value(0).Object().Value("DataAvailableOverBitswap").Object().Value("Responded").Boolean().IsTrue()
	})

	t.Run("Data found via HTTP", func(t *testing.T) {
		testData := []byte(t.Name())
		mh, err := multihash.Sum(testData, multihash.SHA2_256, -1)
		require.NoError(t, err)
		testCid := cid.NewCidV1(cid.Raw, mh)
		testBlock, err := blocks.NewBlockWithCid(testData, testCid)
		require.NoError(t, err)
		err = bstore.Put(ctx, testBlock)
		require.NoError(t, err)

		obj := test.Query(t, "http://localhost:1234", testCid.String(), httpPeerAddr.String(), "httpRetrieval=on")
		obj.Value("DataAvailableOverHTTP").Object().Value("Found").Boolean().IsTrue()
	})

	t.Run("Data found via HTTP with just CID", func(t *testing.T) {
		testData := []byte(t.Name())
		mh, err := multihash.Sum(testData, multihash.SHA2_256, -1)
		require.NoError(t, err)
		testCid := cid.NewCidV1(cid.Raw, mh)
		testBlock, err := blocks.NewBlockWithCid(testData, testCid)
		require.NoError(t, err)
		err = bstore.Put(ctx, testBlock)
		require.NoError(t, err)

		fmt.Println(ipniAddr)
		fmt.Println(httpAddr.String())
		obj := test.QueryCid(t, "http://localhost:1234", testCid.String(), "httpRetrieval=on", "ipniIndexer="+ipniAddr)
		obj.Value(0).Object().Value("DataAvailableOverHTTP").Object().Value("Found").Boolean().IsTrue()
	})

}
