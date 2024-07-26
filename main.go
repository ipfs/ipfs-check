package main

import (
	"context"
	"encoding/json"
	"log"
	"net"
	"net/http"
	"os"

	"github.com/urfave/cli/v2"
)

func main() {
	app := cli.NewApp()
	app.Name = "ipfs-check"
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
	}
	app.Action = func(cctx *cli.Context) error {
		ctx := cctx.Context
		d, err := newDaemon(ctx, cctx.Bool("accelerated-dht"))
		if err != nil {
			return err
		}
		return startServer(ctx, d, cctx.String("address"))
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}

func startServer(ctx context.Context, d *daemon, tcpListener string) error {
	l, err := net.Listen("tcp", tcpListener)
	if err != nil {
		return err
	}

	log.Printf("listening on %v\n", l.Addr())

	d.mustStart()

	log.Printf("ready to start serving")

	// 1. Is the peer findable in the DHT?
	// 2. Does the multiaddr work? If not, what's the error?
	// 3. Is the CID in the DHT?
	// 4. Does the peer respond that it has the given data over Bitswap?
	http.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Add("Access-Control-Allow-Origin", "*")
		data, err := d.runCheck(r.URL.Query())
		if err == nil {
			w.Header().Add("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(data)
		} else {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(err.Error()))
		}
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
