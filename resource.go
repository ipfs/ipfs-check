package main

import (
	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/network"
	rcmgr "github.com/libp2p/go-libp2p/p2p/host/resource-manager"
)

func NewResourceManager() (network.ResourceManager, error) {
	// Copied from:
	// https://github.com/libp2p/go-libp2p/blob/98837aad1591a9c5834fb6589318ee443cd12fe3/p2p/host/resource-manager/README.md

	scalingLimits := rcmgr.DefaultLimits
	libp2p.SetDefaultServiceLimits(&scalingLimits)

	scaledDefaultLimits := scalingLimits.AutoScale()

	cfg := rcmgr.PartialLimitConfig{
		System: rcmgr.ResourceLimits{
			ConnsOutbound:  rcmgr.Unlimited,
			Conns:          rcmgr.Unlimited,
			ConnsInbound:   rcmgr.Unlimited,
		},
	}

	limits := cfg.Build(scaledDefaultLimits)
	limiter := rcmgr.NewFixedLimiter(limits)
	rm, err := rcmgr.NewResourceManager(limiter)

	return rm, err
}
