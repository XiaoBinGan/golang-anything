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

# The tool without cgo: starts in 2.4 ms and answers everything that does not parse.
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$out/go-anything" .
# The same program with cgo, which answers `__parse`: the fast build starts it for the commands that
# read TypeScript. One source, two builds, no separate helper to keep in step.
CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o "$out/go-anything-cgo" .

# The fast binary has to actually be the fast one. Copying a cgo build over it adds ~0.15 ms of startup
# and looks like a regression in the tool rather than a mistake in the shell — it happened twice.
if ! go version -m "$out/go-anything" 2>/dev/null | grep -q 'CGO_ENABLED=0'; then
  echo "build.sh: $out/go-anything was not built without cgo; startup measurements will lie" >&2
  exit 1
fi

echo "built $out/go-anything (no cgo, 2.4 ms startup) and $out/go-anything-cgo (parser)"
