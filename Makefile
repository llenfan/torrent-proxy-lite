BINARY  := torrent-proxy
MODULE  := github.com/llenfan/torrent-proxy-lite
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -X $(MODULE)/internal/version.Version=$(VERSION)

.PHONY: build test run lint clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BINARY) ./cmd/torrent-proxy

test:
	go test ./...

run: build
	./bin/$(BINARY)

lint:
	go vet ./...
	@unformatted=$$(gofmt -l .); if [ -n "$$unformatted" ]; then echo "gofmt needed:"; echo "$$unformatted"; exit 1; fi

clean:
	rm -rf bin
