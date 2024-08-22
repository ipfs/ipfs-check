package main

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/urfave/cli/v2"
)

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

func startServer(ctx context.Context, d *daemon, tcpListener, metricsUsername, metricPassword string) error {
	fmt.Printf("Starting %s %s\n", name, version)
	l, err := net.Listen("tcp", tcpListener)
	if err != nil {
		return err
	}

	log.Printf("listening on %v\n", l.Addr())
	log.Printf("Libp2p host peer id %s\n", d.h.ID())
	log.Printf("Libp2p host listening on %v\n", d.h.Addrs())
	log.Printf("listening on %v\n", l.Addr())

	d.mustStart()

	log.Printf("ready to start serving")

	checkHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Access-Control-Allow-Origin", "*")
		data, err := d.runCheck(r.URL.Query())
		if err == nil {
			w.Header().Add("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(data)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(err.Error()))
		}
	}

	// Create a custom registry
	reg := prometheus.NewRegistry()

	requestsTotal := prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total number of slow requests",
		},
		[]string{"code"},
	)

	requestDuration := prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "Duration of slow requests",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"code"},
	)

	requestsInFlight := prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "slow_requests_in_flight",
		Help: "Number of slow requests currently being served",
	})

	// Register metrics with our custom registry
	reg.MustRegister(requestsTotal)
	reg.MustRegister(requestDuration)
	reg.MustRegister(requestsInFlight)
	// Instrument the slowHandler
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

	// 1. Is the peer findable in the DHT?
	// 2. Does the multiaddr work? If not, what's the error?
	// 3. Is the CID in the DHT?
	// 4. Does the peer respond that it has the given data over Bitswap?
	http.Handle("/check", instrumentedHandler)

	http.Handle("/metrics/libp2p", BasicAuth(promhttp.Handler(), metricsUsername, metricPassword))
	http.Handle("/metrics/http", BasicAuth(promhttp.HandlerFor(reg, promhttp.HandlerOpts{}), metricsUsername, metricPassword))

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
