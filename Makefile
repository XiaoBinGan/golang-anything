# The two commands a person needs: one to build, one to check everything the project claims.
#
# Everything runs from this directory with the toolchain in PATH. On a machine where brew's go and a
# goenv GOROOT disagree, `make GOROOT=... check` is the way to say which one to use.
GO ?= go
BIN ?= bin
SHELL := /bin/bash

.PHONY: all build test check contract evidence matrix perf clean

all: build

build:
	./scripts/build.sh $(BIN)

# The Go half: unit tests for every package, with cgo on so the parser is the real one.
test:
	CGO_ENABLED=1 $(GO) test ./...

# Everything the project claims, in the order a person would check it: the shared rules, then the
# crates, then the claims about the tool as a whole.
check: build test contract matrix evidence
	@echo
	@echo "all checks passed"

contract:
	GO=$(BIN)/go-anything ./scripts/contract.sh

matrix:
	GO=$(BIN)/go-anything ./scripts/matrix.sh

evidence:
	GO=$(BIN)/go-anything ./scripts/evidence.sh

perf:
	GO=$(BIN)/go-anything GO_ANYTHING_PARSE=$(BIN)/go-anything-cgo ./scripts/perf.sh

clean:
	rm -rf $(BIN)
