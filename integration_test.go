package main

import (
	"context"
	"github.com/aschmahmann/ipfs-check/test"
	bsnet "github.com/ipfs/boxo/bitswap/network"
	bsserver "github.com/ipfs/boxo/bitswap/server"
	"github.com/ipfs/boxo/blockstore"
	blocks "github.com/ipfs/go-block-format"
	"github.com/ipfs/go-cid"
	"github.com/ipfs/go-datastore"
	dssync "github.com/ipfs/go-datastore/sync"
	"github.com/libp2p/go-libp2p"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	mplex "github.com/libp2p/go-libp2p-mplex"
	routinghelpers "github.com/libp2p/go-libp2p-routing-helpers"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/multiformats/go-multihash"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

func TestBasicIntegration(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	testDHTPrefix := protocol.TestingID
	testDHTID := protocol.TestingID + "/kad/1.0.0"

	dhtHost, err := libp2p.New()
	require.NoError(t, err)
	defer dhtHost.Close()
	dhtServer, err := dht.New(ctx, dhtHost, dht.Mode(dht.ModeServer), dht.ProtocolPrefix(testDHTPrefix))
	require.NoError(t, err)
	defer dhtServer.Close()

	go func() {
		rm, err := NewResourceManager()
		require.NoError(t, err)

		c, err := connmgr.NewConnManager(600, 900, connmgr.WithGracePeriod(time.Second*30))
		require.NoError(t, err)

		queryHost, err := libp2p.New(
			libp2p.DefaultMuxers,
			libp2p.Muxer(mplex.ID, mplex.DefaultTransport),
			libp2p.ConnectionManager(c),
			libp2p.ResourceManager(rm),
			libp2p.EnableHolePunching(),
		)
		require.NoError(t, err)

		pm, err := dhtProtocolMessenger(testDHTID, queryHost)
		require.NoError(t, err)
		queryDHT, err := dht.New(ctx, queryHost, dht.ProtocolPrefix(testDHTPrefix), dht.BootstrapPeers(peer.AddrInfo{ID: dhtHost.ID(), Addrs: dhtHost.Addrs()}))
		require.NoError(t, err)

		d := &daemon{
			h:            queryHost,
			dht:          queryDHT,
			dhtMessenger: pm,
			createTestHost: func() (host.Host, error) {
				return libp2p.New(libp2p.DefaultMuxers,
					libp2p.Muxer(mplex.ID, mplex.DefaultTransport),
					libp2p.EnableHolePunching())
			},
		}
		_ = startServer(ctx, d, ":1234")
	}()

	h, err := libp2p.New()
	defer h.Close()
	require.NoError(t, err)
	bn := bsnet.NewFromIpfsHost(h, routinghelpers.Null{})
	bstore := blockstore.NewBlockstore(dssync.MutexWrap(datastore.NewMapDatastore()))
	bswap := bsserver.New(ctx, bn, bstore)
	bn.Start(bswap)
	defer bswap.Close()
	dhtClient, err := dht.New(ctx, h, dht.ProtocolPrefix(testDHTPrefix), dht.Mode(dht.ModeClient), dht.BootstrapPeers(peer.AddrInfo{ID: dhtHost.ID(), Addrs: dhtHost.Addrs()}))
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

		obj.Value("CidInDHT").Boolean().IsTrue()
		obj.Value("ConnectionError").String().IsEmpty()
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

		obj.Value("CidInDHT").Boolean().IsFalse()
		obj.Value("ConnectionError").String().IsEmpty()
		obj.Value("DataAvailableOverBitswap").Object().Value("Error").String().IsEmpty()
		obj.Value("DataAvailableOverBitswap").Object().Value("Found").Boolean().IsTrue()
		obj.Value("DataAvailableOverBitswap").Object().Value("Responded").Boolean().IsTrue()
	})

	t.Run("Data that's advertised but not served", func(t *testing.T) {
		testData := []byte(t.Name())
		mh, err := multihash.Sum(testData, multihash.SHA2_256, -1)
		require.NoError(t, err)
		testCid := cid.NewCidV1(cid.Raw, mh)
		require.NoError(t, err)
		err = dhtClient.Provide(ctx, testCid, true)
		require.NoError(t, err)

		obj := test.Query(t, "http://localhost:1234", testCid.String(), hostAddr.String())

		obj.Value("CidInDHT").Boolean().IsTrue()
		obj.Value("ConnectionError").String().IsEmpty()
		obj.Value("DataAvailableOverBitswap").Object().Value("Error").String().IsEmpty()
		obj.Value("DataAvailableOverBitswap").Object().Value("Found").Boolean().IsFalse()
		obj.Value("DataAvailableOverBitswap").Object().Value("Responded").Boolean().IsTrue()
	})
}
