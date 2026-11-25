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

# The toolchain this machine actually has: see scripts/env.sh.
. "$(dirname "$0")/env.sh"

GO=${GO:-./bin/go-anything}
GO_CGO=${GO_CGO:-./bin/go-anything-cgo}
RUST_ANYTHING=${RUST_ANYTHING:-/tmp/ra-39/target/release/rust-anything}
GO_CRATE=${GO_CRATE:-testdata/sample/port-rs}
RUST_CRATE=${RUST_CRATE:-/tmp/racmp2/rust/text-rs}
ROUNDS=${ROUNDS:-7}
OUT=${OUT:-/tmp/perf.txt}

# A missing rig gives a traceback or a table of zeroes, and both read like a measurement. Refuse instead:
# the numbers are only worth printing when both sides are there and both crates answer.
# The startup axis compares the fast build. A cgo build left in place by hand adds ~0.15 ms and reads as a
# regression: CI never catches it because nothing builds over it there, and the shell does.
if ! go version -m "$GO" 2>/dev/null | grep -q "CGO_ENABLED=0"; then
  echo "perf: $GO is not the no-cgo build; the startup axis would measure the wrong binary" >&2
  echo "      run ./scripts/build.sh first" >&2
  exit 2
fi

for needed in "$RUST_ANYTHING" "$RUST_CRATE/cases.jsonl" "$GO_CRATE/cases.jsonl" "$GO_CGO"; do
  if [ ! -e "$needed" ]; then
    echo "perf: nothing to measure — $needed is not there" >&2
    echo "      set RUST_ANYTHING and RUST_CRATE, and run scripts/build.sh first" >&2
    exit 2
  fi
done
if ! "$GO" diff "$GO_CRATE" >/dev/null 2>&1; then
  echo "perf: the go crate does not diff; timing it would measure failure" >&2
  exit 2
fi
if ! "$RUST_ANYTHING" diff "$RUST_CRATE" >/dev/null 2>&1; then
  echo "perf: the rust crate does not diff; timing it would measure failure" >&2
  exit 2
fi

export GO_ANYTHING_PARSE=${GO_ANYTHING_PARSE:-$GO_CGO}

python3 - "$GO" "$RUST_ANYTHING" "$GO_CRATE" "$RUST_CRATE" "$ROUNDS" <<'PY' | tee "$OUT"
import subprocess, sys, time, statistics, os, resource, random

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

try:
    load = os.getloadavg()[0]
except OSError:
    load = float("nan")
print("== startup, one spawn, median of %d rounds ==" % rounds)
print(f"  (load average {load:.2f}: the paired interval is what to read, not the medians)")
# Wall time on a busy machine measures the machine: the process-spawn floor moves by a millisecond between
# runs, and an early version of this axis reported +0.07 ms one day and +1.3 ms another for the same
# binaries. CPU time of the child does not move with load, so it decides the axis and wall time is printed
# beside it for context.
def cpu_ms(cmd):
    before = resource.getrusage(resource.RUSAGE_CHILDREN)
    subprocess.run(cmd, capture_output=True)
    after = resource.getrusage(resource.RUSAGE_CHILDREN)
    return ((after.ru_utime - before.ru_utime) + (after.ru_stime - before.ru_stime)) * 1000

pairs = [("go-anything --help", [go, "--help"]), ("rust-anything --help", [rust, "--help"])]
samples = max(rounds * 6, 40)
wall = {name: [] for name, _ in pairs}
cpu = {name: [] for name, _ in pairs}
diffs = []
for _ in range(samples):
    round_cpu = {}
    # Random order inside each round: measuring one tool first every time gives it the colder cache, and
    # with a gap of a tenth of a millisecond that bias is bigger than the gap.
    order = pairs[:]
    random.shuffle(order)
    for name, cmd in order:
        wall[name].append(ms(cmd)[0])
        round_cpu[name] = cpu_ms(cmd)
        cpu[name].append(round_cpu[name])
    diffs.append(round_cpu[pairs[0][0]] - round_cpu[pairs[1][0]])
for name, _ in pairs:
    print(f"  {name:24} cpu {statistics.median(cpu[name]):5.2f} ms   wall {statistics.median(wall[name]):5.2f} ms")

# Paired differences with a bootstrap interval, because the medians alone move by half a millisecond with
# the load on the machine and the gap being decided is smaller than that.
def bootstrap_ci(xs, n=4000):
    meds = sorted(statistics.median([random.choice(xs) for _ in xs]) for _ in range(n))
    return meds[int(0.025 * n)], meds[int(0.975 * n)]

lo, hi = bootstrap_ci(diffs)
med = statistics.median(diffs)
verdict = "the intervals overlap zero: not distinguishable" if lo < 0 < hi else (
    "slower" if med > 0 else "faster")
print(f"  {'paired difference':24} {med:+5.3f} ms   95% CI [{lo:+.3f}, {hi:+.3f}]  -> {verdict}")
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
bridge_status = 0
if os.path.exists(probe):
    out = subprocess.run(["node", probe, go_crate, rust_crate], capture_output=True, text=True)
    print(out.stdout.strip() or out.stderr.strip())
    # The probe exits non-zero when the wasm bridge is not faster than napi: that is the axis the goal is
    # about, so a run where it lost is a failed run, not a table to read.
    bridge_status = out.returncode
else:
    print("  (scripts/floor/bridge-ab.mjs is missing)")
    bridge_status = 1

if bridge_status != 0:
    # The bridge axis is the one the goal is about: a run where the wasm path lost is a failed run.
    sys.exit(bridge_status)

PY

status=$?

echo
echo "written to $OUT"
exit "$status"
