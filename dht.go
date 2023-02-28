package main

import (
	"context"
	"time"

	dhtpb "github.com/libp2p/go-libp2p-kad-dht/pb"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/libp2p/go-msgio/protoio"
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

	w := protoio.NewDelimitedWriter(s)
	if err := w.WriteMsg(pmes); err != nil {
		return nil, err
	}

	r := protoio.NewDelimitedReader(s, network.MessageSizeMax)
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

func ctxReadMsg(ctx context.Context, rc protoio.ReadCloser, mes *dhtpb.Message) error {
	errc := make(chan error, 1)
	go func(r protoio.ReadCloser) {
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

	w := protoio.NewDelimitedWriter(s)
	return w.WriteMsg(pmes)
}

var _ dhtpb.MessageSender = (*dhtMsgSender)(nil)
