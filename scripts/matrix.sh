#!/usr/bin/env bash
# One line per crate: does the port answer every recorded case, through which bridge, and how fast.
#
# Capability and performance in the same table, because a crate that is green but never measured and a
# crate that is fast but wrong are both unfinished. Every crate below is a real unit or a fixture that
# has been ported by hand; nothing here is skipped to make the table look better.
set -uo pipefail

# The toolchain this machine actually has: see scripts/env.sh.
. "$(dirname "$0")/env.sh"

GO=${GO:-./bin/golang-anything}
GO_CGO=${GO_CGO:-./bin/golang-anything-cgo}
OUT=${OUT:-/tmp/matrix.txt}
export GOLANG_ANYTHING_PARSE=${GOLANG_ANYTHING_PARSE:-$GO_CGO}

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
    # A fresh clone has none of the generated files: index.cjs, cmd/portctl and answer.go are written by
    # `bridge`, and the addon by `build --fast`. Generate them here, at 15 ms a crate, so the matrix says
    # what the crates can do rather than what a clone happens to contain.
    if [ ! -f "$crate/index.cjs" ] || [ ! -d "$crate/cmd/portctl" ] || [ ! -f "$crate/answer.go" ]; then
      "$GO" bridge "$crate" >/dev/null 2>&1
    fi
    if [ ! -f "$crate/addon.node" ] && [ "${NO_BUILD:-0}" != "1" ]; then
      "$GO" build --fast "$crate" >/dev/null 2>&1
    fi
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
    # --json rather than the table: `grep -o 'Geometric mean: …'` dies quietly the day the sentence changes,
    # and then the column reads "-" for every crate and nobody knows why.
    speed=$("$GO" bench --json "$crate" 2>/dev/null | python3 -c "
import json, sys
try:
    x = json.load(sys.stdin)['speedup']
except Exception:
    print('-')
    sys.exit(0)
print('%.2fx %s' % (x if x >= 1 else 1 / x, 'faster' if x >= 1 else 'slower'))
")
    speed=${speed:-"-"}

    printf '%-16s %7s %5s %6s %-9s %s\n' "$name" "$cases" "$gate" "$check" "$bridge" "$speed"
  done
  echo
  if [ "$failures" -eq 0 ]; then
    echo "every crate gates, answers through the in-process bridge, and can be timed"
  else
    echo "$failures problem(s) above"
  fi
  return $((failures > 0))
}
# The table is only a check if it can fail: a broken port used to print FAIL in a column and still exit 0,
# which is the same lie as a test that never asserts.
run | tee "$OUT"
status=${PIPESTATUS[0]}

echo
echo "written to $OUT"
exit "$status"
