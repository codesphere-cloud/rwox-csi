.PHONY: all fmt test build verify

all: verify

fmt:
	gofmt -w api cmd internal

test:
	go test ./...

build:
	go build ./cmd/csi-controller ./cmd/csi-node ./cmd/operator

verify:
	test -z "$$(gofmt -l api cmd internal)"
	go vet ./...
	go test ./...
	go build ./cmd/csi-controller ./cmd/csi-node ./cmd/operator
