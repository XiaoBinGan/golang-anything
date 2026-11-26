package record

// pythonDriver records a Python unit: the same generated calls, answered by importing the module.
const pythonDriver = `# The recorder for Python units: the same generated calls the JavaScript recorder makes, answered by
# importing the module instead of a node process.
#
#   python3 record.py <unit.py> <out.jsonl> <plan.json> [extra.jsonl] [only]
#
# The plan is {"name": {"path": "name", "types": ["any", ...]}}; the type table and the walk over it are
# the ones this file defines, so Python gets the same discipline as TypeScript: values by declared type,
# a paired walk, a bounded cross product, and every answer marked the way JSON needs it marked.
import json
import math
import sys

MAX_BYTES = 64 * 1024
PAIRWISE_HEAD = 6

STRINGS = ['', 'a', 'abc', '  ', 'a b  c', "it's", 'a/b', '~0~1', '中文 字', 'a\\b', 'a|b', 'a;b',
           'cmd --flag | tee', '$(id)', '"', "'", '--flag', '0', 'héllo', '😀😀']
NUMBERS = [0, 1, -1, 2, 0.5, -0.5, 1e6, -1e6, float('nan'), float('inf'), float('-inf'), 2 ** 53, 3.0]
BOOLEANS = [True, False]
LISTS = [[], [1], ['a'], [1, 'a'], [None], [[1], [2]]]
NONES = [None]


def values_for(t):
    """Values one declared type may be generated from; an unannotated parameter gets everything."""
    t = (t or 'any').strip()
    values = []
    if t in ('str', 'string', 'String', 'text'):
        values.extend(STRINGS)
    elif t in ('int', 'float', 'number', 'Number'):
        values.extend(NUMBERS)
    elif t in ('bool', 'boolean', 'Boolean'):
        values.extend(BOOLEANS)
    elif t in ('list', 'List', 'array', 'tuple'):
        values.extend(LISTS)
    elif t in ('None', 'NoneType'):
        values.extend(NONES)
    else:
        # unannotated: everything, so a port cannot pass by assuming one kind
        values.extend(STRINGS[:8] + NUMBERS[:8] + BOOLEANS + LISTS[:3] + NONES)
    # repr() keeps NaN, -0 and True apart where a set over the values themselves would not.
    seen, out = set(), []
    for v in values:
        key = repr(v)
        if key in seen:
            continue
        seen.add(key)
        out.append(v)
    return out


def plan_calls(plan, only):
    calls = []
    for name, entry in plan.items():
        if only and name not in only:
            continue
        types = entry.get('types', []) if isinstance(entry, dict) else entry
        path = entry.get('path', name) if isinstance(entry, dict) else name
        positions = [values_for(t) for t in types]
        if not types:
            calls.append((name, path, []))
            continue
        # paired walk: every value at every position, tables stepping together
        longest = max(len(p) for p in positions)
        for i in range(longest):
            calls.append((name, path, [positions[j][i % len(positions[j])] for j in range(len(positions))]))
        # bounded cross product over the first few values of each position
        # Every position, not only the first few: with four or more parameters a bug that needs the last
        # one would never be reached, and ` + "`" + `divide(a, b)` + "`" + ` with b == 0 is exactly that case.
        head = [p[:PAIRWISE_HEAD] for p in positions]
        if len(head) <= 3 and head:
            stack = [[]]
            for column in head:
                stack = [prefix + [v] for prefix in stack for v in column]
            for args in stack:
                calls.append((name, path, args))
        elif head:
            # More positions than the bounded product covers: pair the cheap value at one position with
            # every value at the others, so each position is exercised against a plain argument.
            for j in range(len(head)):
                for value in positions[j]:
                    args = [p[0] for p in positions]
                    args[j] = value
                    calls.append((name, path, args))
    return calls


def mark(v):
    """The wire: what JSON cannot carry, spelled the way both sides agree on."""
    if v is None:
        return None
    if isinstance(v, bool):
        return v
    if isinstance(v, float):
        if math.isnan(v):
            return {'$number': 'NaN'}
        if math.isinf(v):
            return {'$number': 'Infinity' if v > 0 else '-Infinity'}
        if v == 0 and math.copysign(1, v) < 0:
            return {'$number': '-0'}
        if v == int(v) and abs(v) < 2 ** 53:
            return int(v) if float(v).is_integer() else v
        return v
    if isinstance(v, int):
        if abs(v) >= 2 ** 53:
            return {'$bigint': str(v)}
        return v
    if isinstance(v, (list, tuple)):
        return [mark(x) for x in v]
    if isinstance(v, dict):
        return {k: mark(x) for k, x in v.items()}
    if isinstance(v, (bytes, bytearray)):
        return list(v)
    if isinstance(v, str):
        return v
    return {'$repr': repr(v)}


def main():
    unit, out_path, plan_path = sys.argv[1:4]
    extra_path = sys.argv[4] if len(sys.argv) > 4 and sys.argv[4] else ''
    only = set(filter(None, (sys.argv[5] if len(sys.argv) > 5 else '').split(',')))

    # A real project's modules use relative imports (a dot-prefixed import), which need the
    # package context. The module name and the import root come from the Go side; the file load stays as a
    # fallback for a module that is not part of a package at all.
    import importlib.util, os
    root = os.environ.get('GO_ANYTHING_PY_ROOT', '')
    modname = os.environ.get('GO_ANYTHING_PY_MODULE', '')
    module = None
    if root and modname:
        sys.path.insert(0, root)
        try:
            module = importlib.import_module(modname)
        except Exception as exc:
            print('go-anything: importing %s failed (%s); falling back to a file load' % (modname, exc),
                  file=sys.stderr)
    if module is None:
        spec = importlib.util.spec_from_file_location('unit_under_test', unit)
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)

    plan = json.load(open(plan_path))
    calls = plan_calls(plan, only)
    if extra_path:
        with open(extra_path) as f:
            for line in f:
                line = line.strip()
                if not line:
                    continue
                case = json.loads(line)
                calls.append((case.get('fn', ''), case.get('fn', ''), case.get('args', [])))

    written = 0
    with open(out_path, 'w') as out:
        for name, path, args in calls:
            fn = module
            for part in path.split('.'):
                fn = getattr(fn, part, None)
                if fn is None:
                    break
            if fn is None:
                continue
            case = {'fn': name, 'args': [mark(a) for a in args]}
            try:
                answer = fn(*args)
                marked = mark(answer)
                size = len(json.dumps(marked))
                if size > MAX_BYTES:
                    continue
                case['ok'] = marked
            except Exception as exc:  # the original's failure is part of its behaviour
                case['error'] = type(exc).__name__ + ': ' + str(exc)
            out.write(json.dumps(case) + '\n')
            written += 1
    print(f'{written} cases from {unit}', file=sys.stderr)


if __name__ == '__main__':
    main()
`
