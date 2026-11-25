#!/usr/bin/env bash
# Measures the four numbers the goal is about, on this machine, and prints them as a table.
#
# Everything is measured the same way: the same crate, the same machine, three runs, the median. A
# number without a command next to it is a claim, not a measurement.
set -u

GO_ANYTHING=${GO_ANYTHING:-/tmp/go-anything}
CRATE=${CRATE:-testdata/sample/port-rs}
RUNS=${RUNS:-3}
OUT=${OUT:-/tmp/perf.txt}

median() { sort -n | awk '{ a[NR]=$1 } END { if (NR % 2) print a[(NR+1)/2]; else print (a[NR/2]+a[NR/2+1])/2 }'; }

# millis runs a command N times and prints the median wall time in milliseconds.
millis() {
  for _ in $(seq "$RUNS"); do
    local start end
    start=$(python3 -c 'import time;print(time.perf_counter_ns())')
    "$@" >/dev/null 2>&1
    end=$(python3 -c 'import time;print(time.perf_counter_ns())')
    python3 -c "print(($end-$start)/1e6)"
  done | median
}

{
  echo "== startup =="
  printf 'help            %8.1f ms\n' "$(millis "$GO_ANYTHING" --help)"
  printf 'doctor          %8.1f ms\n' "$(millis "$GO_ANYTHING" doctor)"
  printf 'analyze (1 file)%8.1f ms\n' "$(millis "$GO_ANYTHING" analyze testdata/sample)"

  echo
  echo "== analyze on the fixture (25 lines, 4 exports) =="
  for _ in $(seq "$RUNS"); do
    "$GO_ANYTHING" analyze testdata/sample >/dev/null 2>&1 || true
  done
  printf 'cold runs       %s x\n' "$RUNS"

  echo
  echo "== diff throughput =="
  CASES=$(wc -l < "$CRATE/cases.jsonl" | tr -d ' ')
  D=$(millis "$GO_ANYTHING" diff "$CRATE")
  printf 'diff %s cases  %8.1f ms  →  %.0f cases/s\n' "$CASES" "$D" \
    "$(python3 -c "print($CASES/($D/1000))")"

  echo
  echo "== record throughput =="
  R=$(millis "$GO_ANYTHING" record --unit src/text.ts "$CRATE")
  printf 'record %s cases %7.1f ms  →  %.0f cases/s\n' "$CASES" "$R" \
    "$(python3 -c "print($CASES/($R/1000))")"

  echo
  echo "== bridge latency (20000 calls, per call) =="
  ( cd "$CRATE" && node /tmp/lat.mjs 2>/dev/null | head -3 )
} | tee "$OUT"

echo
echo "written to $OUT"
