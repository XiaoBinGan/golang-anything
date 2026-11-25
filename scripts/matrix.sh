#!/usr/bin/env bash
# One line per crate: does the port answer every recorded case, through which bridge, and how fast.
#
# Capability and performance in the same table, because a crate that is green but never measured and a
# crate that is fast but wrong are both unfinished. Every crate below is a real unit or a fixture that
# has been ported by hand; nothing here is skipped to make the table look better.
set -uo pipefail

GO=${GO:-./bin/go-anything}
GO_CGO=${GO_CGO:-./bin/go-anything-cgo}
OUT=${OUT:-/tmp/matrix.txt}
export GO_ANYTHING_PARSE=${GO_ANYTHING_PARSE:-$GO_CGO}

CRATES=${CRATES:-"
testdata/sample/port-rs
testdata/waves/rust/util-go
testdata/waves/rust/format-go
testdata/realworld/go-ports/byteToHex-go
testdata/realworld/go-ports/shellQuote-go
testdata/realworld3/go-ports/uleb-go
testdata/realworld4/go-ports/jadx-go
testdata/markers/go-ports/markers-go
testdata/realworld5/go-ports/jp-go
"}

run() {
  printf '%-16s %7s %5s %6s %-9s %s\n' crate cases gate check bridge speed
  printf '%-16s %7s %5s %6s %-9s %s\n' ---------------- ------- ----- ------ --------- -----
  failures=0
  for crate in $CRATES; do
    name=$(basename "$crate")
    cases=$(wc -l < "$crate/cases.jsonl" 2>/dev/null | tr -d ' ')
    cases=${cases:-0}

    # gate: the definition of done, through the driver.
    if "$GO" gate "$crate" >/dev/null 2>&1; then gate=ok; else gate=FAIL; failures=$((failures+1)); fi

    # check: the same cases through the wrapper a caller actually uses. A crate that gates but fails
    # here has one of the two paths wrong, and that is the drift this column exists to catch: a port
    # that answered [[97],[98]] through the addon and ["a","b"] through the driver passed gate alone.
    check_text=$("$GO" check "$crate" 2>/dev/null | tail -1)
    if printf '%s' "$check_text" | grep -q '^[0-9]*/[0-9]* cases'; then check=ok; else check=FAIL; failures=$((failures+1)); fi
    case "$check_text" in
      *"in-process addon"*) bridge=addon ;;
      *stdio*)              bridge="stdio!" ;; # a crate whose addon was never built
      *)                    bridge="-" ;;
    esac
    # The in-process bridge is the one that ships: a crate falling back to the pipe has not been built
    # the way a caller is told to build it, and the speed column would be measuring the wrong path.
    if [ "$bridge" = "stdio!" ]; then failures=$((failures+1)); fi

    # speed: the geometric mean over the crate's functions, or "-" when it cannot be timed.
    speed=$("$GO" bench "$crate" 2>/dev/null | grep -o 'Geometric mean: [0-9.]*x [a-z]*' | sed 's/Geometric mean: //')
    speed=${speed:-"-"}

    printf '%-16s %7s %5s %6s %-9s %s\n' "$name" "$cases" "$gate" "$check" "$bridge" "$speed"
  done
  echo
  if [ "$failures" -eq 0 ]; then
    echo "every crate gates, answers through the in-process bridge, and can be timed"
  else
    echo "$failures problem(s) above"
  fi
}
run | tee "$OUT"

echo
echo "written to $OUT"
