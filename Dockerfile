# syntax=docker/dockerfile:1
FROM golang:1.24 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
ARG VERSION=dev
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/csi-controller ./cmd/csi-controller && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w -X main.version=${VERSION}" -o /out/csi-node ./cmd/csi-node && \
    CGO_ENABLED=0 go build -trimpath -o /out/operator ./cmd/operator

FROM debian:bookworm-slim
RUN apt-get update && apt-get install -y --no-install-recommends ca-certificates nfs-common && \
    rm -rf /var/lib/apt/lists/*
COPY --from=build /out/ /
USER 0:0
ENTRYPOINT ["/csi-controller"]
