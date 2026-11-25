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
"}

{
  printf '%-16s %8s %6s %-22s %s\n' crate cases gate bridge speed
  printf '%-16s %8s %6s %-22s %s\n' ---------------- -------- ----- ---------------------- -----
  failures=0
  for crate in $CRATES; do
    name=$(basename "$crate")
    cases=$(wc -l < "$crate/cases.jsonl" 2>/dev/null | tr -d ' ')
    cases=${cases:-0}

    # gate: the definition of done. A crate that fails here is reported as such, not hidden.
    if "$GO" gate "$crate" >/dev/null 2>&1; then gate=ok; else gate=FAIL; failures=$((failures+1)); fi

    # check: the same cases through the wrapper a caller uses, and which bridge answered them.
    check=$("$GO" check "$crate" 2>/dev/null | tail -1)
    case "$check" in
      *"in-process addon"*) bridge=addon ;;
      *"stdio"*)            bridge=stdio ;;
      *)                    bridge="none"; failures=$((failures+1)) ;;
    esac

    # bench: the geometric mean over the crate's functions, or "-" when it cannot be timed.
    speed=$("$GO" bench "$crate" 2>/dev/null | grep -o 'Geometric mean: [0-9.]*x [a-z]*' | sed 's/Geometric mean: //')
    speed=${speed:-"-"}

    printf '%-16s %8s %6s %-22s %s\n' "$name" "$cases" "$gate" "$bridge" "$speed"
  done
  echo
  if [ "$failures" -eq 0 ]; then
    echo "every crate gates and answers through a real bridge"
  else
    echo "$failures problem(s) above"
  fi
} | tee "$OUT"

echo
echo "written to $OUT"
