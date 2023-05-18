package main

import (
	"github.com/libp2p/go-libp2p/core/connmgr"
	"github.com/libp2p/go-libp2p/core/control"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	ma "github.com/multiformats/go-multiaddr"
	manet "github.com/multiformats/go-multiaddr/net"
)

type privateAddrFilterConnectionGater struct{}

var _ connmgr.ConnectionGater = (*privateAddrFilterConnectionGater)(nil)

func (f *privateAddrFilterConnectionGater) InterceptAddrDial(_ peer.ID, addr ma.Multiaddr) (allow bool) {
	return manet.IsPublicAddr(addr)
}

func (f *privateAddrFilterConnectionGater) InterceptPeerDial(p peer.ID) (allow bool) {
	return true
}

func (f *privateAddrFilterConnectionGater) InterceptAccept(connAddr network.ConnMultiaddrs) (allow bool) {
	return manet.IsPublicAddr(connAddr.RemoteMultiaddr())
}

func (f *privateAddrFilterConnectionGater) InterceptSecured(_ network.Direction, _ peer.ID, connAddr network.ConnMultiaddrs) (allow bool) {
	return manet.IsPublicAddr(connAddr.RemoteMultiaddr())
}

func (f *privateAddrFilterConnectionGater) InterceptUpgraded(_ network.Conn) (allow bool, reason control.DisconnectReason) {
	return true, 0
}
