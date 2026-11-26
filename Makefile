# The two commands a person needs: one to build, one to check everything the project claims.
#
# Everything runs from this directory with the toolchain in PATH. On a machine where brew's go and a
# goenv GOROOT disagree, `make GOROOT=... check` is the way to say which one to use.
GO ?= go
BIN ?= bin
SHELL := /bin/bash

.PHONY: all build test check contract evidence matrix smoke perf negative clean

all: build

build:
	./scripts/build.sh $(BIN)

# The Go half: unit tests for every package, with cgo on so the parser is the real one.
test:
	. ./scripts/env.sh && CGO_ENABLED=1 $(GO) test ./...

# Everything the project claims, in the order a person would check it: the shared rules, then the
# crates, then the claims about the tool as a whole.
check: build test contract matrix evidence smoke
	@echo
	@echo "all checks passed"

contract:
	GO=$(BIN)/golang-anything ./scripts/contract.sh

matrix:
	GO=$(BIN)/golang-anything ./scripts/matrix.sh

evidence:
	GO=$(BIN)/golang-anything ./scripts/evidence.sh

# Every command and every flag that changes behaviour, once, on a throwaway copy.
smoke:
	GO=$(BIN)/golang-anything ./scripts/smoke.sh

perf:
	GO=$(BIN)/golang-anything GOLANG_ANYTHING_PARSE=$(BIN)/golang-anything-cgo ./scripts/perf.sh

# Slow and destructive on purpose: every check is run against a known break and must fail. This is the
# reason to believe `make check`, so it is its own target rather than part of it.
negative:
	./scripts/negative.sh

clean:
	rm -rf $(BIN)
