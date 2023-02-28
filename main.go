package main

import (
	"context"
	"fmt"
	"net"
	"net/http"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/routing"
)

type kademlia interface {
	routing.Routing
	GetClosestPeers(ctx context.Context, key string) ([]peer.ID, error)
}

func main() {
	daemon := NewDaemon()

	l, err := net.Listen("tcp", ":3333")
	if err != nil {
		panic(err)
	}

	fmt.Printf("listening on %v\n", l.Addr())

	daemon.MustStart()

	fmt.Println("Ready to start serving")

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

	err = http.Serve(l, nil)
	if err != nil {
		panic(err)
	}
}
