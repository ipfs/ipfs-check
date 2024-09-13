package main

import (
	"context"
	"crypto/subtle"
	"embed"
	"encoding/json"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/urfave/cli/v2"
)

//go:embed web
var webFS embed.FS

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

const DEFAULT_CHECK_TIMEOUT = 60
const DEFAULT_IPNI_INDEXER = "https://cid.contact"

func startServer(ctx context.Context, d *daemon, tcpListener, metricsUsername, metricPassword string) error {
	log.Printf("Starting %s %s\n", name, version)
	l, err := net.Listen("tcp", tcpListener)
	if err != nil {
		return err
	}

	log.Printf("Libp2p host peer id %s\n", d.h.ID())
	log.Printf("Libp2p host listening on %v\n", d.h.Addrs())

	d.mustStart()

	log.Printf("Backend ready and listening on %v\n", l.Addr())

	webAddr := getWebAddress(l)
	log.Printf("Test fronted at http://%s/web/?backendURL=http://%s\n", webAddr, webAddr)
	log.Printf("Metrics endpoint at http://%s/metrics\n", webAddr)
	log.Printf("Ready to start serving.")

	checkHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Access-Control-Allow-Origin", "*")

		maStr := r.URL.Query().Get("multiaddr")
		cidStr := r.URL.Query().Get("cid")
		timeoutStr := r.URL.Query().Get("timeoutSeconds")
		ipniIndexer := r.URL.Query().Get("ipniIndexer")

		if cidStr == "" {
			err = errors.New("missing 'cid' query parameter")
		}

		timeout := DEFAULT_CHECK_TIMEOUT
		if timeoutStr != "" {
			timeout, err = strconv.Atoi(timeoutStr)
			if err != nil {
				http.Error(w, "Invalid timeout value (in seconds)", http.StatusBadRequest)
				return
			}
		}

		if ipniIndexer == "" {
			ipniIndexer = DEFAULT_IPNI_INDEXER
		}

		FindIPNIProviders(ctx, cidStr, ipniIndexer)

		log.Printf("Checking %s with timeout %d seconds", cidStr, timeout)
		withTimeout, cancel := context.WithTimeout(r.Context(), time.Duration(timeout)*time.Second)
		defer cancel()
		var err error
		var data interface{}

		if maStr == "" {
			data, err = d.runCidCheck(withTimeout, cidStr)
		} else {
			data, err = d.runPeerCheck(withTimeout, maStr, cidStr)
		}

		if err == nil {
			w.Header().Add("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(data)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(err.Error()))
		}
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
	// Set up the root route to redirect to /web
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/web", http.StatusFound)
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
	case "":
		fallthrough
	case "0.0.0.0":
		fallthrough
	case "::":
		return net.JoinHostPort("localhost", port)
	default:
		return addr
	}
}
