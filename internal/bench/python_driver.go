package bench

// pythonDriver times a Python unit over its recorded cases, the way originalScript does for node: warm up,
// then repeat until the budget is spent, and report nanoseconds per call.
const pythonDriver = `import importlib.util, json, sys, time

unit, cases_path, out_path, min_ms, repeat = sys.argv[1], sys.argv[2], sys.argv[3], float(sys.argv[4]), int(sys.argv[5])
spec = importlib.util.spec_from_file_location('unit_under_test', unit)
mod = importlib.util.module_from_spec(spec)
spec.loader.exec_module(mod)

cases = [json.loads(line) for line in open(cases_path) if line.strip()]
by_fn = {}
for case in cases:
    by_fn.setdefault(case['fn'], []).append(case.get('args') or [])

rows = []
for name, argsets in by_fn.items():
    fn = getattr(mod, name, None)
    if fn is None:
        continue
    for args in argsets:  # the first call pays for lazy work a caller pays once
        try:
            fn(*args)
        except Exception:
            pass
    rounds, started = 0, time.perf_counter_ns()
    while True:
        for _ in range(repeat):
            for args in argsets:
                try:
                    fn(*args)
                except Exception:
                    pass
        rounds += 1
        elapsed = time.perf_counter_ns() - started
        if elapsed / 1e6 >= min_ms or rounds >= 10000:
            break
    rows.append({'name': name, 'cases': len(argsets), 'ns': elapsed / (rounds * repeat * len(argsets))})

open(out_path, 'w').write(json.dumps(rows))
`
