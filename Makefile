GO ?= go
BINARY := bin/janetpls
COMPILER_BINARY := bin/janetpls-compiler

.PHONY: build compiler install install-compiler test compiler-test race check check-compiler clean

build:
	$(GO) build -trimpath -o $(BINARY) ./cmd/janetpls

compiler:
	CGO_ENABLED=1 $(GO) build -tags libjanet -trimpath -o $(COMPILER_BINARY) ./cmd/janetpls-compiler

install:
	$(GO) install ./cmd/janetpls

install-compiler:
	CGO_ENABLED=1 $(GO) install -tags libjanet ./cmd/janetpls-compiler

test:
	$(GO) test ./...

compiler-test:
	CGO_ENABLED=1 $(GO) test -tags libjanet ./cmd/janetpls-compiler

race:
	$(GO) test -race ./...

check: test race build
	$(BINARY) --version

check-compiler: compiler-test compiler

clean:
	rm -rf bin
