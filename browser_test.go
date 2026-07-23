package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	madns "github.com/multiformats/go-multiaddr-dns"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/stretchr/testify/require"
)

// A real certificate hash, as go-libp2p advertises for WebTransport and
// WebRTC Direct. Anything shorter fails to parse as a multihash.
const certHash = "uEiDDq4_xNyDorSBQD8fTCjrPe1lFcVOSp8_MTAWuAwHijA"

// A well-formed certhash under SHA-512 instead. Browsers only ever compare
// SHA-256, so this is a hash they cannot match.
const certHashSHA512 = "uE0DuJrDdSvfnSaoajuPBCumSP2GJgHcuRz-IGaXUlA4NsnrBhfig4dX4T4i8iH_WexQ3MsMEzF-prY5vV_UAKKj_"

func TestClassifyAddr(t *testing.T) {
	for _, tc := range []struct {
		name          string
		addr          string
		webBrowser    bool
		serviceWorker bool
	}{
		{
			name: "plain websocket is refused by https pages",
			addr: "/ip4/137.184.243.187/tcp/3000/ws",
		},
		{
			name:          "secure websocket",
			addr:          "/dns4/example.com/tcp/443/wss",
			webBrowser:    true,
			serviceWorker: true,
		},
		{
			name:          "secure websocket in long form",
			addr:          "/dns4/example.com/tcp/443/tls/ws",
			webBrowser:    true,
			serviceWorker: true,
		},
		{
			// What kubo announces by default, with AutoTLS.ShortAddrs on.
			name:          "autotls libp2p.direct address, short form",
			addr:          "/dns4/1-2-3-4.k51qzi5uqu5dinf3g2p1isw96xmcpiaqgom41kj2vgsr0oj7g5xkhpi7fhtbf9.libp2p.direct/tcp/4001/tls/ws",
			webBrowser:    true,
			serviceWorker: true,
		},
		{
			name:          "autotls libp2p.direct address, resolved form",
			addr:          "/ip4/1.2.3.4/tcp/4001/tls/sni/1-2-3-4.k51qzi5uqu5dinf3g2p1isw96xmcpiaqgom41kj2vgsr0oj7g5xkhpi7fhtbf9.libp2p.direct/ws",
			webBrowser:    true,
			serviceWorker: true,
		},
		{
			name:          "webtransport with certificate hash",
			addr:          "/ip4/1.2.3.4/udp/4001/quic-v1/webtransport/certhash/" + certHash,
			webBrowser:    true,
			serviceWorker: true,
		},
		{
			name: "webtransport without certificate hash",
			addr: "/ip4/1.2.3.4/udp/4001/quic-v1/webtransport",
		},
		{
			name: "webtransport over legacy quic draft-29",
			addr: "/ip4/1.2.3.4/udp/4001/quic/webtransport/certhash/" + certHash,
		},
		{
			name: "webtransport with more certificate hashes than a browser matches",
			addr: "/ip4/1.2.3.4/udp/4001/quic-v1/webtransport/certhash/" + certHash + "/certhash/" + certHash + "/certhash/" + certHash,
		},
		{
			name: "certificate hash that is not sha-256",
			addr: "/ip4/1.2.3.4/udp/4001/quic-v1/webtransport/certhash/" + certHashSHA512,
		},
		{
			name:       "webrtc-direct works in a tab but not in a service worker",
			addr:       "/ip4/1.2.3.4/udp/4001/webrtc-direct/certhash/" + certHash,
			webBrowser: true,
		},
		{
			name: "webrtc-direct without certificate hash",
			addr: "/ip4/1.2.3.4/udp/4001/webrtc-direct",
		},
		{
			name: "webrtc-direct with a hash a browser cannot match",
			addr: "/ip4/1.2.3.4/udp/4001/webrtc-direct/certhash/" + certHashSHA512,
		},
		{
			name: "bare webrtc is signalling, not a dial target",
			addr: "/ip4/1.2.3.4/udp/4001/webrtc/certhash/" + certHash,
		},
		{
			name: "relayed webrtc depends on the relay",
			addr: "/ip4/1.2.3.4/tcp/4001/p2p/12D3KooWGjgvfDkpuVAQTaP4b1tYGqDhLTHrJ6mgtCjNJyNr3ZKm/p2p-circuit/webrtc",
		},
		{
			name: "relayed secure websocket depends on the relay",
			addr: "/dns4/example.com/tcp/443/wss/p2p/12D3KooWGjgvfDkpuVAQTaP4b1tYGqDhLTHrJ6mgtCjNJyNr3ZKm/p2p-circuit",
		},
		{
			name: "tcp",
			addr: "/ip4/1.2.3.4/tcp/4001",
		},
		{
			name: "quic",
			addr: "/ip4/1.2.3.4/udp/4001/quic-v1",
		},
		{
			name:          "https trustless gateway endpoint",
			addr:          "/dns/example.com/tcp/443/https",
			webBrowser:    true,
			serviceWorker: true,
		},
		{
			name:          "https endpoint in long form",
			addr:          "/dns/example.com/tcp/443/tls/http",
			webBrowser:    true,
			serviceWorker: true,
		},
		{
			name: "plain http endpoint",
			addr: "/dns/example.com/tcp/8080/http",
		},
		{
			name: "unresolved dnsaddr hides its transport",
			addr: "/dnsaddr/bitswap.example.com",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			addr, err := multiaddr.NewMultiaddr(tc.addr)
			require.NoError(t, err)

			webBrowser, serviceWorker := classifyAddr(addr)
			require.Equal(t, tc.webBrowser, webBrowser, "web browser")
			require.Equal(t, tc.serviceWorker, serviceWorker, "service worker")
		})
	}
}

// mustAddrs is the address list of a test case, as multiaddrs.
func mustAddrs(t *testing.T, addrs ...string) []multiaddr.Multiaddr {
	t.Helper()
	out := make([]multiaddr.Multiaddr, 0, len(addrs))
	for _, a := range addrs {
		addr, err := multiaddr.NewMultiaddr(a)
		require.NoError(t, err)
		out = append(out, addr)
	}
	return out
}

// withLoopbackAllowed lets a test use a stub server on 127.0.0.1, which the
// public-address filter would otherwise drop.
func withLoopbackAllowed(t *testing.T) {
	t.Helper()
	previous := isPublicAddr
	isPublicAddr = func(multiaddr.Multiaddr) bool { return true }
	t.Cleanup(func() { isPublicAddr = previous })
}

// withMockDNS points /dnsaddr resolution at fixtures for the duration of a test.
func withMockDNS(t *testing.T, txt map[string][]string) {
	t.Helper()
	resolver, err := madns.NewResolver(madns.WithDefaultResolver(&madns.MockResolver{TXT: txt}))
	require.NoError(t, err)

	previous := dnsaddrResolver
	dnsaddrResolver = resolver
	t.Cleanup(func() { dnsaddrResolver = previous })
}

func TestResolveDNSAddrs(t *testing.T) {
	const (
		thisPeer  = "12D3KooWGjgvfDkpuVAQTaP4b1tYGqDhLTHrJ6mgtCjNJyNr3ZKm"
		otherPeer = "12D3KooWRBy97UB99e3J6hiPesre1MZeuNQvfan4gBziswrRJsNK"
	)
	peerID, err := peer.Decode(thisPeer)
	require.NoError(t, err)

	t.Run("dnsaddr is expanded to the addresses behind it", func(t *testing.T) {
		withMockDNS(t, map[string][]string{
			"_dnsaddr.bitswap.example.com": {"dnsaddr=/dns4/bitswap.example.com/tcp/3000/ws/p2p/" + thisPeer},
		})

		resolved := resolveDNSAddrs(t.Context(), peerID, mustAddrs(t, "/dnsaddr/bitswap.example.com"))
		require.Len(t, resolved, 1)
		require.Equal(t, "/dns4/bitswap.example.com/tcp/3000/ws", resolved[0].String())
	})

	t.Run("addresses of other peers behind the same name are dropped", func(t *testing.T) {
		withMockDNS(t, map[string][]string{
			"_dnsaddr.bootstrap.example.com": {
				"dnsaddr=/dns4/one.example.com/tcp/443/wss/p2p/" + otherPeer,
				"dnsaddr=/dns4/two.example.com/tcp/3000/ws/p2p/" + thisPeer,
			},
		})

		resolved := resolveDNSAddrs(t.Context(), peerID, mustAddrs(t, "/dnsaddr/bootstrap.example.com"))
		require.Equal(t, []string{"/dns4/two.example.com/tcp/3000/ws"}, addrStrings(resolved))
	})

	t.Run("a dnsaddr pointing at another dnsaddr is followed", func(t *testing.T) {
		// The shape the libp2p bootstrappers use: one record per region,
		// each pointing at the record that names the transports.
		withMockDNS(t, map[string][]string{
			"_dnsaddr.bootstrap.example.com":      {"dnsaddr=/dnsaddr/sv15.bootstrap.example.com/p2p/" + thisPeer},
			"_dnsaddr.sv15.bootstrap.example.com": {"dnsaddr=/dns/sv15.bootstrap.example.com/tcp/443/wss/p2p/" + thisPeer},
		})

		resolved := resolveDNSAddrs(t.Context(), peerID, mustAddrs(t, "/dnsaddr/bootstrap.example.com"))
		require.Equal(t, []string{"/dns/sv15.bootstrap.example.com/tcp/443/wss"}, addrStrings(resolved))
	})

	t.Run("a dnsaddr loop stops instead of spinning", func(t *testing.T) {
		withMockDNS(t, map[string][]string{
			"_dnsaddr.loop.example.com": {"dnsaddr=/dnsaddr/loop.example.com/p2p/" + thisPeer},
		})

		resolved := resolveDNSAddrs(t.Context(), peerID, mustAddrs(t, "/dnsaddr/loop.example.com"))
		require.Equal(t, []string{"/dnsaddr/loop.example.com"}, addrStrings(resolved))
	})

	t.Run("addresses that already name a transport are left alone", func(t *testing.T) {
		withMockDNS(t, nil)

		in := mustAddrs(t, "/dns4/example.com/tcp/443/wss", "/ip4/1.2.3.4/tcp/4001")
		require.Equal(t, in, resolveDNSAddrs(t.Context(), peerID, in))
	})

	t.Run("a dnsaddr that does not resolve is kept as it is", func(t *testing.T) {
		withMockDNS(t, nil)

		in := mustAddrs(t, "/dnsaddr/missing.example.com")
		require.Equal(t, in, resolveDNSAddrs(t.Context(), peerID, in))
	})
}

// addrStrings renders a list of multiaddrs for comparison in assertions.
func addrStrings(addrs []multiaddr.Multiaddr) []string {
	out := make([]string, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, a.String())
	}
	return out
}

func TestCheckBrowserCompat(t *testing.T) {
	// No case here reaches a working address, so a host is never needed.
	noHost := func() (host.Host, error) {
		t.Fatal("a host was created for a peer with no browser-dialable address")
		return nil, nil
	}
	failingHost := func() (host.Host, error) {
		return nil, errNoHost
	}
	peerID, err := peer.Decode("12D3KooWGjgvfDkpuVAQTaP4b1tYGqDhLTHrJ6mgtCjNJyNr3ZKm")
	require.NoError(t, err)
	testCid, err := cid.Decode("bafkreidfxpjcmiazyhkkzu5nnmmhnbmvyrnvmhpvyd6nrxxlpqcfrnnpxi")
	require.NoError(t, err)

	t.Run("a plain websocket peer is reachable by neither", func(t *testing.T) {
		withMockDNS(t, nil)

		out := checkBrowserCompat(t.Context(), noHost, peerID, testCid, mustAddrs(t,
			"/ip4/137.184.243.187/tcp/3000/ws",
			"/ip4/137.184.243.187/tcp/4001",
		))

		require.True(t, out.Enabled)
		require.False(t, out.WebBrowserCompatible)
		require.False(t, out.ServiceWorkerCompatible)
		require.Empty(t, out.CandidateAddrs)
		require.Empty(t, out.VerifiedAddr)
		require.Empty(t, out.Error)
	})

	t.Run("a dnsaddr hiding a plain websocket is reachable by neither", func(t *testing.T) {
		withMockDNS(t, map[string][]string{
			"_dnsaddr.bitswap.example.com": {"dnsaddr=/dns4/bitswap.example.com/tcp/3000/ws/p2p/12D3KooWGjgvfDkpuVAQTaP4b1tYGqDhLTHrJ6mgtCjNJyNr3ZKm"},
		})

		out := checkBrowserCompat(t.Context(), noHost, peerID, testCid, mustAddrs(t, "/dnsaddr/bitswap.example.com"))

		require.True(t, out.Enabled)
		require.False(t, out.WebBrowserCompatible)
	})

	t.Run("an address that looks right but does not answer is not compatible", func(t *testing.T) {
		withMockDNS(t, nil)

		out := checkBrowserCompat(t.Context(), failingHost, peerID, testCid, mustAddrs(t,
			"/dns4/example.com/tcp/443/wss",
			"/ip4/1.2.3.4/udp/4001/webrtc-direct/certhash/"+certHash,
		))

		require.False(t, out.WebBrowserCompatible, "the dial failed, so nothing was confirmed")
		require.False(t, out.ServiceWorkerCompatible)
		require.Equal(t, []string{"/dns4/example.com/tcp/443/wss", "/ip4/1.2.3.4/udp/4001/webrtc-direct/certhash/" + certHash}, out.CandidateAddrs)
		require.Empty(t, out.VerifiedAddr)
		require.Contains(t, out.Error, errNoHost.Error())
	})

	t.Run("an https endpoint that allows cross-origin reads is reachable", func(t *testing.T) {
		withMockDNS(t, nil)
		withLoopbackAllowed(t)

		server := corsTestServer(t, "*", true)
		out := checkBrowserCompat(t.Context(), noHost, peerID, testCid, []multiaddr.Multiaddr{httpMaddr(t, server)})

		require.True(t, out.WebBrowserCompatible)
		require.True(t, out.ServiceWorkerCompatible, "fetch() works in a Service Worker too")
		require.True(t, out.CORS.Enabled)
		require.True(t, out.CORS.Allowed)
		require.Empty(t, out.CORS.Error)
	})

	t.Run("an https endpoint that refuses them is not, and says so", func(t *testing.T) {
		withMockDNS(t, nil)
		withLoopbackAllowed(t)

		server := corsTestServer(t, "", true)
		out := checkBrowserCompat(t.Context(), noHost, peerID, testCid, []multiaddr.Multiaddr{httpMaddr(t, server)})

		require.False(t, out.WebBrowserCompatible)
		require.True(t, out.CORS.Enabled)
		require.False(t, out.CORS.Allowed)
		require.Contains(t, out.CORS.Error, "Access-Control-Allow-Origin")
	})

	t.Run("CORS is reported even when a libp2p address already works", func(t *testing.T) {
		withMockDNS(t, nil)
		withLoopbackAllowed(t)

		// The dial fails here, but the point is that the HTTPS endpoint was
		// asked at all rather than skipped once there was something to dial.
		server := corsTestServer(t, "*", true)
		out := checkBrowserCompat(t.Context(), failingHost, peerID, testCid, []multiaddr.Multiaddr{
			httpMaddr(t, server),
			mustAddrs(t, "/dns4/example.com/tcp/443/wss")[0],
		})

		require.True(t, out.CORS.Enabled)
		require.True(t, out.CORS.Allowed)
		require.True(t, out.WebBrowserCompatible, "the HTTPS endpoint is a way in on its own")
	})

	t.Run("peers without an https endpoint report no CORS answer at all", func(t *testing.T) {
		withMockDNS(t, nil)

		out := checkBrowserCompat(t.Context(), failingHost, peerID, testCid, mustAddrs(t, "/dns4/example.com/tcp/443/wss"))

		require.False(t, out.CORS.Enabled)
		require.False(t, out.CORS.Allowed)
		require.Empty(t, out.CORS.Error)
	})

	t.Run("private addresses are not browser-usable from here", func(t *testing.T) {
		withMockDNS(t, nil)

		out := checkBrowserCompat(t.Context(), noHost, peerID, testCid, mustAddrs(t,
			"/ip4/192.168.1.10/tcp/443/wss",
			"/dns4/localhost/tcp/443/wss",
		))

		require.False(t, out.WebBrowserCompatible)
		require.Empty(t, out.CandidateAddrs)
	})

}

// corsTestServer answers block requests, sending allowOrigin as the
// Access-Control-Allow-Origin header. An empty allowOrigin sends none, and
// answerPreflight=false makes it reject OPTIONS the way many servers do.
func corsTestServer(t *testing.T, allowOrigin string, answerPreflight bool) *httptest.Server {
	t.Helper()
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions && !answerPreflight {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if allowOrigin != "" {
			w.Header().Set("Access-Control-Allow-Origin", allowOrigin)
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Write([]byte("block"))
	}))
	t.Cleanup(server.Close)

	// The stub serves its own certificate, so the probe has to be told to
	// trust it the way a browser trusts a public CA.
	previous := corsProbeClient
	corsProbeClient = server.Client()
	t.Cleanup(func() { corsProbeClient = previous })

	return server
}

// httpMaddr is the /https multiaddr of a test server.
func httpMaddr(t *testing.T, server *httptest.Server) multiaddr.Multiaddr {
	t.Helper()
	addr, err := manet.FromNetAddr(server.Listener.Addr())
	require.NoError(t, err)
	httpComponent, err := multiaddr.NewMultiaddr("/https")
	require.NoError(t, err)
	return addr.Encapsulate(httpComponent)
}

func TestProbeCORS(t *testing.T) {
	testCid, err := cid.Decode("bafkreidfxpjcmiazyhkkzu5nnmmhnbmvyrnvmhpvyd6nrxxlpqcfrnnpxi")
	require.NoError(t, err)

	t.Run("a wildcard origin lets any page read the block", func(t *testing.T) {
		server := corsTestServer(t, "*", true)
		require.NoError(t, probeCORS(t.Context(), httpMaddr(t, server), testCid))
	})

	t.Run("the probe origin echoed back also counts", func(t *testing.T) {
		server := corsTestServer(t, corsProbeOrigin, true)
		require.NoError(t, probeCORS(t.Context(), httpMaddr(t, server), testCid))
	})

	t.Run("no header means a browser may not read the response", func(t *testing.T) {
		server := corsTestServer(t, "", true)
		err := probeCORS(t.Context(), httpMaddr(t, server), testCid)
		require.ErrorContains(t, err, "Access-Control-Allow-Origin")
	})

	t.Run("another site's origin is not ours", func(t *testing.T) {
		server := corsTestServer(t, "https://example.com", true)
		err := probeCORS(t.Context(), httpMaddr(t, server), testCid)
		require.ErrorContains(t, err, "Access-Control-Allow-Origin")
	})

	t.Run("refusing the preflight is fine when the block request is allowed", func(t *testing.T) {
		// A trustless gateway fetch is a simple request, which browsers never
		// preflight, so OPTIONS support is not what decides this.
		server := corsTestServer(t, "*", false)
		require.NoError(t, probeCORS(t.Context(), httpMaddr(t, server), testCid))
	})

	t.Run("an endpoint that is not there reports the failure", func(t *testing.T) {
		server := corsTestServer(t, "*", true)
		addr := httpMaddr(t, server)
		server.Close()

		err := probeCORS(t.Context(), addr, testCid)
		require.ErrorContains(t, err, "request failed")
	})
}

// errNoHost stands in for a host that could not be created.
var errNoHost = errors.New("no host available")
