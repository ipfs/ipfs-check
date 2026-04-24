# ipfs-check

> Check if you can find your content on IPFS

A debugging tool that verifies whether IPFS peers can retrieve your data.

## Install

`go install github.com/ipfs/ipfs-check@latest` builds and installs the server binary into your Go binary directory (typically `~/go/bin`).

### Docker

The [GitHub container registry](https://github.com/ipfs/ipfs-check/pkgs/container/ipfs-check) publishes Docker images automatically:

- Releases
  - `latest` tracks the most recent stable release
  - `vN.N.N` pins a specific [release tag](https://github.com/ipfs/ipfs-check/releases)
- Unreleased developer builds
  - `main-latest` tracks the `HEAD` of the `main` branch
  - `main-YYYY-DD-MM-GITSHA` pins a specific commit on `main`
- ⚠️ Experimental, unstable builds
  - `staging-latest` tracks the `HEAD` of the `staging` branch
  - `staging-YYYY-DD-MM-GITSHA` pins a specific commit on `staging`
  - Developers use these for internal testing; end users should not rely on them

Pass configuration via `-e`:
```console
$ docker pull ghcr.io/ipfs/ipfs-check:main-latest
$ docker run --rm -it --net=host -e IPFS_CHECK_ACCELERATED_DHT=true ghcr.io/ipfs/ipfs-check:main-latest
```

Run `./ipfs-check --help` to list the available variables.

## Build

### Backend

`go build` produces the `./ipfs-check` binary in the current directory.

### Frontend

The Go binary embeds everything under `./web` and serves it directly. Deployment needs no build step: the pre-built CSS and static files live in the repository.

The `main` branch deploys automatically to <https://check.ipfs.network>.

To modify the web interface styles:
1. Edit `web/input.css`.
2. Run `npm ci` and `npm run build` inside the `web` directory (see `web/README.md`).
3. Commit the updated `web/output.css`.

> [!IMPORTANT]
> Preserve backward compatibility of the HTTP API and frontend. A new `./web` must keep working against older backend versions.

Production deployments should terminate HTTPS at a reverse proxy in front of the Go server.


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

A test frontend runs at <http://localhost:3333/web/?backendURL=http://localhost:3333>.

### Terminal 2

To serve `/web` from a separate tool instead of the built-in HTTP server, any static file server works (or open the HTML file directly in a browser):

```
npx -y serve -l 3000 web
# Then open http://localhost:3000?backendURL=http://localhost:3333
```

## Logging

Control log verbosity via the [go-log environment variables](https://github.com/ipfs/go-log/?tab=readme-ov-file#environment-variables).

For example, enable debug logs for specific subsystems:

```console
$ GOLOG_LOG_LEVEL=info,dht=debug,net/identify=debug ./ipfs-check
```

## Running a check

Make an HTTP call with the `cid` and `multiaddr` query parameters:

```bash
$ curl "localhost:3333/check?cid=bafybeicklkqcnlvtiscr2hzkubjwnwjinvskffn4xorqeduft3wq7vm5u4&multiaddr=/p2p/12D3KooWRBy97UB99e3J6hiPesre1MZeuNQvfan4gBziswrRJsNK"
```

The `multiaddr` takes two forms:

- Peer ID only (`/p2p/PeerID`): the server resolves the Peer ID through the DHT and dials any returned address.
- Full multiaddr with transport and Peer ID, e.g. `/ip4/140.238.164.150/udp/4001/quic-v1/p2p/12D3KooWRTUNZVyVf7KBBNZ6MRR5SYGGjKzS6xyiU5zBeY9wxomo/p2p-circuit/p2p/12D3KooWRBy97UB99e3J6hiPesre1MZeuNQvfan4gBziswrRJsNK`: the Bitswap check uses the supplied multiaddr directly.

### Check results

The checks performed depend on whether the request includes a **multiaddr** or only a **cid**.

#### Results when only a `cid` is passed

The `cidCheckOutput` type describes the results:

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

Fields of `providerOutput`:

- `ID`: peer ID of the provider.
- `ConnectionError`: error message when the connection to the provider failed.
- `Addrs`: multiaddrs of the provider from the DHT.
- `ConnectionMaddrs`: multiaddrs used to reach the provider.
- `DataAvailableOverBitswap`: result of the Bitswap check.
- `DataAvailableOverHTTP`: result of the HTTP check.
- `Source`: origin of the provider record (`IPNI` or `Amino DHT`).

#### Results when a `multiaddr` and a `cid` are passed

The `peerCheckOutput` type describes the results:

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

The check answers five questions:

1. Does the given peer advertise the CID in the DHT or in IPNI?
   - `ProviderRecordFromPeerInDHT`: the peer has a provider record in the DHT.
   - `ProviderRecordFromPeerInIPNI`: the peer has a provider record in IPNI.

2. Are the peer's addresses discoverable?
   - `PeerFoundInDHT`: map of discovered addresses to how often each appeared in the DHT.

3. Does the peer respond at the supplied address?
   - `ConnectionError`: empty on success; otherwise the error.
   - `ConnectionMaddrs`: multiaddrs used for the dial (includes both relay and direct addresses when NAT traversal occurred).

4. Is the data available over Bitswap? `DataAvailableOverBitswap` contains:
   - `Enabled`: the Bitswap check ran.
   - `Duration`: how long the check took.
   - `Found`: the block was returned.
   - `Responded`: the peer replied.
   - `Error`: the error, if any.

5. Is the data available over HTTP? `DataAvailableOverHTTP` contains:
   - `Enabled`: the HTTP check ran.
   - `Duration`: how long the check took.
   - `Endpoints`: the HTTP multiaddrs attempted.
   - `Connected`: the connection succeeded.
   - `Requested`: the request was sent.
   - `Found`: the block was returned.
   - `Error`: the error, if any.

## Metrics

The server exposes Prometheus metrics at `/metrics`, covering [go-libp2p metrics](https://blog.libp2p.io/2023-08-15-metrics-in-go-libp2p/) and HTTP metrics for the check endpoint.

### Securing the metrics endpoint

Protect `/metrics` with HTTP basic auth via the `--metrics-auth-username` and `--metrics-auth-password` flags:

```
./ipfs-check --metrics-auth-username=user --metrics-auth-password=pass
```

The `IPFS_CHECK_METRICS_AUTH_USER` and `IPFS_CHECK_METRICS_AUTH_PASS` environment variables work as well.

## License

[SPDX-License-Identifier: Apache-2.0 OR MIT](LICENSE.md)
