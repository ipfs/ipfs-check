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

There's an HTML file in `web` that interacts with the Go HTTP server that can be deployed however you deploy HTML files. 
Maybe just deploy it on IPFS and reference it with DNSLink.

For anything other than local testing you're going to want to have a proxy to give you HTTPS support on the Go server.

## Docker

There's a `Dockerfile` that runs the tool in docker.

```sh
docker build -t ipfs-check .
docker run -d ipfs-check
```

## License

[SPDX-License-Identifier: Apache-2.0 OR MIT](LICENSE.md)
