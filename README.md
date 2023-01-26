ipfs-check
=======================

> Check if you can find your content on IPFS

A tool for checking the accessibility of your data by IPFS peers

## Documentation


### Build

`go build` will build the server binary in your local directory

### Install
`go install` will build and install the server binary in your global Go binary directory (e.g. `~/go/bin`)

### Deploy

There are web assets in `web` that interacts with the Go HTTP server that can be deployed however you deploy web assets.
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
# Then open http://localhost:3000?backendUrl=http://localhost:3333
```

## License

[SPDX-License-Identifier: Apache-2.0 OR MIT](LICENSE.md)
