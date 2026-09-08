GO ?= go
BINARY := bin/janet-lsp

.PHONY: build test race check clean

build:
	$(GO) build -trimpath -o $(BINARY) ./cmd/janet-lsp

test:
	$(GO) test ./...

race:
	$(GO) test -race ./...

check: test race build
	$(BINARY) --version

clean:
	rm -rf bin
