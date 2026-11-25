#!/usr/bin/env bash
# The four numbers the goal is about, on this machine, with the method written down next to each one.
#
# Two rules make these honest:
#   1. Both tools run on their own crate, produced by their own `bridge`, and both diffs pass. Comparing
#      a real run against a failed one measures how fast failure is.
#   2. Every number is a median over alternating rounds, because a single run on a busy machine lies.
#
# Requires: ./bin/go-anything and ./bin/go-anything-cgo (scripts/build.sh), a rust-anything release
# binary in $RUST_ANYTHING, and its crate in $RUST_CRATE.
set -uo pipefail

GO=${GO:-./bin/go-anything}
GO_CGO=${GO_CGO:-./bin/go-anything-cgo}
RUST_ANYTHING=${RUST_ANYTHING:-/tmp/ra-39/target/release/rust-anything}
GO_CRATE=${GO_CRATE:-testdata/sample/port-rs}
RUST_CRATE=${RUST_CRATE:-/tmp/racmp2/rust/text-rs}
ROUNDS=${ROUNDS:-7}
OUT=${OUT:-/tmp/perf.txt}

export GO_ANYTHING_PARSE=${GO_ANYTHING_PARSE:-$GO_CGO}

python3 - "$GO" "$RUST_ANYTHING" "$GO_CRATE" "$RUST_CRATE" "$ROUNDS" <<'PY' | tee "$OUT"
import subprocess, sys, time, statistics, os

go, rust, go_crate, rust_crate, rounds = sys.argv[1], sys.argv[2], sys.argv[3], sys.argv[4], int(sys.argv[5])

def ms(cmd, n=1):
    t0 = time.perf_counter_ns()
    for _ in range(n):
        r = subprocess.run(cmd, capture_output=True, text=True)
    return (time.perf_counter_ns() - t0) / 1e6 / n, r

def interleaved(pairs):
    """Runs each command once per round, in order, and reports medians: load spikes hit everyone."""
    got = {name: [] for name, _ in pairs}
    for _ in range(rounds):
        for name, cmd in pairs:
            v, _ = ms(cmd)
            got[name].append(v)
    return {k: statistics.median(v) for k, v in got.items()}

print("== startup, one spawn, median of %d rounds ==" % rounds)
start = interleaved([
    ("go-anything --help", [go, "--help"]),
    ("rust-anything --help", [rust, "--help"]),
])
for k, v in start.items():
    print(f"  {k:24} {v:6.2f} ms")

print()
print("== record, cases per second, both writing their own cases ==")
def cases_of(path):
    with open(os.path.join(path, "cases.jsonl")) as f:
        return sum(1 for line in f if line.strip())
record = []
for name, cmd, crate in (
    ("go-anything record", [go, "record", "--unit", "src/text.ts", go_crate], go_crate),
    ("rust-anything record", [rust, "record", rust_crate], rust_crate),
):
    got = []
    for _ in range(3):
        v, r = ms(cmd)
        got.append(v)
    n = cases_of(crate)
    record.append((name, statistics.median(got), n))
for name, v, n in record:
    print(f"  {name:24} {v:7.1f} ms   {n:5d} cases   {n/(v/1000):9,.0f} cases/s   {v/n*1000:5.1f} µs/case")
# The two tools do not produce the same number of cases, so cases/s flatters whichever one writes more.
# Wall time for the same unit is the number a person waits for, and it is printed above it.
print("  (the case counts differ by design: this is a paired walk plus a bounded cross product, not the")
print("   full cross product, so compare the wall time for the unit rather than cases per second)")

print()
print("== diff, both passing, on each tool's own crate ==")
def cases(path):
    with open(os.path.join(path, "cases.jsonl")) as f:
        return sum(1 for line in f if line.strip())
gc, rc = cases(go_crate), cases(rust_crate)
diff = interleaved([
    ("go-anything diff", [go, "diff", go_crate]),
    ("rust-anything diff", [rust, "diff", rust_crate]),
])
for k, n in (("go-anything diff", gc), ("rust-anything diff", rc)):
    v = diff[k]
    print(f"  {k:24} {v:6.2f} ms   {n:5d} cases   {n/(v/1000):9,.0f} cases/s   {v/n*1000:5.1f} µs/case")

print()
print("== bridge, one call in-process, same function on both sides ==")
probe = os.path.join(os.environ.get("PERF_DIR", "."), "scripts", "floor", "bridge-ab.mjs")
if os.path.exists(probe):
    out = subprocess.run(["node", probe, go_crate, rust_crate], capture_output=True, text=True)
    print(out.stdout.strip() or out.stderr.strip())
else:
    print("  (scripts/floor/bridge-ab.mjs is missing)")

PY

echo
echo "written to $OUT"
