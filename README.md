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

The web assets in `./web` directory are embedded directly into the Go binary and served as-is. No build step is required for deployment - the pre-built CSS and static files are already committed to the repository.

The latest version from the `main` branch is automatically deployed to https://check.ipfs.network for convenience.

If you need to modify the web interface styles:
1. Make changes to `web/input.css`
2. Run `npm ci` and `npm run build` in the `web` directory (see `web/README.md` for details)
3. Commit the updated `web/output.css` file

> [!IMPORTANT]
> Breaking changes to the HTTP API or frontend MUST be avoided. The new `./web` should always work with old backend versions to ensure compatibility.

For production deployments, you'll want a proxy for HTTPS support on the Go server.


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
	DataAvailableOverHTTP    HTTPCheckOutput
	Source                   string
}
```

The `providerOutput` type contains the following fields:

- `ID`: The peer ID of the provider.
- `ConnectionError`: An error message if the connection to the provider failed.
- `Addrs`: The multiaddrs of the provider from the DHT.
- `ConnectionMaddrs`: The multiaddrs that were used to connect to the provider.
- `DataAvailableOverBitswap`: The result of the Bitswap check.
- `DataAvailableOverHTTP`: The result of the HTTP check.
- `Source`: The source of the provider (either "IPNI" or "Amino DHT").

#### Results when a `multiaddr` and a `cid` are passed

The results of the check are expressed by the `peerCheckOutput` type:

```go
type peerCheckOutput struct {
	ConnectionError              string
	PeerFoundInDHT              map[string]int
	ProviderRecordFromPeerInDHT  bool
	ProviderRecordFromPeerInIPNI bool
	ConnectionMaddrs             []string
	DataAvailableOverBitswap     BitswapCheckOutput
	DataAvailableOverHTTP        HTTPCheckOutput
}

type BitswapCheckOutput struct {
	Enabled   bool
	Duration  time.Duration
	Found     bool
	Responded bool
	Error     string
}

type HTTPCheckOutput struct {
	Enabled   bool
	Duration  time.Duration
	Endpoints []multiaddr.Multiaddr
	Connected bool
	Requested bool
	Found     bool
	Error     string
}
```

The check performs several validations:

1. Is the CID advertised in the DHT by the Passed PeerID (or later IPNI)?
   - `ProviderRecordFromPeerInDHT`: Whether the peer has a provider record in the DHT
   - `ProviderRecordFromPeerInIPNI`: Whether the peer has a provider record in IPNI

2. Are the peer's addresses discoverable?
   - `PeerFoundInDHT`: Map of discovered addresses and their frequency in the DHT

3. Is the peer contactable with the address the user gave us?
   - `ConnectionError`: Empty if connection successful, otherwise contains the error
   - `ConnectionMaddrs`: The multiaddrs used to connect (includes both relay and direct addresses if NAT traversal occurred)

4. Is the data available over Bitswap?
   - `DataAvailableOverBitswap`: Contains:
     - `Enabled`: Whether Bitswap check was performed
     - `Duration`: How long the check took
     - `Found`: Whether the block was found
     - `Responded`: Whether the peer responded
     - `Error`: Any error that occurred

5. Is the data available over HTTP?
   - `DataAvailableOverHTTP`: Contains:
     - `Enabled`: Whether HTTP check was performed
     - `Duration`: How long the check took
     - `Endpoints`: List of HTTP multiaddrs
     - `Connected`: Whether connection was successful
     - `Requested`: Whether the request was sent
     - `Found`: Whether the block was found
     - `Error`: Any error that occurred

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
