# ipfs-check

> Check if you can find your content on IPFS

A debugging tool for checking the retrievability of data by IPFS peers

## Install

`go install github.com/ipfs/ipfs-check@latest` will build and install the server binary in your global Go binary directory (e.g. `~/go/bin`)

### Docker

Automated Docker container releases are available from the [Github container registry](https://github.com/ipfs/ipfs-check/pkgs/container/ipfs-check):

- Releases
  - `latest` always points at the latest stable release
  - `vN.N.N` point at a specific [release tag](https://github.com/ipfs/ipfs-check/releases)
- Unreleased developer builds
  - `main-latest` always points at the `HEAD` of the `main` branch
  - `main-YYYY-DD-MM-GITSHA` points at a specific commit from the `main` branch
- ⚠️ Experimental, unstable builds
  - `staging-latest` always points at the `HEAD` of the `staging` branch
  - `staging-YYYY-DD-MM-GITSHA` points at a specific commit from the `staging` branch
  - This tag is used by developers for internal testing, not intended for end users

When using Docker, make sure to pass necessary config via `-e`:
```console
$ docker pull ghcr.io/ipfs/ipfs-check:main-latest
$ docker run --rm -it --net=host -e IPFS_CHECK_ACCELERATED_DHT=true ghcr.io/ipfs/ipfs-check:main-latest
```

Learn available variables via `./ipfs-check --help`

## Build

### Backend

`go build` will build the `./ipfs-check` binary in your local directory

### Frontend

There are web assets in `web` that interact with the Go HTTP server that can be deployed however you deploy web assets.
Maybe just deploy it on IPFS and reference it with DNSLink.

For anything other than local testing you're going to want to have a proxy to give you HTTPS support on the Go server.

At a minimum, the following files should be available from your web-server on prod: `web/index.html`, `web/tachyons.min.css`.


## Running locally

### Terminal 1

```console
$ go build
$ ./ipfs-check
Starting ipfs-check
...
2024/08/29 20:42:34 Please wait, initializing accelerated-dht client.. (mapping Amino DHT may takes 5 or more minutes)
2024/08/29 20:42:34 Accelerated DHT client ready
2024/08/29 20:46:59 Backend ready and listening on [::]:3333
2024/08/29 20:46:59 Test fronted at http://localhost:3333/web/?backendURL=http://localhost:3333
2024/08/29 20:46:59 Ready to start serving.
```

As a convenience, a test frontend is provided at <http://localhost:3333/web/?backendURL=http://localhost:3333>.

### Terminal 2

If you don't want to use test HTTP server from ipfs-check itself, feel free to
use any other tool to serve the contents of the /web folder (you can open the
html file directly in your browser).

```
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

The server performs several checks depending on whether you also pass a **multiaddr** or just a **cid**.

#### Results when only a `cid` is passed

The results of the check are expressed by the `cidCheckOutput` type:

```go
type cidCheckOutput *[]providerOutput

type providerOutput struct {
	ID                       string
	ConnectionError          string
	Addrs                    []string
	ConnectionMaddrs         []string
	DataAvailableOverBitswap BitswapCheckOutput
}
```

The `providerOutput` type contains the following fields:

- `ID`: The peer ID of the provider.
- `ConnectionError`: An error message if the connection to the provider failed.
- `Addrs`: The multiaddrs of the provider from the DHT.
- `ConnectionMaddrs`: The multiaddrs that were used to connect to the provider.
- `DataAvailableOverBitswap`: The result of the Bitswap check.

#### Results when a `multiaddr` and a `cid` are passed

The results of the check are expressed by the `peerCheckOutput` type:

```go
type peerCheckOutput struct {
	ConnectionError             string
	PeerFoundInDHT              map[string]int
	ProviderRecordFromPeerInDHT bool
	ConnectionMaddrs            []string
	DataAvailableOverBitswap    BitswapCheckOutput
}

type BitswapCheckOutput struct {
	Duration  time.Duration
	Found     bool
	Responded bool
	Error     string
}
```

1. Is the CID (really multihash) advertised in the DHT by the Passed PeerID (or later IPNI)?

- `ProviderRecordFromPeerInDHT`

2. Are the peer's addresses discoverable (particularly useful if the announcements are DHT based, but also independently useful)

- `PeerFoundInDHT`

3. Is the peer contactable with the address the user gave us?

- If `ConnectionError` is any empty string, a connection to the peer was successful. Otherwise, it contains the error.
- If a connection is successful, `ConnectionMaddrs` contains the multiaddrs that were used to connect. If the peer is behind NAT, it will contain both the circuit relay multiaddr and the direct maddr.

4. Is the address the user gave us present in the DHT?

- If `PeerFoundInDHT` contains the address the user passed in

1. Does the peer say they have at least the block for the CID (doesn't say anything about the rest of any associated DAG) over Bitswap?

- `DataAvailableOverBitswap` contains the duration of the check and whether the peer responded and has the block. If there was an error, `DataAvailableOverBitswap.Error` will contain the error. 

## Metrics

The ipfs-check server is instrumented and exposes two Prometheus metrics endpoints:

- `/metrics` exposes [go-libp2p metrics](https://blog.libp2p.io/2023-08-15-metrics-in-go-libp2p/) and http metrics for the check endpoint.

### Securing the metrics endpoints

To add HTTP basic auth to the two metrics endpoints, you can use the `--metrics-auth-username` and `--metrics-auth-password` flags:

```
./ipfs-check --metrics-auth-username=user --metrics-auth-password=pass
```

Alternatively, you can use the `IPFS_CHECK_METRICS_AUTH_USER` and `IPFS_CHECK_METRICS_AUTH_PASS` env vars.

## License

[SPDX-License-Identifier: Apache-2.0 OR MIT](LICENSE.md)
