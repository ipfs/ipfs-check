package main

import (
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
	app.Action = func(ctx *cli.Context) error {
		daemon, err := newDaemon(ctx.Context, ctx.Bool("accelerated-dht"))
		if err != nil {
			return err
		}

		l, err := net.Listen("tcp", ctx.String("address"))
		if err != nil {
			return err
		}

		log.Printf("listening on %v\n", l.Addr())

		daemon.mustStart()

		log.Printf("ready to start serving")

		/*
			1. Is the peer findable in the DHT?
			2. Does the multiaddr work? (what's the error)
			3. Is the CID in the DHT?
			4. Does the peer respond that it has the given data over Bitswap?
		*/
		http.HandleFunc("/", func(writer http.ResponseWriter, request *http.Request) {
			if err := daemon.runCheck(writer, request.RequestURI); err != nil {
				writer.Header().Add("Access-Control-Allow-Origin", "*")
				writer.WriteHeader(http.StatusInternalServerError)
				_, _ = writer.Write([]byte(err.Error()))
				return
			}
		})

		return http.Serve(l, nil)
	}

	err := app.Run(os.Args)
	if err != nil {
		log.Fatal(err)
	}
}
