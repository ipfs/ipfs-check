package main

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/ipfs/boxo/namesys"
	"github.com/ipfs/boxo/path"
	"github.com/ipfs/go-cid"
	golog "github.com/ipfs/go-log/v2"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/gologshim"
	"github.com/multiformats/go-multiaddr"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/urfave/cli/v2"
)

//go:embed web
var webFS embed.FS

func init() {
	// Bridge slog-based libraries (like go-libp2p >= 0.45) back into go-log.
	// This ensures go-libp2p logs are visible and can be adjusted at runtime.
	// See: https://github.com/ipfs/go-log/releases/tag/v2.9.0
	slog.SetDefault(slog.New(golog.SlogHandler()))
	gologshim.SetDefaultHandler(golog.SlogHandler())
}

func main() {
	app := cli.NewApp()
	app.Name = name
	app.Usage = "Server tool for checking the accessibility of your data by IPFS peers"
	app.Flags = []cli.Flag{
		&cli.StringFlag{
			Name:    "address",
			Value:   ":3333",
			Usage:   "address to run on",
			EnvVars: []string{"IPFS_CHECK_ADDRESS"},
		},
		&cli.BoolFlag{
			Name:    "accelerated-dht",
			Value:   true,
			EnvVars: []string{"IPFS_CHECK_ACCELERATED_DHT"},
			Usage:   "run the accelerated DHT client",
		},
		&cli.StringFlag{
			Name:    "metrics-auth-username",
			Value:   "",
			EnvVars: []string{"IPFS_CHECK_METRICS_AUTH_USER"},
			Usage:   "http basic auth user for the metrics endpoints",
		},
		&cli.StringFlag{
			Name:    "metrics-auth-password",
			Value:   "",
			EnvVars: []string{"IPFS_CHECK_METRICS_AUTH_PASS"},
			Usage:   "http basic auth password for the metrics endpoints",
		},
	}
	app.Action = func(cctx *cli.Context) error {
		ctx := cctx.Context

		d, err := newDaemon(ctx, cctx.Bool("accelerated-dht"))
		if err != nil {
			return err
		}

		return startServer(ctx, d, cctx.String("address"), cctx.String("metrics-auth-username"), cctx.String("metrics-auth-password"))
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

const (
	defaultCheckTimeout = 60 * time.Second
	defaultIndexerURL   = "https://cid.contact"
	libp2pKeyCodec      = 0x72 // multicodec for libp2p-key (PeerID in CIDv1 format)
)

func startServer(ctx context.Context, d *daemon, tcpListener, metricsUsername, metricPassword string) error {
	log.Printf("Starting %s %s\n", name, version)
	l, err := net.Listen("tcp", tcpListener)
	if err != nil {
		return err
	}

	log.Printf("Libp2p host peer id %s\n", d.h.ID())
	log.Printf("Libp2p host listening on %v\n", d.h.Addrs())

	log.Printf("Backend ready and listening on %v\n", l.Addr())

	webAddr := getWebAddress(l)
	log.Printf("Test fronted at http://%s/web/?backendURL=http://%s\n", webAddr, webAddr)
	log.Printf("Metrics endpoint at http://%s/metrics\n", webAddr)
	log.Printf("Ready to start serving.")

	checkHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Access-Control-Allow-Origin", "*")

		q := r.URL.Query()
		maStr := q.Get("multiaddr")
		cidStr := q.Get("cid")
		timeoutStr := q.Get("timeoutSeconds")
		ipniURL := q.Get("ipniIndexer")
		httpRetrieval := q.Get("httpRetrieval") == "on"

		if cidStr == "" {
			http.Error(w, "missing 'cid' query parameter", http.StatusBadRequest)
			return
		}

		checkTimeout := defaultCheckTimeout
		if timeoutStr != "" {
			var err error
			checkTimeout, err = time.ParseDuration(timeoutStr + "s")
			if err != nil {
				http.Error(w, "Invalid timeout value (in seconds)", http.StatusBadRequest)
				return
			}
		}

		if ipniURL == "" {
			ipniURL = defaultIndexerURL
		}

		log.Printf("Checking %s with timeout %s seconds", cidStr, checkTimeout.String())
		withTimeout, cancel := context.WithTimeout(r.Context(), checkTimeout)
		defer cancel()

		// Resolve input (CID, IPNS name, or DNSLink)
		cidKey, mutableRes, err := resolveInput(withTimeout, d.ns, cidStr)
		if err != nil {
			if mutableRes != nil && mutableRes.Error != "" {
				// Resolution attempted but failed, return resolution info with error
				w.Header().Add("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]interface{}{
					"MutableResolution": mutableRes,
				})
				return
			}
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		var data interface{}
		if maStr == "" {
			cidOutput, err := d.runCidCheck(withTimeout, cidKey, ipniURL, httpRetrieval)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			// Wrap response with resolution info at top level
			if mutableRes != nil {
				data = map[string]interface{}{
					"MutableResolution": mutableRes,
					"Providers":         cidOutput,
				}
			} else {
				data = cidOutput
			}
		} else {
			ma, ai, err400 := parseMultiaddr(maStr)
			if err400 != nil {
				http.Error(w, err400.Error(), http.StatusBadRequest)
				return
			}
			peerOutput, err := d.runPeerCheck(withTimeout, ma, ai, cidKey, ipniURL, httpRetrieval)
			if err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			// Wrap response with resolution info at top level
			if mutableRes != nil {
				data = map[string]interface{}{
					"MutableResolution": mutableRes,
					"Result":            peerOutput,
				}
			} else {
				data = peerOutput
			}
		}
		w.Header().Add("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(data)
	}

	// Register the default Go collector
	d.promRegistry.MustRegister(collectors.NewGoCollector())

	// Register the process collector
	d.promRegistry.MustRegister(collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}))

	requestsTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of HTTP requests",
		},
		[]string{"code"},
	)

	requestDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "Duration of HTTP requests",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"code"},
	)

	requestsInFlight := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "http_requests_in_flight",
		Help: "Number of HTTP requests currently being served",
	})

	// Register metrics with our custom registry
	d.promRegistry.MustRegister(requestsTotal)
	d.promRegistry.MustRegister(requestDuration)
	d.promRegistry.MustRegister(requestsInFlight)

	// Instrument the checkHandler
	instrumentedHandler := promhttp.InstrumentHandlerCounter(
		requestsTotal,
		promhttp.InstrumentHandlerDuration(
			requestDuration,
			promhttp.InstrumentHandlerInFlight(
				requestsInFlight,
				http.HandlerFunc(checkHandler),
			),
		),
	)

	http.Handle("/check", instrumentedHandler)

	// Use a single metrics endpoint for all Prometheus metrics
	http.Handle("/metrics", BasicAuth(promhttp.HandlerFor(d.promRegistry, promhttp.HandlerOpts{}), metricsUsername, metricPassword))

	// Serve frontend on /web
	fileServer := http.FileServer(http.FS(webFS))
	http.Handle("/web/", fileServer)
	// Set up the root route to redirect to /web/ with preserved query parameters
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		target := "/web/"
		if r.URL.RawQuery != "" {
			target = target + "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusFound)
	})

	done := make(chan error, 1)
	go func() {
		defer close(done)
		done <- http.Serve(l, nil)
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = l.Close()
		return <-done
	}
}

func BasicAuth(handler http.Handler, username, password string) http.Handler {
	if username == "" || password == "" {
		log.Println("Warning: no http basic auth for the metrics endpoint.")
		return handler
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()

		if !ok || subtle.ConstantTimeCompare([]byte(user), []byte(username)) != 1 || subtle.ConstantTimeCompare([]byte(pass), []byte(password)) != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="Restricted"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		handler.ServeHTTP(w, r)
	})
}

// getWebAddress returns listener with [::] and 0.0.0.0 replaced by localhost
func getWebAddress(l net.Listener) string {
	addr := l.Addr().String()
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	switch host {
	case "", "0.0.0.0", "::":
		return net.JoinHostPort("localhost", port)
	default:
		return addr
	}
}

// buildDiagnosticURL returns the appropriate diagnostic URL for a given IPNS name or DNSLink.
func buildDiagnosticURL(name string) string {
	if strings.Contains(name, ".") {
		return "https://dnslink.dev/#" + name
	}
	return "https://ipns.ipfs.network/#" + name
}

// resolveInput attempts to parse input as a CID,
// and if that fails, tries to resolve it as an IPNS name or DNSLink.
// Returns the resolved CID and optional resolution info.
func resolveInput(ctx context.Context, ns namesys.NameSystem, input string) (cid.Cid, *MutableResolution, error) {
	// Strip ipfs:// prefix if present
	input = strings.TrimPrefix(input, "ipfs://")

	// Try to decode as CID
	if c, err := cid.Decode(input); err == nil {
		if c.Type() == libp2pKeyCodec {
			// PeerID in CIDv1 format - resolve as IPNS
			return resolveMutablePath(ctx, ns, "/ipns/"+input, true)
		}
		// Regular content CID - return immediately
		return c, nil, nil
	}

	// Try to decode as legacy PeerID (base58btc multihash)
	if _, err := peer.Decode(input); err == nil {
		// Legacy PeerID - resolve as IPNS
		return resolveMutablePath(ctx, ns, "/ipns/"+input, true)
	}

	// Must be DNSLink or IPNS path - resolve with warning
	return resolveMutablePath(ctx, ns, input, true)
}

// resolveMutablePath resolves an IPNS or DNSLink path to a CID and returns resolution metadata.
func resolveMutablePath(ctx context.Context, ns namesys.NameSystem, input string, isMutableInput bool) (cid.Cid, *MutableResolution, error) {
	mutableRes := &MutableResolution{
		IsMutableInput: isMutableInput,
	}

	// Normalize input to an IPNS path
	var p path.Path
	var err error

	if strings.HasPrefix(input, "/ipns/") || strings.HasPrefix(input, "/ipfs/") {
		mutableRes.InputPath = input
		p, err = path.NewPath(input)
	} else {
		// Bare domain or IPNS name - prefix with /ipns/
		mutableRes.InputPath = "/ipns/" + input
		p, err = path.NewPath(mutableRes.InputPath)
	}

	if err != nil {
		return cid.Cid{}, nil, fmt.Errorf("not a valid CID, IPNS name, or DNSLink: %w", err)
	}

	// If it's an /ipfs/ path, extract the CID directly
	if !p.Mutable() {
		segments := p.Segments()
		c, err := cid.Decode(segments[1])
		if err != nil {
			return cid.Cid{}, nil, fmt.Errorf("invalid CID in path: %w", err)
		}
		return c, nil, nil
	}

	// Attempt IPNS/DNSLink resolution
	result, err := ns.Resolve(ctx, p)
	name := p.Segments()[1]

	if err != nil {
		mutableRes.Error = err.Error()
		mutableRes.DiagnosticURL = buildDiagnosticURL(name)
		return cid.Cid{}, mutableRes, fmt.Errorf("resolution failed: %w", err)
	}

	mutableRes.ResolvedPath = result.Path.String()
	mutableRes.DiagnosticURL = buildDiagnosticURL(name)

	// Extract CID from resolved path
	segments := result.Path.Segments()
	if len(segments) < 2 {
		return cid.Cid{}, mutableRes, fmt.Errorf("resolved path has insufficient components: %s", result.Path.String())
	}

	c, err := cid.Decode(segments[1])
	if err != nil {
		return cid.Cid{}, mutableRes, fmt.Errorf("invalid CID in resolved path: %w", err)
	}

	return c, mutableRes, nil
}

func parseMultiaddr(maStr string) (multiaddr.Multiaddr, peer.AddrInfo, error) {
	ma, err := multiaddr.NewMultiaddr(maStr)
	if err != nil {
		return nil, peer.AddrInfo{}, err
	}
	ai, err := peer.AddrInfoFromP2pAddr(ma)
	if err != nil {
		return ma, peer.AddrInfo{}, err
	}
	return ma, *ai, nil
}
