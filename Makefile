GO ?= go
BINARY := bin/janet-lsp
COMPILER_BINARY := bin/janet-lsp-compiler

.PHONY: build compiler install install-compiler test compiler-test race check check-compiler clean

build:
	$(GO) build -trimpath -o $(BINARY) ./cmd/janet-lsp

compiler:
	CGO_ENABLED=1 $(GO) build -tags libjanet -trimpath -o $(COMPILER_BINARY) ./cmd/janet-lsp-compiler

install:
	$(GO) install ./cmd/janet-lsp

install-compiler:
	CGO_ENABLED=1 $(GO) install -tags libjanet ./cmd/janet-lsp-compiler

test:
	$(GO) test ./...

compiler-test:
	CGO_ENABLED=1 $(GO) test -tags libjanet ./cmd/janet-lsp-compiler

race:
	$(GO) test -race ./...

check: test race build
	$(BINARY) --version

check-compiler: compiler-test compiler

clean:
	rm -rf bin
