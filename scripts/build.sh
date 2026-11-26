#!/usr/bin/env bash
# Builds both binaries: the tool without cgo (fast to start) and the parse helper with cgo.
#
# The split exists because cgo costs a fixed 1.2 ms before any of this tool's own code runs. A build
# without cgo starts in 2.40 ms on this machine against 3.24 ms with it; the parser lives in a helper
# that is started only by the commands that parse something.
set -euo pipefail

# The toolchain this machine actually has; building needs no node.
REQUIRE_NODE=0 . "$(dirname "$0")/env.sh"

out=${1:-bin}
mkdir -p "$out"

# Two binaries: the front answers --help and --version and execs the engine for everything else, so the
# help path does not pay for the twenty-seven commands it is describing. The engine is the whole tool.
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$out/golang-anything-engine" .
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$out/golang-anything" ./cmd/front
# The same program with cgo, which answers `__parse`: the fast build starts it for the commands that
# read TypeScript. One source, two builds, no separate helper to keep in step.
CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o "$out/golang-anything-cgo" .

# The front binary must not have grown the engine's dependencies: that is its whole reason to exist.
if go version -m "$out/golang-anything" 2>/dev/null | grep -q 'CGO_ENABLED=1'; then
  echo "build.sh: $out/golang-anything was built with cgo; the help path will be slow" >&2
  exit 1
fi

# The fast binary has to actually be the fast one. Copying a cgo build over it adds ~0.15 ms of startup
# and looks like a regression in the tool rather than a mistake in the shell — it happened twice.
if ! go version -m "$out/golang-anything-engine" 2>/dev/null | grep -q 'CGO_ENABLED=0'; then
  echo "build.sh: $out/golang-anything-engine was not built without cgo; startup measurements will lie" >&2
  exit 1
fi

echo "built $out/golang-anything (no cgo, 2.4 ms startup) and $out/golang-anything-cgo (parser)"
