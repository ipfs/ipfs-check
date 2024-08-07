# ipfs-check

> Check if you can find your content on IPFS

A debugging tool for checking the retrievability of data by IPFS peers

## Documentation

### Build

`go build` will build the server binary in your local directory

### Install

`go install` will build and install the server binary in your global Go binary directory (e.g. `~/go/bin`)

### Deploy

There are web assets in `web` that interact with the Go HTTP server that can be deployed however you deploy web assets.
Maybe just deploy it on IPFS and reference it with DNSLink.

For anything other than local testing you're going to want to have a proxy to give you HTTPS support on the Go server.

When deploying to prod, since the addition of telemetry (https://github.com/ipfs-shipyard/ipfs-check/pull/30) you will also need to run the following before serving the web assets:

```
cd web
npm install && npm run build
```

At a minimum, the following files should be available from your web-server on prod: `web/index.html`, `web/tachyons.min.css`, `web/dist/telemetry.js`.

## Docker

There's a `Dockerfile` that runs the tool in docker.

```sh
docker build -t ipfs-check .
docker run -d ipfs-check
```

## Running locally

### Terminal 1

```
go build
./ipfs-check # Note listening port.. output should say something like "listening on [::]:3333"
```

### Terminal 2

```
# feel free to use any other tool to serve the contents of the /web folder (you can open the html file directly in your browser)
npx -y serve -l 3000 web
# Then open http://localhost:3000?backendURL=http://localhost:3333
```

## Running a check

To run a check, make an http call with the `cid` and `multiaddr` query parameters:

```bash
$ curl "localhost:3333/check?cid=bafybeicklkqcnlvtiscr2hzkubjwnwjinvskffn4xorqeduft3wq7vm5u4&multiaddr=/p2p/12D3KooWRBy97UB99e3J6hiPesre1MZeuNQvfan4gBziswrRJsNK"
```

Note that the `multiaddr` can be:

- A `multiaddr` with just a Peer ID, i.e. `/p2p/PeerID`. In this case, the server will attempt to resolve this Peer ID with the DHT and connect to any of resolved addresses.
- A `multiaddr` with an address port and transport, and Peer ID, e.g. `/ip4/140.238.164.150/udp/4001/quic-v1/p2p/12D3KooWRTUNZVyVf7KBBNZ6MRR5SYGGjKzS6xyiU5zBeY9wxomo/p2p-circuit/p2p/12D3KooWRBy97UB99e3J6hiPesre1MZeuNQvfan4gBziswrRJsNK`. In this case, the Bitswap check will only happen using the passed multiaddr.

### Check results

The server performs several checks given a CID. The results of the check are expressed by the `output` type:

```go
type output struct {
 ConnectionError          string
 PeerFoundInDHT           map[string]int
 CidInDHT                 bool
 DataAvailableOverBitswap BitswapCheckOutput
}

type BitswapCheckOutput struct {
	Duration  time.Duration
	Found     bool
	Responded bool
	Error     string
}
```

1. Is the CID (really multihash) advertised in the DHT (or later IPNI)?

- `CidInDHT`

2. Are the peer's addresses discoverable (particularly useful if the announcements are DHT based, but also independently useful)

- `PeerFoundInDHT`

3. Is the peer contactable with the address the user gave us?

- If `ConnectionError` is any empty string, the peer can be reached. Otherwise, it contains the error.

4. Is the address the user gave us present in the DHT?

- If `PeerFoundInDHT` contains the address the user passed in

1. Does the peer say they have at least the block for the CID (doesn't say anything about the rest of any associated DAG) over Bitswap?

- `DataAvailableOverBitswap` contains the duration of the check and whether the peer responded and has the block. If there was an error, `DataAvailableOverBitswap.Error` will contain the error. 

## Metrics

The ipfs-check server is instrumented and exposes two Prometheus metrics endpoints:

- `/metrics/libp2p` exposes [go-libp2p metrics](https://blog.libp2p.io/2023-08-15-metrics-in-go-libp2p/).
- `/metrics/http` exposes http metrics for the check endpoint.

### Securing the metrics endpoints

To add HTTP basic auth to the two metrics endpoints, you can use the `--metrics-auth-username` and `--metrics-auth-password` flags:

```
./ipfs-check --metrics-auth-username=user --metrics-auth-password=pass
```

Alternatively, you can use the `IPFS_CHECK_METRICS_AUTH_USER` and `IPFS_CHECK_METRICS_AUTH_PASS` env vars.

## License

[SPDX-License-Identifier: Apache-2.0 OR MIT](LICENSE.md)
