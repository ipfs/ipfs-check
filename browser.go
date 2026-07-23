package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ipfs/boxo/bitswap/network"
	"github.com/ipfs/go-cid"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	madns "github.com/multiformats/go-multiaddr-dns"
	manet "github.com/multiformats/go-multiaddr/net"
	"github.com/multiformats/go-multihash"
)

// browserDialTimeout bounds the extra dial that confirms a peer really answers
// on a browser-dialable address. It runs alongside the Bitswap probe, so it
// gets the same order of budget as the main dial without extending the check.
const browserDialTimeout = 30 * time.Second

// corsProbeTimeout bounds the two HTTP requests that learn whether an endpoint
// allows cross-origin reads.
const corsProbeTimeout = 10 * time.Second

// corsProbeOrigin is the origin the probe claims to come from. Any origin that
// is not the endpoint's own answers the question, and this one names the tool
// asking, which is friendlier in a server log than something made up.
const corsProbeOrigin = "https://check.ipfs.network"

// dnsaddrResolveTimeout bounds the TXT lookups needed to see through a
// /dnsaddr record. Resolution only decides how an address is labelled, so a
// slow resolver costs a label, not the check.
const dnsaddrResolveTimeout = 10 * time.Second

// dnsaddrMaxDepth bounds how many chained /dnsaddr records are followed.
// Two is what the libp2p bootstrappers use; the rest is headroom.
const dnsaddrMaxDepth = 4

// dnsaddrMaxAddrs bounds the expansion itself, since each round can multiply
// the address count and the records are supplied by whoever runs the name.
const dnsaddrMaxAddrs = 32

// corsProbeClient makes the two cross-origin requests. Tests replace it with a
// client that trusts a stub server's certificate.
var corsProbeClient = &http.Client{Timeout: corsProbeTimeout}

// dnsaddrResolver reads the TXT records behind /dnsaddr addresses. Tests
// replace it with a resolver backed by fixtures.
var dnsaddrResolver = madns.DefaultResolver

// isPublicAddr decides which addresses this tool is willing to judge. Tests
// replace it so a loopback stub server can stand in for a real endpoint.
var isPublicAddr = manet.IsPublicAddr

// BrowserCheckOutput reports whether a peer can serve data to libp2p nodes
// running inside a web browser, and to the narrower environment of a Service
// Worker (which is what powers https://inbrowser.link and ipfs-webui).
//
// A browser can only open connections the platform gives it an API for:
// Secure WebSockets, WebTransport and WebRTC. Raw TCP and QUIC are not
// reachable at all, and a plaintext /ws address is refused outright on an
// HTTPS page, because a secure page may not open insecure connections.
//
// A Service Worker is narrower still: it has WebSocket and WebTransport, but
// no WebRTC, so a peer whose only browser-dialable address is /webrtc-direct
// can be reached from a tab and is invisible to a Service Worker gateway.
//
// An HTTPS endpoint counts too: a browser can fetch blocks from it over plain
// HTTP as a trustless gateway, no libp2p transport involved.
// See https://specs.ipfs.tech/http-gateways/trustless-gateway/
//
// The two verdicts report what was reached, not what was advertised. A peer
// announcing a perfect /wss address that never answers is not compatible with
// anything.
type BrowserCheckOutput struct {
	Enabled                 bool
	WebBrowserCompatible    bool
	ServiceWorkerCompatible bool
	// VerifiedAddr is the address that worked: one a libp2p handshake
	// completed over, or an HTTPS endpoint that allows cross-origin reads.
	VerifiedAddr string
	// CandidateAddrs are the announced addresses a browser could have used.
	// They say what was tried, not what a browser can rely on.
	CandidateAddrs []string
	// Error is why none of the candidates worked.
	Error string
	// CORS reports what the peer's HTTPS endpoints said when asked as a page
	// from another origin.
	CORS CORSCheckOutput
}

// CORSCheckOutput reports whether a web page served from somewhere else is
// allowed to read blocks from an HTTPS endpoint.
//
// This is the whole question for an HTTP provider. The endpoint can answer the
// request perfectly and the browser will still throw the bytes away unless the
// response carries an Access-Control-Allow-Origin header that covers the
// page's origin.
type CORSCheckOutput struct {
	// Enabled is set when the peer had an HTTPS endpoint to ask.
	Enabled bool
	// Allowed is set when a cross-origin page may read the response.
	Allowed bool
	// Error is why it may not.
	Error string
}

// classifyAddr reports whether a browser tab and a Service Worker can dial
// addr, from the address alone.
//
// The rules follow what browsers expose, described at
// https://web.archive.org/web/20251118040510/https://connectivity.libp2p.io/#websocket
// and the sections next to it:
//
//   - /wss and its long form /tls/ws (including both shapes AutoTLS produces):
//     dialable everywhere, given a CA-trusted certificate.
//   - /quic-v1/webtransport with one or two SHA-256 /certhash components:
//     dialable everywhere. The certificate hash is how a browser accepts a node
//     that has no CA-signed certificate.
//   - /webrtc-direct with a SHA-256 /certhash: dialable from a tab only.
//   - /https and its long form /tls/http: usable everywhere, as a trustless
//     gateway fetched over HTTPS rather than a libp2p transport.
//   - plain /ws, plain /http, raw TCP, QUIC: not usable from a browser at all.
//
// The certhash conditions are what a browser actually enforces rather than what
// the multiaddr grammar allows: a WebTransport address with no certhash, with
// three of them, over legacy /quic, or hashed with anything but SHA-256 parses
// fine and cannot be dialed.
//
// Relayed (/p2p-circuit) addresses report false: whether a browser can use one
// depends on the relay's own addresses, which are not part of this record. That
// check comes first, because a relayed address often carries a /wss prefix for
// the relay itself.
func classifyAddr(addr multiaddr.Multiaddr) (webBrowser, serviceWorker bool) {
	var tls, ws, wss, webTransport, webRTCDirect, circuit, https, http, quicV1 bool
	var certHashes int
	var certHashUsable = true

	multiaddr.ForEach(addr, func(c multiaddr.Component) bool {
		switch c.Protocol().Code {
		case multiaddr.P_TLS:
			tls = true
		case multiaddr.P_WS:
			ws = true
		case multiaddr.P_WSS:
			wss = true
		case multiaddr.P_QUIC_V1:
			quicV1 = true
		case multiaddr.P_WEBTRANSPORT:
			webTransport = true
		case multiaddr.P_WEBRTC_DIRECT:
			webRTCDirect = true
		case multiaddr.P_CIRCUIT:
			circuit = true
		case multiaddr.P_HTTPS:
			https = true
		case multiaddr.P_HTTP:
			http = true
		case multiaddr.P_CERTHASH:
			certHashes++
			// Browsers compare the hash with SHA-256 and nothing else, so a
			// certhash under any other function is one they cannot match.
			decoded, err := multihash.Decode(c.RawValue())
			if err != nil || decoded.Code != multihash.SHA2_256 {
				certHashUsable = false
			}
		}
		return true
	})

	usableCertHash := certHashes > 0 && certHashUsable

	switch {
	case circuit:
		return false, false
	case wss, tls && ws:
		return true, true
	case webTransport:
		// Legacy /quic (draft-29) is a different protocol code that browsers
		// never speak, and js-libp2p matches at most two certhashes.
		usable := quicV1 && usableCertHash && certHashes <= 2
		return usable, usable
	case https, tls && http:
		return true, true
	case webRTCDirect:
		// Browser only: WebRTC has no API inside a Service Worker.
		return usableCertHash, false
	default:
		// Includes bare /webrtc, which is browser-to-browser signalling over
		// an existing relayed connection rather than something to dial.
		return false, false
	}
}

// isHTTPTransport reports whether addr is an HTTP endpoint a browser would
// fetch from, rather than an address it would open a libp2p connection over.
// The two are verified differently: one is dialed, the other is asked whether
// it allows cross-origin reads.
func isHTTPTransport(addr multiaddr.Multiaddr) bool {
	for _, p := range addr.Protocols() {
		if p.Code == multiaddr.P_HTTPS || p.Code == multiaddr.P_HTTP {
			return true
		}
	}
	return false
}

// resolveDNSAddrs expands /dnsaddr entries so their transports become visible.
// A /dnsaddr record names a host and nothing else, so an address like
// /dnsaddr/bitswap.example.com says nothing about whether a browser can use
// it until the TXT record behind it is read.
//
// The peer ID is part of the query, because one TXT record set can list
// addresses for many peers (bootstrap.libp2p.io is the usual example). Without
// it, another peer's addresses would be read as this peer's.
//
// Only /dnsaddr is expanded. /dns4, /dns6 and /dns already carry their
// transport in the address, so resolving them would add IP addresses without
// changing any verdict.
//
// Addresses that fail to resolve are kept as they are, so an unreachable
// resolver cannot turn a peer's other addresses into a verdict of its own.
// Returned addresses carry no /p2p component, which is what dialing wants.
func resolveDNSAddrs(ctx context.Context, p peer.ID, addrs []multiaddr.Multiaddr) []multiaddr.Multiaddr {
	ctx, cancel := context.WithTimeout(ctx, dnsaddrResolveTimeout)
	defer cancel()

	p2pAddr, err := multiaddr.NewMultiaddr("/p2p/" + p.String())
	if err != nil {
		log.Printf("could not build /p2p address for %s: %v", p, err)
		return addrs
	}

	// One round of resolution can hand back another /dnsaddr: bootstrap.libp2p.io
	// points at sv15.bootstrap.libp2p.io, and only the second record names the
	// transports. Depth is capped so a record pointing at itself cannot spin.
	work := addrs
	for depth := 0; depth < dnsaddrMaxDepth; depth++ {
		next := make([]multiaddr.Multiaddr, 0, len(work))
		expanded := false

		for _, addr := range work {
			if !isDNSAddr(addr) {
				next = append(next, addr)
				continue
			}
			query := addr
			if _, id := peer.SplitAddr(addr); id == "" {
				query = addr.Encapsulate(p2pAddr)
			}
			resolved, err := dnsaddrResolver.Resolve(ctx, query)
			if err != nil || len(resolved) == 0 {
				if err != nil {
					log.Printf("could not resolve %s for browser check: %v", addr, err)
				}
				next = append(next, addr)
				continue
			}
			expanded = true
			next = append(next, resolved...)
		}

		work = next
		if len(work) > dnsaddrMaxAddrs {
			log.Printf("stopping /dnsaddr expansion for %s at %d addresses", p, len(work))
			work = work[:dnsaddrMaxAddrs]
			break
		}
		if !expanded {
			break
		}
	}

	out := make([]multiaddr.Multiaddr, 0, len(work))
	seen := make(map[string]struct{}, len(work))
	for _, addr := range work {
		transport, id := peer.SplitAddr(addr)
		if id != "" && id != p {
			continue
		}
		s := transport.String()
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, transport)
	}
	return out
}

// isDNSAddr reports whether addr still hides its transport behind a /dnsaddr
// name.
func isDNSAddr(addr multiaddr.Multiaddr) bool {
	protos := addr.Protocols()
	return len(protos) > 0 && protos[0].Code == multiaddr.P_DNSADDR
}

// checkBrowserCompat answers whether a browser can retrieve from this peer, by
// finding the addresses a browser could use and then using one of them.
//
// The verdict is what worked, never what was announced. An address can look
// right and be unusable: an expired certificate on a /wss address, a
// /webrtc-direct port that never answers, an HTTPS endpoint that refuses
// cross-origin requests. All three announce a browser transport and none of
// them let a page fetch a block.
func checkBrowserCompat(ctx context.Context, newHost func() (host.Host, error), p peer.ID, c cid.Cid, addrs []multiaddr.Multiaddr) BrowserCheckOutput {
	out := BrowserCheckOutput{Enabled: true}

	var dialable, httpEndpoints []multiaddr.Multiaddr
	for _, addr := range resolveDNSAddrs(ctx, p, addrs) {
		// Checked after resolution, since a /dnsaddr can expand to a LAN
		// address. This tool answers from the public internet, so an address
		// it cannot reach is not one to make promises about.
		if !isPublicAddr(addr) {
			continue
		}
		if webBrowser, _ := classifyAddr(addr); !webBrowser {
			continue
		}
		out.CandidateAddrs = append(out.CandidateAddrs, addr.String())
		if isHTTPTransport(addr) {
			httpEndpoints = append(httpEndpoints, addr)
		} else {
			dialable = append(dialable, addr)
		}
	}

	if len(out.CandidateAddrs) == 0 {
		return out
	}

	// The dial and the CORS questions are independent, and the CORS answer is
	// reported on its own even when a libp2p address already works, so both
	// run and neither waits for the other.
	var wg sync.WaitGroup
	var dialAddr, corsAddr multiaddr.Multiaddr
	var dialErr, corsErr error

	if len(dialable) > 0 {
		wg.Go(func() {
			dialAddr, dialErr = dialBrowserAddrs(ctx, newHost, p, dialable)
		})
	}
	if len(httpEndpoints) > 0 {
		out.CORS.Enabled = true
		wg.Go(func() {
			for _, endpoint := range httpEndpoints {
				if err := probeCORS(ctx, endpoint, c); err != nil {
					corsErr = fmt.Errorf("%s: %w", endpoint, err)
					continue
				}
				corsAddr, corsErr = endpoint, nil
				break
			}
		})
	}
	wg.Wait()

	if out.CORS.Enabled {
		out.CORS.Allowed = corsAddr != nil
		if corsErr != nil {
			out.CORS.Error = corsErr.Error()
		}
	}

	switch {
	case dialAddr != nil:
		out.WebBrowserCompatible = true
		// WebRTC is the one browser transport a Service Worker does not have,
		// so it is the only thing that separates the two answers.
		out.ServiceWorkerCompatible = !isWebRTC(dialAddr)
		out.VerifiedAddr = dialAddr.String()
	case corsAddr != nil:
		// fetch() exists in a Service Worker just as it does in a tab.
		out.WebBrowserCompatible = true
		out.ServiceWorkerCompatible = true
		out.VerifiedAddr = corsAddr.String()
	default:
		var failures []string
		for _, err := range []error{dialErr, corsErr} {
			if err != nil {
				failures = append(failures, err.Error())
			}
		}
		out.Error = strings.Join(failures, "\n")
	}
	return out
}

// isWebRTC reports whether a connection was made over WebRTC, which a Service
// Worker has no API for.
func isWebRTC(addr multiaddr.Multiaddr) bool {
	for _, proto := range addr.Protocols() {
		if proto.Code == multiaddr.P_WEBRTC_DIRECT || proto.Code == multiaddr.P_WEBRTC {
			return true
		}
	}
	return false
}

// dialBrowserAddrs connects to p over the given addresses and nothing else,
// and returns the address the connection was established on.
//
// It runs on its own host so the restricted address set cannot interfere with
// the Bitswap probe, which dials the same peer over whatever works.
func dialBrowserAddrs(ctx context.Context, newHost func() (host.Host, error), p peer.ID, addrs []multiaddr.Multiaddr) (multiaddr.Multiaddr, error) {
	h, err := newHost()
	if err != nil {
		return nil, fmt.Errorf("could not create host for browser dial: %w", err)
	}
	defer h.Close()

	dialCtx, cancel := context.WithTimeout(ctx, browserDialTimeout)
	defer cancel()

	if err := h.Connect(dialCtx, peer.AddrInfo{ID: p, Addrs: addrs}); err != nil {
		return nil, err
	}

	conns := h.Network().ConnsToPeer(p)
	if len(conns) == 0 {
		return nil, fmt.Errorf("connection to %s closed before it could be inspected", p)
	}
	return conns[0].RemoteMultiaddr(), nil
}

// probeCORS asks an HTTPS endpoint the question a browser asks on a page's
// behalf: may a page from another origin read this response?
//
// It sends the preflight first, the OPTIONS request described at
// https://developer.mozilla.org/en-US/docs/Glossary/Preflight_request, and then
// the real block request. Both are checked because a trustless gateway fetch
// is a simple request that browsers do not preflight at all, so a server can
// refuse OPTIONS and still be perfectly usable from a page.
//
// A nil error means a browser would be allowed to read the block.
func probeCORS(ctx context.Context, endpoint multiaddr.Multiaddr, c cid.Cid) error {
	parsed, err := network.ExtractHTTPAddress(endpoint)
	if err != nil {
		return fmt.Errorf("not a usable HTTP address: %w", err)
	}

	ctx, cancel := context.WithTimeout(ctx, corsProbeTimeout)
	defer cancel()

	blockURL := parsed.URL.JoinPath("ipfs", c.String())
	query := blockURL.Query()
	query.Set("format", "raw")
	blockURL.RawQuery = query.Encode()

	client := corsProbeClient

	preflight, err := http.NewRequestWithContext(ctx, http.MethodOptions, blockURL.String(), nil)
	if err != nil {
		return err
	}
	preflight.Header.Set("Origin", corsProbeOrigin)
	preflight.Header.Set("Access-Control-Request-Method", http.MethodGet)
	preflight.Header.Set("Access-Control-Request-Headers", "accept")
	preflight.Header.Set("User-Agent", userAgent)

	if resp, err := client.Do(preflight); err == nil {
		allowed := allowsOrigin(resp)
		resp.Body.Close()
		if allowed {
			return nil
		}
	}

	// No preflight approval, so fall back to the request a gateway fetch
	// actually makes. Only the headers matter, so the body is dropped.
	get, err := http.NewRequestWithContext(ctx, http.MethodGet, blockURL.String(), nil)
	if err != nil {
		return err
	}
	get.Header.Set("Origin", corsProbeOrigin)
	get.Header.Set("Accept", "application/vnd.ipld.raw")
	get.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(get)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if !allowsOrigin(resp) {
		return errors.New("no Access-Control-Allow-Origin header, so a browser may not read the response")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("cross-origin requests are allowed, but the block request returned HTTP %d", resp.StatusCode)
	}
	return nil
}

// allowsOrigin reports whether the response lets the probe's origin read it.
func allowsOrigin(resp *http.Response) bool {
	allowed := resp.Header.Get("Access-Control-Allow-Origin")
	return allowed == "*" || allowed == corsProbeOrigin
}
