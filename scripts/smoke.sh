#!/usr/bin/env bash
# Every command, every flag that changes behaviour, run once on a throwaway copy and reported PASS/FAIL.
# A capability table is a claim; this is the check. Nothing here is skipped to make the table look better:
# a command that cannot run says FAIL with the reason.
set -uo pipefail
. "$(dirname "$0")/env.sh"

GO=${GO:-$(pwd)/bin/golang-anything}
case "$GO" in
  /*) ;;
  *) GO=$(cd "$(dirname "$GO")" && pwd)/$(basename "$GO") ;;
esac
SRC=${SRC:-testdata/sample}
UNIT=${UNIT:-src/text.ts}
WORK=$(mktemp -d)
trap '[ -n "${KEEP:-}" ] || rm -rf "$WORK"' EXIT
[ -n "${KEEP:-}" ] && echo "work dir: $WORK"

pass=0; failed=0
ok()   { printf 'ok    %-46s %s\n' "$1" "${2:-}"; pass=$((pass+1)); }
bad()  { printf 'FAIL  %-46s %s\n' "$1" "${2:-}"; failed=$((failed+1)); }
try()  { local name="$1"; shift; if "$@" >"$WORK/last.log" 2>&1; then ok "$name"; else bad "$name" "$(tail -1 "$WORK/last.log" | cut -c1-70)"; fi; }
tryq() { local name="$1"; shift; if "$@" >"$WORK/last.log" 2>&1; then ok "$name"; else bad "$name" "$(tail -1 "$WORK/last.log" | cut -c1-70)"; fi; }

echo "== the tool itself =="
try "--help"                    "$GO" --help
try "--version"                 "$GO" --version
try "doctor"                    "$GO" doctor
try "unknown command fails"     bash -c "! $GO nonsense"

echo
echo "== plan: analyze, map, scaffold =="
cp -r "$SRC" "$WORK/repo"
REPO="$WORK/repo"
try "analyze"                   "$GO" analyze "$REPO"
try "analyze --json"            bash -c "$GO analyze --json '$REPO' | python3 -c 'import json,sys; json.load(sys.stdin)'"
# A unit whose surface is instance methods has to say so in the table a person reads, not only in --json:
# "0 exports" with no sentence is how a library looks like an empty file.
mkdir -p "$WORK/instances"
printf 'module Util\n  def helper(n)\n    n * 2\n  end\n  def self.double(n)\n    n * 2\n  end\nend\n' > "$WORK/instances/util.rb"
try "analyze prints its notes"  bash -c "$GO analyze '$WORK/instances' | grep -q '## Notes' && $GO analyze '$WORK/instances' | grep -q 'could be promoted'"
# A library whose whole surface is instance methods has nothing for scaffold to see. The shim is what gives it
# an export — so the three steps are checked in order, and reverting has to leave the file alone.
mkdir -p "$WORK/promoted"
printf 'module smoke-promoted\n\ngo 1.24\n' > "$WORK/promoted/go.mod"
printf 'class Timer\n  def double(n)\n    n * 2\n  end\nend\n' > "$WORK/promoted/timer.rb"
try "promote writes a shim"     bash -c "$GO promote --unit timer.rb '$WORK/promoted' && grep -q 'allocate.double' '$WORK/promoted/timer.rb'"
mkdir -p "$WORK/promoted/go-ports"
try "the shim is a real export" bash -c "$GO scaffold --unit timer.rb '$WORK/promoted/go-ports/timer-go' >/dev/null"
# And the whole point: a promoted method records, ports and gates like any other unit. The port answers the
# arity error as well, because the recorder kept the zero-argument call as a case.
if (
  cd "$WORK/promoted" || exit 1
  sed -i '' "s|example.com/timer-go|smoke-promoted/go-ports/timer-go|g" go-ports/timer-go/*.go go-ports/timer-go/golang-anything.json go-ports/timer-go/cmd/*/main.go 2>/dev/null
  python3 -c 'import sys;p=sys.argv[1];s=open(p).read();s=s.replace("return nil, fmt.Errorf(\"not implemented: port double\")","if len(args) != 1 {\n\t\treturn nil, fmt.Errorf(\"ArgumentError: wrong number of arguments (given %d, expected 1)\", len(args))\n\t}\n\treturn int(args[0].(float64)) * 2, nil");open(p,"w").write(s)' go-ports/timer-go/port.go
  "$GO" record --unit timer.rb go-ports/timer-go &&
    "$GO" bridge go-ports/timer-go &&
    "$GO" build --fast go-ports/timer-go &&
    "$GO" gate go-ports/timer-go
) >"$WORK/promote.log" 2>&1; then ok "promote → record → gate"; else bad "promote → record → gate" "$(tail -1 "$WORK/promote.log" | cut -c1-70)"; fi
try "promote --revert"          bash -c "$GO promote --revert --unit timer.rb '$WORK/promoted' && ! grep -q 'allocate.double' '$WORK/promoted/timer.rb'"
# PHP cannot reopen a class, so its route is a proxy class in its own file — and the point of the file is that
# `analyze` then finds the method as an export of its own.
mkdir -p "$WORK/phpprom"
printf '<?php\nclass Timer {\n    public function double(int $n): int { return $n * 2; }\n}\n' > "$WORK/phpprom/timer.php"
try "promote writes a PHP proxy" bash -c "$GO promote --unit timer.php '$WORK/phpprom' && grep -q 'class TimerPromoted' '$WORK/phpprom/promoted.php'"
try "the PHP proxy parses"       bash -c "php -l '$WORK/phpprom/promoted.php'"
# Enabling twice must not write a second block: the marker is what makes the command idempotent, and a second
# `class TimerPromoted` is a fatal error in PHP.
try "promote is idempotent (PHP)" bash -c "$GO promote --unit timer.php '$WORK/phpprom' >/dev/null 2>&1; grep -c 'class TimerPromoted' '$WORK/phpprom/promoted.php' | grep -qx 1"
try "analyze sees the proxy"     bash -c "$GO analyze '$WORK/phpprom' | grep -q 'TimerPromoted.double'"
# And the chain the proxy exists for: record, port, gate. It failed for two rounds — a variadic proxy has no
# export and a proxy that does not require its class records the class error as the case.
printf 'module smoke-phpprom\n\ngo 1.24\n' > "$WORK/phpprom/go.mod"
"$GO" promote --unit timer.php "$WORK/phpprom" >/dev/null 2>&1
"$GO" scaffold --unit promoted.php "$WORK/phpprom/go-ports/p-go" >/dev/null 2>&1
sed -i '' "s|example.com/p-go|smoke-phpprom/go-ports/p-go|g" "$WORK/phpprom/go-ports/p-go"/*.go \
  "$WORK/phpprom/go-ports/p-go/golang-anything.json" "$WORK/phpprom/go-ports/p-go/cmd"/*/main.go 2>/dev/null
python3 -c 'import sys;p=sys.argv[1];s=open(p).read();s=s.replace("return nil, fmt.Errorf(\"not implemented: port double\")","n, ok := args[0].(float64)\n\tif !ok {\n\t\treturn nil, fmt.Errorf(\"TypeError: double() expects int, got %T\", args[0])\n\t}\n\treturn int(n) * 2, nil");open(p,"w").write(s)' \
  "$WORK/phpprom/go-ports/p-go/port.go"
try "PHP record"                bash -c "cd '$WORK/phpprom' && $GO record --unit promoted.php go-ports/p-go"
try "PHP bridge"                bash -c "cd '$WORK/phpprom' && $GO bridge go-ports/p-go"
try "PHP build"                 bash -c "cd '$WORK/phpprom' && $GO build --fast go-ports/p-go"
try "PHP gate"                  bash -c "cd '$WORK/phpprom' && $GO gate go-ports/p-go"
try "PHP promote --revert"      bash -c "$GO promote --revert --unit timer.php '$WORK/phpprom' && [ ! -f '$WORK/phpprom/promoted.php' ]"
try "map"                       "$GO" map "$REPO"
mkdir -p "$REPO/go-ports"
try "scaffold --unit"           "$GO" scaffold --unit "$UNIT" "$REPO/go-ports/one-go"
sed -i '' "s|example.com/one-go|smoke/go-ports/one-go|g" "$REPO/go-ports/one-go"/*.go \
  "$REPO/go-ports/one-go/golang-anything.json" "$REPO/go-ports/one-go"/cmd/*/main.go 2>/dev/null
printf 'module smoke\n\ngo 1.24\n' > "$REPO/go.mod"
CRATE="$REPO/go-ports/one-go"

echo
echo "== record: types, flags, fingerprints =="
try "record"                    "$GO" record --unit "$UNIT" "$CRATE"
# The flags are --inputs <jsonl> and --test-coverage <command>, not the shapes I guessed first: the smoke
# test is what caught me writing the wrong invocation and calling the feature missing.
try "record --only"              "$GO" record --unit "$UNIT" --only count "$CRATE"
try "record --inputs"           bash -c "printf '{\"fn\":\"count\",\"args\":[\"a b\"]}\n' > $WORK/in.jsonl && $GO record --unit $UNIT --inputs $WORK/in.jsonl $CRATE"
try "record --test-coverage"    bash -c "$GO record --unit $UNIT --test-coverage 'true' $CRATE"
try "record writes a fingerprint" test -s "$CRATE/recorded-from.json"

echo
echo "== the port: stubs, driver, wrappers =="
try "gate (stubs fail, as they must)" bash -c "! $GO gate '$CRATE'"
try "bridge"                    "$GO" bridge "$CRATE"
try "refresh keeps port.go"     bash -c "cp '$CRATE/port.go' $WORK/port.before && $GO refresh '$CRATE' && cmp -s '$CRATE/port.go' $WORK/port.before"
# The generated driver is compiled by nobody: `build` writes portctl and gobench, and `go build ./...` is what a
# user runs. cmd/godriver named port.Undefined from answer.go, so a scaffold without a bridge did not compile —
# and nothing in the suite could have noticed.
try "the generated driver builds" bash -c "cd '$CRATE' && go build ./..."
try "build"                     "$GO" build "$CRATE"
try "build --fast"              "$GO" build --fast "$CRATE"
try "wasm"                      "$GO" wasm "$CRATE"

echo
echo "== the finished port: gate, check, diff, bench, fuzz =="
cp testdata/sample/port-rs/port.go "$CRATE/port.go"
# The addon carries a compiled copy of port.go: replacing the source without rebuilding it leaves check
# comparing against the stubs, which reads as 328 differences in a port that is right.
"$GO" build --fast "$CRATE" >/dev/null 2>&1
try "gate (finished port)"      "$GO" gate "$CRATE"
try "diff"                      "$GO" diff "$CRATE"
try "check (in-process)"        "$GO" check "$CRATE"
try "bench"                     "$GO" bench "$CRATE"
# The table is for a person; a row another tool reads should come out as JSON rather than be scraped.
try "bench --json"              bash -c "$GO bench --json '$CRATE' | python3 -c 'import json,sys; d=json.load(sys.stdin); sys.exit(0 if d[\"rows\"] and d[\"rows\"][0][\"port_ns\"] > 0 else 1)'"
# The row has to say which path it timed: 1.72x through the typed entry point and 3.28x through the registry are
# both real measurements of the same crate.
try "bench --json says typed"   bash -c "$GO bench --json '$CRATE' | python3 -c 'import json,sys; rows=json.load(sys.stdin)[\"rows\"]; sys.exit(0 if any(r.get(\"typed\") for r in rows) else 1)'"
try "fuzz --count --seed"       "$GO" fuzz --count 20 --seed 7 "$CRATE"
try "fuzz --keep"               "$GO" fuzz --count 20 --seed 8 --keep "$CRATE"
# diff has no --json: --json belongs to analyze and doctor, and writing a test for a flag that does not
# exist says nothing about either. The report is what carries diff's numbers.
# Parsing is not enough: the JSON has to carry the checks the text output prints, or a caller that reads JSON
# sees fewer checks than a person does and cannot tell.
try "doctor --json names every check" bash -c "$GO doctor --json | python3 -c 'import json,sys; names={c[\"name\"] for c in json.load(sys.stdin)}; sys.exit(0 if {\"go\", \"node\", \"clang (bench)\", \"cgo\"} <= names else 1)'"

echo
echo "== takeover: enable, disable, shadow =="
try "enable"                    "$GO" enable "$CRATE"
try "disable (byte for byte)"   bash -c "$GO disable '$CRATE' && cmp -s '$REPO/$UNIT' '$SRC/$UNIT'"
try "enable --only"             "$GO" enable --only count "$CRATE"
try "enable --async"            "$GO" enable --async "$CRATE"
try "disable after --async"     "$GO" disable "$CRATE"
try "shadow"                    "$GO" shadow "$CRATE"
try "shadow-report"             "$GO" shadow-report "$CRATE"

echo
echo "== reports and packaging =="
try "report"                    "$GO" report "$CRATE"
try "report --out"              "$GO" report --out "$WORK/out" "$CRATE"
try "package (library)"         "$GO" package --name smoke --version 0.1.0 "$CRATE"
try "package --shape cli"       "$GO" package --shape cli --name smokecli --version 0.1.0 "$CRATE"
try "package --shape service"   "$GO" package --shape service --name smokesvc --version 0.1.0 "$CRATE"

echo
echo "== whole programs: app record, replay, front door =="
# The repository root comes before the flags: these take `app record <repo> --command ...`.
mkdir -p "$WORK/plain"
# --command takes a program, not a shell line: this was the smoke test's mistake, not the tool's.
try "app record"                "$GO" app record --command /bin/echo --args hello "$WORK/plain"
try "app replay"                "$GO" app replay --command /bin/echo "$WORK/plain"
try "front-door new"            "$GO" front-door new --name front "$WORK/plain"
# new writes <repo>/go-app; wire and lift take that folder, not the repository above it.
# The recorded commands are the ones app record saw: --help, --version and no arguments. Wiring one of
# those is the smallest honest case; "--count" was never recorded and wire refuses it, which is right.
# A recorded command's key includes the program name: `app record --command /bin/echo --args hello`
# records "hello", "hello --help" and "hello --version", so that is what wire matches on.
try "front-door wire"           "$GO" front-door wire --command "hello --version" --port smoke/go-ports/one-go --call count "$WORK/plain/go-app"
# lift only permits a full takeover, which is the design: an unwired command with the fallback removed
# would be a command that stops working. So lifting here must be refused, and the refusal is the check.
try "front-door wire (second)"  "$GO" front-door wire --command "hello --help" --port smoke/go-ports/one-go --call count "$WORK/plain/go-app"
try "front-door lift refuses a partial takeover" bash -c "! $GO front-door lift '$WORK/plain/go-app'"

echo
echo "== profiling and the whole loop =="
try "profile"                   "$GO" profile --command "-e process.exit(0)" --top 3 "$REPO"
try "go --unit (profile, scaffold)" "$GO" go --command "-e process.exit(0)" --unit "$UNIT" "$REPO"

echo
echo "== second language: Python =="
mkdir -p "$WORK/pyroot"
cat > "$WORK/pyroot/mathx.py" <<'PYEOF'
def add(a, b):
    return a + b

def divide(a, b):
    if b == 0:
        raise ValueError("division by zero")
    return a / b
PYEOF
try "analyze names python"      bash -c "$GO analyze '$WORK/pyroot' | head -1 | grep -q python"
try "scaffold a .py unit"       "$GO" scaffold --unit mathx.py "$WORK/pyroot/go-ports/mathx-go"
sed -i '' "s|example.com/mathx-go|pyroot/go-ports/mathx-go|g" "$WORK/pyroot/go-ports/mathx-go"/*.go \
  "$WORK/pyroot/go-ports/mathx-go/golang-anything.json" "$WORK/pyroot/go-ports/mathx-go"/cmd/*/main.go 2>/dev/null
printf 'module pyroot\n\ngo 1.24\n' > "$WORK/pyroot/go.mod"
try "record a .py unit"         "$GO" record --unit mathx.py "$WORK/pyroot/go-ports/mathx-go"
try "record caught the raise"   bash -c "grep -q 'division by zero' '$WORK/pyroot/go-ports/mathx-go/cases.jsonl'"
# It must refuse with the reason, not fail with node's error: the check is that the sentence is there.
try "fuzz says why not yet"     bash -c "$GO fuzz --count 5 '$WORK/pyroot/go-ports/mathx-go' 2>&1 | grep -q 'Python runner'"
try "analyze names what it cannot read" bash -c "mkdir -p $WORK/sw && echo 'func f() {}' > $WORK/sw/A.swift && $GO analyze '$WORK/sw' | grep -q 'does not read'"

echo
echo "== the other languages: one unit each, end to end =="
try "ruby: analyze finds it"     bash -c "mkdir -p $WORK/rb && printf 'def snake(name)\n  name.to_s.downcase\nend\n' > $WORK/rb/s.rb && $GO analyze '$WORK/rb' | grep -q ruby"
try "ruby: record runs the original" bash -c "cd '$WORK/rb' && printf 'module rb\\n\\ngo 1.24\\n' > go.mod && $GO scaffold --unit s.rb go-ports/s-go >/dev/null 2>&1 && $GO record --unit s.rb go-ports/s-go 2>&1 | grep -q 'cases'"
try "java: analyze finds it"     bash -c "mkdir -p $WORK/jv && printf 'public final class M {\n  public static int twice(int n) { return n * 2; }\n}\n' > $WORK/jv/M.java && $GO analyze '$WORK/jv' | grep -q java"
try "java: record runs the original" bash -c "cd '$WORK/jv' && printf 'module jv\\n\\ngo 1.24\\n' > go.mod && $GO scaffold --unit M.java go-ports/m-go >/dev/null 2>&1 && $GO record --unit M.java go-ports/m-go 2>&1 | grep -q 'cases'"
try "c: analyze finds it"        bash -c "mkdir -p $WORK/cc && printf 'int twice(int n) { return n * 2; }\n' > $WORK/cc/m.c && $GO analyze '$WORK/cc' | grep -q '(c)'"
try "c: record runs the original" bash -c "cd '$WORK/cc' && printf 'module cc\\n\\ngo 1.24\\n' > go.mod && $GO scaffold --unit m.c go-ports/m-go >/dev/null 2>&1 && $GO record --unit m.c go-ports/m-go 2>&1 | grep -q 'cases'"
try "c++: analyze finds it"      bash -c "mkdir -p $WORK/cx && printf 'int twice(int n) { return n * 2; }\n' > $WORK/cx/m.cpp && $GO analyze '$WORK/cx' | grep -q cpp"
try "c++: record runs the original" bash -c "cd '$WORK/cx' && printf 'module cx\\n\\ngo 1.24\\n' > go.mod && $GO scaffold --unit m.cpp go-ports/m-go >/dev/null 2>&1 && $GO record --unit m.cpp go-ports/m-go 2>&1 | grep -q 'cases'"
try "swift is named as unread"   bash -c "mkdir -p $WORK/sw2 && echo 'func f() {}' > $WORK/sw2/A.swift && $GO analyze '$WORK/sw2' | grep -q 'does not read'"

echo
echo "== the four that arrived last =="
try "php: analyze + record"      bash -c "mkdir -p $WORK/ph && printf '<?php function twice(int \$n): int { return \$n * 2; }' > $WORK/ph/m.php && $GO analyze '$WORK/ph' | grep -q '(php)' && cd '$WORK/ph' && printf 'module ph\\n\\ngo 1.24\\n' > go.mod && $GO scaffold --unit m.php go-ports/m-go >/dev/null 2>&1 && $GO record --unit m.php go-ports/m-go 2>&1 | grep -q 'Recorded'"
try "kotlin: analyze + record"   bash -c "mkdir -p $WORK/kl && printf 'fun twice(n: Int): Int = n * 2\n' > $WORK/kl/M.kt && $GO analyze '$WORK/kl' | grep -q '(kotlin)' && cd '$WORK/kl' && printf 'module kl\\n\\ngo 1.24\\n' > go.mod && $GO scaffold --unit M.kt go-ports/m-go >/dev/null 2>&1 && $GO record --unit M.kt go-ports/m-go 2>&1 | grep -q 'Recorded'"
try "csharp: analyze + record"   bash -c "mkdir -p $WORK/cs0 && printf 'public static class M {\n  public static int Twice(int n) { return n * 2; }\n}\n' > $WORK/cs0/M.cs && $GO analyze '$WORK/cs0' | grep -q '(csharp)' && cd '$WORK/cs0' && printf 'module cs0\\n\\ngo 1.24\\n' > go.mod && $GO scaffold --unit M.cs go-ports/m-go >/dev/null 2>&1 && $GO record --unit M.cs go-ports/m-go 2>&1 | grep -q 'Recorded'"
# Every language's crate has to bench: the harness for each is generated source, and one that does not
# compile is a language that half works. scripts/bench-all-languages.sh runs the same sweep from a clean
# scaffold; this checks the crates the blocks above already made.
try "bench works on every language" ./scripts/bench-all-languages.sh

try "powershell: analyze + record" bash -c "mkdir -p $WORK/ps0 && printf 'function Get-Twice {\n  param([int]\$N)\n  return \$N * 2\n}\n' > $WORK/ps0/M.ps1 && $GO analyze '$WORK/ps0' | grep -q '(powershell)' && cd '$WORK/ps0' && printf 'module ps0\\n\\ngo 1.24\\n' > go.mod && $GO scaffold --unit M.ps1 go-ports/m-go >/dev/null 2>&1 && $GO record --unit M.ps1 go-ports/m-go 2>&1 | grep -q 'Recorded'"

echo
echo "== class methods, the shape a real library is made of =="
try "ruby: module functions"     bash -c "mkdir -p $WORK/rbm && printf 'module U\n  def self.double(n)\n    n * 2\n  end\nend\n' > $WORK/rbm/u.rb && $GO analyze '$WORK/rbm' | grep -q 'U.double'"
try "php: static methods"        bash -c "mkdir -p $WORK/phs && printf '<?php class U { public static function double(int \$n): int { return \$n * 2; } }' > $WORK/phs/u.php && $GO analyze '$WORK/phs' | grep -q 'U.double'"

echo
echo "== takeover in Ruby: enable, call, disable =="
takeover_ruby() {
  local d="$WORK/rbt" crate="$WORK/rbt/go-ports/u-go"
  mkdir -p "$d" && cp /tmp/neg-rb-util.rb "$d/util.rb" 2>/dev/null || cat > "$d/util.rb" <<'RB'
module Util
  def self.double(n)
    n * 2
  end
end
RB
  ( cd "$d" && printf 'module rbt\n\ngo 1.24\n' > go.mod
    "$GO" scaffold --unit util.rb go-ports/u-go >/dev/null 2>&1
    sed -i '' "s|example.com/u-go|rbt/go-ports/u-go|g" go-ports/u-go/*.go go-ports/u-go/golang-anything.json go-ports/u-go/cmd/*/main.go 2>/dev/null
    "$GO" record --unit util.rb go-ports/u-go >/dev/null 2>&1
    cat > go-ports/u-go/port.go <<'GO'
package port

import "fmt"

type Fn func(args []any) (any, error)

var Registry = map[string]Fn{"double": double}

func double(args []any) (any, error) {
	switch v := args[0].(type) {
	case float64:
		return v * 2, nil
	case string:
		return v + v, nil
	case nil:
		return nil, fmt.Errorf("NoMethodError: undefined method `*' for nil:NilClass")
	case bool:
		return nil, fmt.Errorf("NoMethodError: undefined method `*' for %v:TrueClass", v)
	case []any:
		return append(append([]any{}, v...), v...), nil
	}
	return nil, fmt.Errorf("TypeError: double needs a number or a string")
}
GO
    "$GO" bridge go-ports/u-go >/dev/null 2>&1
    "$GO" gate go-ports/u-go >/dev/null 2>&1 || { echo "the port does not gate"; exit 1; }
    "$GO" build go-ports/u-go >/dev/null 2>&1
    cp util.rb util.before
    "$GO" enable go-ports/u-go >/dev/null 2>&1 || { echo "enable failed"; exit 1; }
    got=$(ruby -I. -e "require 'util'; puts Util.double(21)" 2>&1 | tail -1)
    [ "$got" = "42" ] || { echo "the switched call answered $got"; exit 1; }
    "$GO" disable go-ports/u-go >/dev/null 2>&1 || { echo "disable failed"; exit 1; }
    cmp -s util.rb util.before || { echo "disable did not restore the file"; exit 1; }
  )
}
try "ruby: enable, call through the port, disable" takeover_ruby
# fuzz runs the original through its own runtime: Ruby has a runner now, and a fresh call has to reach a
# module function through its owner.
try "ruby: fuzz fresh inputs"     bash -c "cd '$WORK/rbt' && '$GO' fuzz --count 10 --seed 3 go-ports/u-go 2>&1 | grep -q 'fresh calls match'"

echo
echo "== agents =="
try "setup --print"             "$GO" setup --print
try "mcp answers tools/list"    bash -c "printf '%s\n' '{\"jsonrpc\":\"2.0\",\"id\":1,\"method\":\"tools/list\"}' | $GO mcp | python3 -c 'import json,sys; d=json.loads(sys.stdin.readline()); assert d[\"result\"][\"tools\"]'"

echo
printf '%d passed, %d failed\n' "$pass" "$failed"
exit $((failed > 0))
