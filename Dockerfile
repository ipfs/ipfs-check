# Builder
FROM --platform=${BUILDPLATFORM:-linux/amd64} golang:1.21-bookworm AS builder

LABEL org.opencontainers.image.source=https://github.com/ipfs-shipyard/ipfs-check
LABEL org.opencontainers.image.description="Check if you can find your content on IPFS"
LABEL org.opencontainers.image.licenses=MIT+APACHE_2.0

ARG TARGETPLATFORM TARGETOS TARGETARCH

ENV GOPATH      /go
ENV SRC_PATH    $GOPATH/src/github.com/ipfs-shipyard/ipfs-check
ENV GO111MODULE on
ENV GOPROXY     https://proxy.golang.org

COPY go.* $SRC_PATH/
WORKDIR $SRC_PATH
RUN go mod download

COPY . $SRC_PATH
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -o $GOPATH/bin/ipfs-check

# Runner
FROM debian:bookworm-slim

RUN apt-get update && \
  apt-get install --no-install-recommends -y tini ca-certificates curl && \
  rm -rf /var/lib/apt/lists/*

ENV GOPATH      /go
ENV SRC_PATH    $GOPATH/src/github.com/ipfs-shipyard/ipfs-check
ENV DATA_PATH   /data/ipfs-check

COPY --from=builder $GOPATH/bin/ipfs-check /usr/local/bin/ipfs-check

RUN mkdir -p $DATA_PATH && \
    useradd -d $DATA_PATH -u 1000 -G users ipfs && \
    chown ipfs:users $DATA_PATH
VOLUME $DATA_PATH
WORKDIR $DATA_PATH

USER ipfs
ENTRYPOINT ["tini", "--", "/usr/local/bin/ipfs-check", "start"]