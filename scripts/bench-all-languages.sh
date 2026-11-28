#!/usr/bin/env bash
# Bench every language from a clean scaffold: one unit per language, recorded by its own runtime and timed
# against the Go port. A language whose timing harness does not compile fails here rather than in front of a
# user.
#
#   ./scripts/bench-all-languages.sh
set -uo pipefail
. "$(dirname "$0")/env.sh"

ROOT=$(cd "$(dirname "$0")/.." && pwd)
W=$(mktemp -d)
trap 'rm -rf "$W"' EXIT
G=${GO:-$ROOT/bin/golang-anything}
bad=0
run() {
  local name=$1 file=$2 unit=$3
  local d=$W/$name; mkdir -p $d
  cp "$file" $d/
  ( cd $d && printf 'module %s\n\ngo 1.24\n' "$name" > go.mod
    $G scaffold --unit "$unit" go-ports/m-go >/dev/null 2>&1
    sed -i '' "s|example.com/m-go|$name/go-ports/m-go|g" go-ports/m-go/*.go go-ports/m-go/golang-anything.json go-ports/m-go/cmd/*/main.go 2>/dev/null
    $G record --unit "$unit" go-ports/m-go >/dev/null 2>&1
    # Scaffold writes `return nil, fmt.Errorf("not implemented: port twice")`, and benching that times
    # fmt.Errorf, not a port: the same C crate read 17 ns / 8.83x slower as a stub and 5 ns / 3.10x slower
    # with one line of real body. The nine toy units all have the same signature `twice(n: int) -> int`, so
    # the same body fills all nine, and the numbers below are a comparison rather than a placeholder.
    python3 - go-ports/m-go/port.go <<'PYP'
import re, sys
p = sys.argv[1]
src = open(p).read()
if 'not implemented: port' not in src:
    sys.exit(0)
# One body for all nine toy units: they are all `twice(n: int) -> int`. The argument arrives as float64 from
# the JSON cases for most recorders and as a string for Ruby, so both are handled rather than guessing which
# recorder this language has.
filled = '''
	switch v := args[0].(type) {
	case float64:
		return int(v) * 2, nil
	case int:
		return v * 2, nil
	case int64:
		return int(v) * 2, nil
	case string:
		n := 0
		for _, c := range v {
			if c >= '0' && c <= '9' {
				n = n*10 + int(c-'0')
			}
		}
		return n * 2, nil
	}
	return nil, fmt.Errorf("twice: %T", args[0])'''
src = re.sub(r'\treturn nil, fmt\.Errorf\("not implemented: port [\w-]+"\)', filled, src)
# The typed entry point delegates until someone writes it, and a delegating stub is what bench would then time:
# assertion plus box, on top of the call. Filling it makes the typed path the direct one, which is the whole
# reason bench prefers it.
# Java and Kotlin get the 32-bit width, so their typed entry point takes int32: filling only the `int` form left a
# delegating stub, which bench then timed as if it were the port.
typed = 'func Twice(n int) (int, error) {\n\treturn n * 2, nil\n}'
src = re.sub(r'func Twice\(n int\) \(int, error\) \{\n(?:.*\n)*?\}', typed, src, count=1)
typed32 = 'func Twice(n int32) (int32, error) {\n\treturn n * 2, nil\n}'
src = re.sub(r'func Twice\(n int32\) \(int32, error\) \{\n(?:.*\n)*?\}', typed32, src, count=1)
open(p, 'w').write(src)
PYP
    $G build --fast go-ports/m-go >/dev/null 2>&1
    # --json, not the table: scraping `Geometric mean: …` out of markdown breaks silently the day the
    # sentence is reworded, and the numbers are the whole point of this script.
    # stdout only: bench writes `sink=…` to stderr, and merging the two turned the JSON into a parse error
    # for five of the nine languages on the first try.
    out=$($G bench --json go-ports/m-go 2>"$W/$name.err") || { echo "FAIL $name: $(tail -1 "$W/$name.err")"; exit 1; }
    # A reading above the band PERF.md documents is a reason to measure again, not a verdict: the same commit measured
    # 2.10x and 1.72x an hour apart. The second reading is printed next to the first, and the verdict uses it.
    # Per language, because the documented bands differ: c reads 1.72–2.07 and cpp 1.72–2.10. One shared number let a
    # c reading of 2.10 through unmeasured while it sat outside c's own band.
    case "$name" in
        c)   rerun_above=${BENCH_RERUN_ABOVE:-2.07} ;;
        cpp) rerun_above=${BENCH_RERUN_ABOVE:-2.10} ;;
        *)   rerun_above=${BENCH_RERUN_ABOVE:-2.10} ;;
    esac
    read -r factor slower < <(printf '%s' "$out" | python3 -c "
import json, sys
d = json.load(sys.stdin)
x = d['speedup']
print('%.4f' % (x if x >= 1 else 1 / x), 'no' if x >= 1 else 'yes')
")
    if [ "$slower" = yes ] && python3 -c "import sys; sys.exit(0 if $factor > $rerun_above else 1)"; then
        printf 'note  %s: %.2fx slower, above the %.2fx band — measuring again\n' "$name" "$factor" "$rerun_above"
        second=$($G bench --json go-ports/m-go 2>>"$W/$name.err") || { echo "FAIL $name: $(tail -1 "$W/$name.err")"; exit 1; }
        second_factor=$(printf '%s' "$second" | python3 -c "
import json, sys
d = json.load(sys.stdin)
x = d['speedup']
print('%.4f' % (x if x >= 1 else 1 / x))
")
        printf 'note  %s: second reading %.2fx slower\n' "$name" "$second_factor"
        out=$second
    fi
    printf '%s' "$out" | python3 -c "
import json, sys
d = json.load(sys.stdin)
x = d['speedup']
print('ok   %s: %.2fx %s over %d function(s), %d ms per side minimum.' % (
    '$name', x if x >= 1 else 1 / x, 'faster' if x >= 1 else 'slower', len(d['rows']), d['min_ms']))
" ) || bad=$((bad + 1))
}
mkdir -p $W/src
printf 'fun twice(n: Int): Int = n * 2\n' > $W/src/M.kt
printf 'public static class M {\n  public static int Twice(int n) { return n * 2; }\n}\n' > $W/src/M.cs
printf 'function Get-Twice {\n  param([int]$N)\n  return $N * 2\n}\n' > $W/src/M.ps1
printf '<?php function twice(int $n): int { return $n * 2; }' > $W/src/m.php
printf 'def twice(n)\n  n * 2\nend\n' > $W/src/m.rb
printf 'def twice(n):\n    return n * 2\n' > $W/src/m.py
printf 'public final class M {\n  public static int twice(int n) { return n * 2; }\n}\n' > $W/src/M.java
printf 'int twice(int n) { return n * 2; }\n' > $W/src/m.c
printf 'int twice(int n) { return n * 2; }\n' > $W/src/m.cpp
run kt $W/src/M.kt M.kt
run cs $W/src/M.cs M.cs
run ps $W/src/M.ps1 M.ps1
run php $W/src/m.php m.php
run rb $W/src/m.rb m.rb
run py $W/src/m.py m.py
run java $W/src/M.java M.java
run c $W/src/m.c m.c
run cpp $W/src/m.cpp m.cpp

echo
if [ "$bad" -eq 0 ]; then
  echo "every language that has a runtime here benches its own unit"
else
  echo "$bad language(s) could not bench: this is a failing gate, not a partial result"
  exit 1
fi
