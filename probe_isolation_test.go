package main

import (
	"crypto/rand"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p"
	libp2pcrypto "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/require"
)

// The next check must dial a failing endpoint again. By default httpnet keeps
// per-host backoff for the whole process, so one failed probe would answer
// every later check of that host for a minute. checkHTTPRetrieval opts out.
// This test fails if the private cooldown tracker is dropped.
func TestHTTPCheckDoesNotReuseCooldownAcrossChecks(t *testing.T) {
	var probes atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		probes.Add(1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, port, err := net.SplitHostPort(srv.Listener.Addr().String())
	require.NoError(t, err)
	maddr, err := multiaddr.NewMultiaddr(fmt.Sprintf("/ip4/127.0.0.1/tcp/%s/http", port))
	require.NoError(t, err)

	c, err := cid.Decode("bafkqaaa")
	require.NoError(t, err)

	_, pub, err := libp2pcrypto.GenerateEd25519Key(rand.Reader)
	require.NoError(t, err)
	pid, err := peer.IDFromPublicKey(pub)
	require.NoError(t, err)
	pinfo := peer.AddrInfo{ID: pid, Addrs: []multiaddr.Multiaddr{maddr}}

	probesPerCheck := int64(0)
	for i := range 2 {
		h, err := libp2p.New(libp2p.NoListenAddrs)
		require.NoError(t, err)

		before := probes.Load()
		out := checkHTTPRetrieval(t.Context(), h, c, pinfo, true)
		made := probes.Load() - before
		require.NoError(t, h.Close())

		require.False(t, out.Connected, "check %d: the endpoint answers 500, so it must not read as connected", i)
		require.Positive(t, made, "check %d did not dial: a cooldown from an earlier check answered it", i)
		require.NotContains(t, out.Error, "cooldown", "check %d reported a cooldown instead of what it found", i)

		if i == 0 {
			probesPerCheck = made
			continue
		}
		require.Equal(t, probesPerCheck, made, "check %d probed a different number of times than the first", i)
	}
}
