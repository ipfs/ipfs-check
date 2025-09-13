package main

import (
	"context"
	"log"
	"time"

	"github.com/ipfs/boxo/ipns"
	"github.com/ipfs/go-cid"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	"github.com/libp2p/go-libp2p-kad-dht/amino"
	"github.com/libp2p/go-libp2p-kad-dht/fullrt"
	dhtpb "github.com/libp2p/go-libp2p-kad-dht/pb"
	record "github.com/libp2p/go-libp2p-record"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-libp2p/core/routing"
	"github.com/libp2p/go-msgio/pbio"
)

func dhtProtocolMessenger(proto protocol.ID, h host.Host) (*dhtpb.ProtocolMessenger, error) {
	ms := &dhtMsgSender{
		h:         h,
		protocols: []protocol.ID{proto},
		timeout:   time.Second * 5,
	}
	messenger, err := dhtpb.NewProtocolMessenger(ms)
	if err != nil {
		return nil, err
	}

	return messenger, nil
}

// dhtMsgSender handles sending dht wire protocol messages to a given peer
type dhtMsgSender struct {
	h         host.Host
	protocols []protocol.ID
	timeout   time.Duration
}

// SendRequest sends a peer a message and waits for its response
func (ms *dhtMsgSender) SendRequest(ctx context.Context, p peer.ID, pmes *dhtpb.Message) (*dhtpb.Message, error) {
	s, err := ms.h.NewStream(ctx, p, ms.protocols...)
	if err != nil {
		return nil, err
	}

	w := pbio.NewDelimitedWriter(s)
	if err := w.WriteMsg(pmes); err != nil {
		return nil, err
	}

	r := pbio.NewDelimitedReader(s, network.MessageSizeMax)
	tctx, cancel := context.WithTimeout(ctx, ms.timeout)
	defer cancel()
	defer func() { _ = s.Close() }()

	msg := new(dhtpb.Message)
	if err := ctxReadMsg(tctx, r, msg); err != nil {
		_ = s.Reset()
		return nil, err
	}

	return msg, nil
}

func ctxReadMsg(ctx context.Context, rc pbio.ReadCloser, mes *dhtpb.Message) error {
	errc := make(chan error, 1)
	go func(r pbio.ReadCloser) {
		defer close(errc)
		err := r.ReadMsg(mes)
		errc <- err
	}(rc)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// SendMessage sends a peer a message without waiting on a response
func (ms *dhtMsgSender) SendMessage(ctx context.Context, p peer.ID, pmes *dhtpb.Message) error {
	s, err := ms.h.NewStream(ctx, p, ms.protocols...)
	if err != nil {
		return err
	}
	defer func() { _ = s.Close() }()

	w := pbio.NewDelimitedWriter(s)
	return w.WriteMsg(pmes)
}

var _ dhtpb.MessageSender = (*dhtMsgSender)(nil)

// bundledDHT combines standard and accelerated DHT clients
// It starts with standard DHT for immediate functionality
// and switches to accelerated DHT when it becomes ready
type bundledDHT struct {
	standard *dht.IpfsDHT
	fullRT   *fullrt.FullRT
}

// getDHT returns the accelerated DHT if ready, otherwise standard DHT
func (b *bundledDHT) getDHT() kademlia {
	if b.fullRT != nil && b.fullRT.Ready() {
		return b.fullRT
	}
	return b.standard
}

// Implement kademlia interface methods

func (b *bundledDHT) Provide(ctx context.Context, c cid.Cid, brdcst bool) error {
	return b.getDHT().Provide(ctx, c, brdcst)
}

func (b *bundledDHT) FindProvidersAsync(ctx context.Context, c cid.Cid, i int) <-chan peer.AddrInfo {
	return b.getDHT().FindProvidersAsync(ctx, c, i)
}

func (b *bundledDHT) FindPeer(ctx context.Context, id peer.ID) (peer.AddrInfo, error) {
	return b.getDHT().FindPeer(ctx, id)
}

func (b *bundledDHT) PutValue(ctx context.Context, k string, v []byte, option ...routing.Option) error {
	return b.getDHT().PutValue(ctx, k, v, option...)
}

func (b *bundledDHT) GetValue(ctx context.Context, s string, option ...routing.Option) ([]byte, error) {
	return b.getDHT().GetValue(ctx, s, option...)
}

func (b *bundledDHT) SearchValue(ctx context.Context, s string, option ...routing.Option) (<-chan []byte, error) {
	return b.getDHT().SearchValue(ctx, s, option...)
}

func (b *bundledDHT) Bootstrap(ctx context.Context) error {
	// Bootstrap the standard DHT
	return b.standard.Bootstrap(ctx)
}

func (b *bundledDHT) GetClosestPeers(ctx context.Context, key string) ([]peer.ID, error) {
	return b.getDHT().GetClosestPeers(ctx, key)
}

// Ensure bundledDHT implements kademlia interface
var _ kademlia = (*bundledDHT)(nil)

// setupDHT initializes the DHT client(s) based on configuration
func setupDHT(ctx context.Context, h host.Host, acceleratedDHT bool) (kademlia, error) {
	// Common validators for both DHT types
	validators := record.NamespacedValidator{
		"pk":   record.PublicKeyValidator{},
		"ipns": ipns.Validator{},
	}

	// Always create standard DHT first (starts immediately)
	standardDHT, err := dht.New(ctx, h,
		dht.Mode(dht.ModeClient),
		dht.Validator(validators),
		dht.BootstrapPeers(dht.GetDefaultBootstrapPeerAddrInfos()...))
	if err != nil {
		return nil, err
	}

	// If not using accelerated DHT, return standard DHT only
	if !acceleratedDHT {
		return standardDHT, nil
	}

	// Create accelerated DHT in parallel (non-blocking)
	fullRTDHT, err := fullrt.NewFullRT(h, dht.DefaultPrefix,
		fullrt.DHTOption(
			dht.BucketSize(amino.DefaultBucketSize),
			dht.Validator(validators),
			dht.BootstrapPeers(dht.GetDefaultBootstrapPeerAddrInfos()...),
			dht.Mode(dht.ModeClient),
		))
	if err != nil {
		return nil, err
	}

	// Create bundled DHT that will switch automatically
	bundled := &bundledDHT{
		standard: standardDHT,
		fullRT:   fullRTDHT,
	}

	// Start monitoring for readiness in background
	go monitorDHTReadiness(bundled)

	return bundled, nil
}

// monitorDHTReadiness logs DHT status and notifies when accelerated DHT is ready
func monitorDHTReadiness(b *bundledDHT) {
	if !b.fullRT.Ready() {
		log.Printf("Please wait, initializing accelerated-dht client, performance will improve soon (mapping Amino DHT takes 5 mins or more)")
	}

	// Check readiness every 10 seconds
	for !b.fullRT.Ready() {
		time.Sleep(10 * time.Second)
	}

	log.Printf("Accelerated DHT client is ready - DHT queries will now have improved performance")
}
