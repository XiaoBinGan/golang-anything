#!/usr/bin/env python3
"""Head-to-head evaluation: every axis the objective names, measured the same way on both tools.

Both sides run their own crate, produced by their own `bridge`, and both must pass their own `diff` — a
comparison between a real run and a failed one measures how fast failure is.

Every number is paired: inside one round both tools run, in random order, and the reported value is the
median of the per-round differences with a bootstrap interval. Wall time on a busy machine is mostly the
queue for a process, so CPU time decides the startup axis and is printed beside it.

    ./scripts/eval.sh            # markdown on stdout, JSON in $OUT
"""
import json, os, random, resource, shutil, statistics, subprocess, sys, tempfile, time

ROOT = os.path.dirname(os.path.dirname(os.path.abspath(__file__)))
GO = os.environ.get("GO", os.path.join(ROOT, "bin/go-anything"))
RUST = os.environ.get("RUST_ANYTHING", "/tmp/ra-39/target/release/rust-anything")
GO_CRATE = os.path.join(ROOT, os.environ.get("GO_CRATE", "testdata/sample/port-rs"))
RUST_CRATE = os.environ.get("RUST_CRATE", "/tmp/racmp2/rust/text-rs")
ROUNDS = int(os.environ.get("ROUNDS", "40"))
OUT = os.environ.get("OUT", "/tmp/eval.json")

def run(cmd, env=None, timeout=600):
    return subprocess.run(cmd, capture_output=True, text=True, env=env, timeout=timeout)

def cpu_ms(cmd, env=None):
    before = resource.getrusage(resource.RUSAGE_CHILDREN)
    with open(os.devnull, "wb") as null:
        subprocess.run(cmd, stdout=null, stderr=null, env=env)
    after = resource.getrusage(resource.RUSAGE_CHILDREN)
    return ((after.ru_utime - before.ru_utime) + (after.ru_stime - before.ru_stime)) * 1000

def wall_ms(cmd, env=None):
    start = time.perf_counter_ns()
    with open(os.devnull, "wb") as null:
        subprocess.run(cmd, stdout=null, stderr=null, env=env)
    return (time.perf_counter_ns() - start) / 1e6

def rss_kb(cmd, env=None):
    """Peak resident set of one run, through /usr/bin/time -l on macOS and -v on Linux."""
    fmt = "-l" if sys.platform == "darwin" else "-v"
    r = run(["/usr/bin/time", fmt] + cmd, env=env)
    for line in r.stderr.splitlines():
        line = line.strip()
        if sys.platform == "darwin" and line.endswith("maximum resident set size"):
            return int(line.split()[0]) / (1024 * 1024)  # darwin prints bytes
        if "Maximum resident set size" in line:
            return int(line.split(":")[1].strip()) / 1024
    return float("nan")

def paired(a, b, rounds=ROUNDS, metric=wall_ms):
    """Runs a and b in random order each round; answers (median a, median b, median(a-b), lo, hi).

    The medians are also returned with their minima: on a machine whose load average is in the twenties,
    every median is a measurement of the queue, while the minimum over many runs is the closest thing to
    what the program costs when nothing else wants the CPU.
    """
    A, B, D = [], [], []
    global LAST_MIN
    LAST_MIN = (0.0, 0.0)
    for _ in range(rounds):
        pair = [(0, a), (1, b)]
        random.shuffle(pair)
        got = {}
        for side, cmd in pair:
            got[side] = metric(cmd)
        A.append(got[0]); B.append(got[1]); D.append(got[0] - got[1])
    def ci(xs, n=3000):
        meds = sorted(statistics.median([random.choice(xs) for _ in xs]) for _ in range(n))
        return meds[int(0.025 * n)], meds[int(0.975 * n)]
    lo, hi = ci(D)
    LAST_MIN = (min(A), min(B))
    return statistics.median(A), statistics.median(B), statistics.median(D), lo, hi

def verdict(lo, hi, med):
    if lo < 0 < hi:
        return "indistinguishable"
    return "faster" if med < 0 else "slower"

LAST_MIN = (0.0, 0.0)
MIN_WALL = (0.0, 0.0)

def main():
    results = {}
    notes = []

    load = os.getloadavg()[0] if hasattr(os, "getloadavg") else float("nan")
    notes.append(f"load average {load:.2f} at the start")

    # A comparison is only worth reading if both sides are correct on the crates being timed.
    for name, cmd in (("go-anything", [GO, "diff", GO_CRATE]), ("rust-anything", [RUST, "diff", RUST_CRATE])):
        r = run(cmd)
        if r.returncode != 0:
            print(f"eval: {name} does not pass its own diff; timing it would measure failure:\n{r.stdout}{r.stderr}",
                  file=sys.stderr)
            return 2

    # 1. startup, three shapes
    for label, go_cmd, rust_cmd in (
        ("--help", [GO, "--help"], [RUST, "--help"]),
        ("--version", [GO, "--version"], [RUST, "--help"]),
        ("no arguments", [GO], [RUST]),
    ):
        gw, rw, dw, lo, hi = paired(go_cmd, rust_cmd, metric=wall_ms)
        MIN_WALL = LAST_MIN
        gc_, rc, dc, clo, chi = paired(go_cmd, rust_cmd, metric=cpu_ms)
        results[f"startup {label}"] = {
            "go_wall": gw, "rust_wall": rw, "diff_wall": dw, "wall_ci": [lo, hi],
            "go_cpu": gc_, "rust_cpu": rc, "diff_cpu": dc, "cpu_ci": [clo, chi],
            "verdict": verdict(clo, chi, dc),
            "go_min_wall": MIN_WALL[0], "rust_min_wall": MIN_WALL[1],
        }

    # 2. the bridge, one call at a time
    probe = os.path.join(ROOT, "scripts/floor/bridge-ab.mjs")
    r = run(["node", probe, GO_CRATE, RUST_CRATE])
    results["bridge"] = {"raw": r.stdout.strip(), "ok": r.returncode == 0}

    # 3. diff and record throughput
    def once(cmd):
        start = time.perf_counter_ns()
        out = run(cmd)
        return (time.perf_counter_ns() - start) / 1e6, out
    def count_cases(path):
        with open(path) as f:
            return sum(1 for line in f if line.strip())

    go_cases = count_cases(os.path.join(GO_CRATE, "cases.jsonl"))
    rust_cases = count_cases(os.path.join(RUST_CRATE, "cases.jsonl"))
    for name, gcmd, rcmd, gc_n, rc_n in (
        ("diff", [GO, "diff", GO_CRATE], [RUST, "diff", RUST_CRATE], go_cases, rust_cases),
    ):
        gw, rw, dw, lo, hi = paired(gcmd, rcmd)
        results[name] = {"go_ms": gw, "rust_ms": rw, "diff_ms": dw, "ci": [lo, hi],
                         "go_cases": gc_n, "rust_cases": rc_n,
                         "go_per_case_us": gw / gc_n * 1000, "rust_per_case_us": rw / rc_n * 1000,
                         "verdict": verdict(lo, hi, dw)}

    # record needs the repository above the crate — that is where the unit's file lives — so the copy has
    # to be the whole root, not just the crate folder. Copying only the crate made record fail in 6 ms and
    # the axis report a 50x win, which is what a broken measurement looks like.
    def repo_root(crate, unit):
        d = os.path.abspath(crate)
        while d != "/":
            if os.path.exists(os.path.join(d, unit)):
                return d
            d = os.path.dirname(d)
        raise SystemExit(f"eval: no folder above {crate} holds {unit}")
    tmp = tempfile.mkdtemp()
    try:
        groot = repo_root(GO_CRATE, "src/text.ts")
        rroot = repo_root(RUST_CRATE, "src/text.ts")
        gcopy_root = os.path.join(tmp, "go")
        rcopy_root = os.path.join(tmp, "rust")
        shutil.copytree(groot, gcopy_root, ignore=shutil.ignore_patterns("work", "*.wasm", "addon.node"))
        shutil.copytree(rroot, rcopy_root, ignore=shutil.ignore_patterns("work", "*.wasm", "addon.node"))
        gcopy = os.path.join(gcopy_root, os.path.relpath(os.path.abspath(GO_CRATE), groot))
        rcopy = os.path.join(rcopy_root, os.path.relpath(os.path.abspath(RUST_CRATE), rroot))
        def record_once(cmd, crate):
            # record writes cases.jsonl from scratch, but an existing file makes it compare instead of
            # build; removing it is what puts the work back in the measurement.
            try:
                os.remove(os.path.join(crate, "cases.jsonl"))
            except FileNotFoundError:
                pass
            start = time.perf_counter_ns()
            out = run(cmd)
            return (time.perf_counter_ns() - start) / 1e6, out.returncode
        G, R, codes = [], [], []
        for _ in range(max(5, ROUNDS // 4)):
            g, gc_ = record_once([GO, "record", "--unit", "src/text.ts", gcopy], gcopy)
            r, rc_ = record_once([RUST, "record", rcopy], rcopy)
            G.append(g); R.append(r); codes += [gc_, rc_]
        gw, rw = statistics.median(G), statistics.median(R)
        results["record"] = {"go_ms": gw, "rust_ms": rw, "diff_ms": gw - rw,
                             "go_cases": go_cases, "rust_cases": rust_cases,
                             "exit_codes": sorted(set(codes)),
                             "verdict": ("faster" if gw < rw else "slower") if set(codes) == {0}
                                        else "invalid: a record run failed"}
    finally:
        shutil.rmtree(tmp, ignore_errors=True)

    # 4. memory and size
    results["rss --help"] = {"go_mb": rss_kb([GO, "--help"]), "rust_mb": rss_kb([RUST, "--help"])}
    results["binary size"] = {"go_mb": os.path.getsize(GO) / 1e6, "rust_mb": os.path.getsize(RUST) / 1e6}

    # 5. correctness, so the speed numbers mean something
    gate = run([GO, "gate", GO_CRATE])
    results["correctness"] = {"gate": gate.returncode == 0, "gate_tail": gate.stdout.strip().splitlines()[-1:] }

    with open(OUT, "w") as f:
        json.dump({"load": load, "notes": notes, "results": results}, f, indent=2)
    print(markdown(results, load))
    return 0

def markdown(results, load):
    out = [f"# go-anything vs rust-anything", "",
           f"Load average at the start: **{load:.2f}**. Paired rounds, medians with a bootstrap 95% interval.",
           "", "| axis | go-anything | rust-anything | difference | verdict |", "| --- | ---: | ---: | ---: | --- |"]
    for label, r in results.items():
        if not isinstance(r, dict) or "verdict" not in r:
            continue
        if label.startswith("startup"):
            out.append(f"| {label} (cpu) | {r['go_cpu']:.2f} ms | {r['rust_cpu']:.2f} ms | "
                       f"{r['diff_cpu']:+.2f} ms [{r['cpu_ci'][0]:+.2f}, {r['cpu_ci'][1]:+.2f}] | {r['verdict']} |")
            fastest = "faster" if r["go_min_wall"] < r["rust_min_wall"] else "slower"
            out.append(f"| {label} (wall, best of {ROUNDS}) | {r['go_min_wall']:.2f} ms | {r['rust_min_wall']:.2f} ms | "
                       f"{r['go_min_wall'] - r['rust_min_wall']:+.2f} ms | {fastest} |")
        elif label in ("diff", "record"):
            per_case = ""
            if "go_per_case_us" in r:
                per_case = f" — {r['go_per_case_us']:.1f} vs {r['rust_per_case_us']:.1f} µs/case"
            out.append(f"| {label} | {r['go_ms']:.1f} ms ({r['go_cases']} cases) | "
                       f"{r['rust_ms']:.1f} ms ({r['rust_cases']} cases) | {r['diff_ms']:+.1f} ms | "
                       f"{r['verdict']}{per_case} |")
    for label in ("rss --help", "binary size"):
        r = results.get(label)
        if r:
            unit = "MB"
            out.append(f"| {label} | {r['go_mb']:.1f} {unit} | {r['rust_mb']:.1f} {unit} | | |")
    bridge = results.get("bridge", {})
    if bridge.get("raw"):
        out += ["", "```", bridge["raw"], "```"]
    corr = results.get("correctness", {})
    out += ["", f"Correctness: `gate` on the go crate: {'passes' if corr.get('gate') else 'FAILS'}.",
            "Both tools passed their own `diff` before any timing started."]
    return "\n".join(out)

if __name__ == "__main__":
    sys.exit(main())
