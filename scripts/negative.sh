#!/usr/bin/env bash
# The checks can fail. Every script in this folder that reports a verdict is run once against a known
# break, and must come back non-zero; a check that cannot fail is decoration.
#
# This is slow and it breaks things on purpose, so it is not part of `make check`: it is the reason to
# believe `make check`.
set -uo pipefail

# The toolchain this machine actually has: see scripts/env.sh.
. "$(dirname "$0")/env.sh"

GO=${GO:-./bin/golang-anything}
# Absolute: several controls cd into a temporary crate, and a relative path stops resolving there. A control
# that "passes" with exit 127 is a control that never ran.
case "$GO" in
  /*) ;;
  *) GO=$(cd "$(dirname "$GO")" && pwd)/$(basename "$GO") ;;
esac
ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT" || exit 1

failures=0
# expect_ok is for a line that asserts a refusal: it passes when the check fires. Written this way because
# "the rule still refuses this" is itself a claim that can stop being true.
expect_ok() {
  local what="$1"
  local status="$2"
  if [ "$status" -eq 0 ]; then
    printf 'ok    %s\n' "$what"
  else
    printf 'FAIL  %s stopped holding\n' "$what"
    failures=$((failures + 1))
  fi
}

expect_fail() {
  local what="$1"
  local status="$2"
  if [ "$status" -ne 0 ]; then
    printf 'ok    %s (exit %s)\n' "$what" "$status"
  else
    printf 'FAIL  %s exited 0 on a known break\n' "$what"
    failures=$((failures + 1))
  fi
}

# 1. matrix: a port that answers its input unchanged must not pass the table.
crate=testdata/realworld5/go-ports/jp-go
cp "$crate/port.go" /tmp/negative-port.go
python3 - "$crate/port.go" <<'PY'
import sys
p = sys.argv[1]
s = open(p).read()
s = s.replace('return strings.ReplaceAll(strings.ReplaceAll(token, "~", "~0"), "/", "~1"), nil',
              '_ = strings.TrimSpace\n\treturn token, nil')
open(p, 'w').write(s)
PY
CRATES="$crate" ./scripts/matrix.sh >/dev/null 2>&1
expect_fail "matrix rejects a broken port" $?
cp /tmp/negative-port.go "$crate/port.go"

# 2. contract: a second copy of a shared implementation must be reported.
cp internal/shadow/shadow.go /tmp/negative-shadow.go
printf '\nfunc canonCopy(v any) any { return v }\n' >> internal/shadow/shadow.go
sed -i '' 's|%JSWIRE_CANON%|const canon = v => v|' internal/shadow/shadow.go
./scripts/contract.sh >/dev/null 2>&1
expect_fail "contract notices a copy of the wire" $?
cp /tmp/negative-shadow.go internal/shadow/shadow.go

# 3. evidence: a crate that no longer gates must fail a claim.
cp "$crate/cases.jsonl" /tmp/negative-cases.jsonl
printf '{"fn":"jsonPointerTokens","args":["/a/b"],"ok":["wrong"]}\n' >> "$crate/cases.jsonl"
./scripts/evidence.sh >/dev/null 2>&1
expect_fail "evidence notices a case that does not hold" $?
cp /tmp/negative-cases.jsonl "$crate/cases.jsonl"

# 4. the bridge's encode: a wrapper that drops -0 must fail check while the driver still passes. If both
#    fail, the two paths are one path and one of them is decoration.
crate=testdata/markers/go-ports/markers-go
cp "$crate/index.cjs" /tmp/negative-index.cjs
sed -i '' "s|if (Object.is(v, -0)) return { \$number: '-0' }||" "$crate/index.cjs"
if "$GO" gate "$crate" >/dev/null 2>&1; then
  printf 'ok    gate still passes with a broken bridge (it does not use the wrapper)\n'
else
  printf 'FAIL  gate failed too, so it cannot tell the bridge from the driver\n'
  failures=$((failures + 1))
fi
"$GO" check "$crate" >/dev/null 2>&1
expect_fail "check notices a bridge that drops -0" $?
cp /tmp/negative-index.cjs "$crate/index.cjs"

# 5. the driver's mark: answering 0 where the original answered -0 must fail gate.
cp "$crate/cmd/godriver/main.go" /tmp/negative-driver.go
sed -i '' 's|return map\[string\]any{"\$number": "-0"}|return 0.0|' "$crate/cmd/godriver/main.go"
"$GO" gate "$crate" >/dev/null 2>&1
expect_fail "gate notices a driver that loses the sign of zero" $?
cp /tmp/negative-driver.go "$crate/cmd/godriver/main.go"

# 6. the three bridges, each broken on its own: allbridges.mjs must notice every one of them. A bridge
#    nothing tests is a bridge that works until it does not.
crate=testdata/markers/go-ports/markers-go
"$GO" bridge "$crate" >/dev/null 2>&1
"$GO" wasm "$crate" >/dev/null 2>&1
"$GO" build "$crate" >/dev/null 2>&1
"$GO" build --fast "$crate" >/dev/null 2>&1

cp "$crate/index.wasm.mjs" /tmp/negative-wasm.mjs
sed -i '' "s|if (Object.is(v, -0)) return { \$number: '-0' }||" "$crate/index.wasm.mjs"
node scripts/allbridges.mjs "$crate" >/dev/null 2>&1
expect_fail "allbridges notices a wasm wrapper that drops -0" $?
cp /tmp/negative-wasm.mjs "$crate/index.wasm.mjs"

cp "$crate/cmd/portctl/main.go" /tmp/negative-portctl.go
sed -i '' 's|port.Answer(req.Name, req.Args)|port.Answer(req.Name, nil)|' "$crate/cmd/portctl/main.go"
"$GO" build "$crate" >/dev/null 2>&1
node scripts/allbridges.mjs "$crate" >/dev/null 2>&1
expect_fail "allbridges notices a pipe that answers without arguments" $?
cp /tmp/negative-portctl.go "$crate/cmd/portctl/main.go"
"$GO" build "$crate" >/dev/null 2>&1

cp "$crate/answer.go" /tmp/negative-answer.go
sed -i '' 's|return map\[string\]any{"\$number": "-0"}|return 0.0|' "$crate/answer.go"
"$GO" build --fast "$crate" >/dev/null 2>&1
node scripts/allbridges.mjs "$crate" >/dev/null 2>&1
expect_fail "allbridges notices an addon that loses the sign of zero" $?
cp /tmp/negative-answer.go "$crate/answer.go"
# The addon carries the compiled copy of answer.go: rebuilding is part of putting the crate back, and a
# control that forgets it reports its own leftover as a failure of the check.
"$GO" build --fast "$crate" >/dev/null 2>&1
node scripts/allbridges.mjs "$crate" >/dev/null 2>&1 || { printf 'FAIL  allbridges does not recover\n'; failures=$((failures + 1)); }

# 7. shadow: an answer that differs must be recorded and reported, and a port that stopped raising must
#    be recorded too. shadow is the only check that sees live values, so it is the only one that could
#    notice a break in what a caller passes in.
jp=testdata/realworld5/go-ports/jp-go
cp "$jp/port.go" /tmp/negative-jp.go
python3 - "$jp/port.go" <<'PY'
import sys
p = sys.argv[1]
s = open(p).read()
s = s.replace('return strings.ReplaceAll(strings.ReplaceAll(token, "~", "~0"), "/", "~1"), nil',
              '_ = strings.TrimSpace\n\treturn token, nil')
open(p, 'w').write(s)
PY
"$GO" build --fast "$jp" >/dev/null 2>&1
"$GO" shadow "$jp" >/dev/null 2>&1
rm -f "$jp/shadow.jsonl"
node -e 'require(process.argv[1] + "/shadow.cjs").escapeJsonPointerToken("a/b")' "$(pwd)/$jp" >/dev/null 2>&1
"$GO" shadow-report "$jp" >/dev/null 2>&1
expect_fail "shadow-report reports an answer that differs" $?
cp /tmp/negative-jp.go "$jp/port.go"
"$GO" build --fast "$jp" >/dev/null 2>&1
rm -f "$jp/shadow.jsonl"

jadx=testdata/realworld4/go-ports/jadx-go
cp "$jadx/port.go" /tmp/negative-jadx.go
python3 - "$jadx/port.go" <<'PY'
import sys
p = sys.argv[1]
s = open(p).read()
s = s.replace("""if signal, ok := args[0].(AbortSignal); ok && signal.Aborted {
		return nil, ErrJadxInterrupted
	}""", """if signal, ok := args[0].(AbortSignal); ok && signal.Aborted && false {
		return nil, ErrJadxInterrupted
	}""")
open(p, 'w').write(s)
PY
"$GO" build --fast "$jadx" >/dev/null 2>&1
"$GO" shadow "$jadx" >/dev/null 2>&1
rm -f "$jadx/shadow.jsonl"
node --experimental-transform-types --no-warnings=ExperimentalWarning -e '
const shr = require(process.argv[1] + "/shadow.cjs")
const stop = () => { const c = new AbortController(); c.abort(new Error("x")); return c.signal }
try { shr.throwIfJadxInterrupted(stop()) } catch {}
' "$(pwd)/$jadx" >/dev/null 2>&1
"$GO" shadow-report "$jadx" >/dev/null 2>&1
expect_fail "shadow-report reports a throw the port stopped making" $?
cp /tmp/negative-jadx.go "$jadx/port.go"
"$GO" build --fast "$jadx" >/dev/null 2>&1
rm -f "$jadx/shadow.jsonl"

# 8. the documents: a command that --help prints and README does not name must be reported. A doc is a
#    claim, and an out-of-date one is the cheapest lie a project can tell.
cp README.md /tmp/negative-readme.md
python3 - README.md <<'PY'
import sys
p = sys.argv[1]
s = open(p).read()
s = s.replace('`shadow-report`', 'shadow-report')
open(p, 'w').write(s)
PY
./scripts/doccheck.sh >/dev/null 2>&1
expect_fail "doccheck notices a command missing from README" $?
cp /tmp/negative-readme.md README.md

# 9. the bridge axis: a generated wrapper that goes through the registry instead of the typed call is
#    the regression that hid for nine rounds. Regenerate the wasm module with a tool whose fast path is
#    dead and the probe must refuse to call it a win.
if [ -e "${RUST_ANYTHING:-/tmp/ra-39/target/release/rust-anything}" ] && [ -e "${RUST_CRATE:-/tmp/racmp2/rust/text-rs}/cases.jsonl" ]; then
  cp internal/wasmbridge/wasmbridge.go /tmp/negative-wasmbridge.go
  python3 - internal/wasmbridge/wasmbridge.go <<'PY'
import sys
p = sys.argv[1]
s = open(p).read()
s = s.replace('\tdetectPortStyle(dir)', '\t// detectPortStyle(dir)')
open(p, 'w').write(s)
PY
  CGO_ENABLED=1 go build -o /tmp/negative-go-deadpath . || { printf 'FAIL  the dead-path build failed\n'; failures=$((failures + 1)); }
  cp /tmp/negative-wasmbridge.go internal/wasmbridge/wasmbridge.go
  /tmp/negative-go-deadpath wasm testdata/sample/port-rs >/dev/null 2>&1
  node scripts/floor/bridge-ab.mjs testdata/sample/port-rs "${RUST_CRATE:-/tmp/racmp2/rust/text-rs}" >/dev/null 2>&1
  expect_fail "the bridge probe refuses a dead fast path" $?
  "$GO" wasm testdata/sample/port-rs >/dev/null 2>&1
  node scripts/floor/bridge-ab.mjs testdata/sample/port-rs "${RUST_CRATE:-/tmp/racmp2/rust/text-rs}" >/dev/null 2>&1 ||
    { printf 'FAIL  the bridge probe does not pass after restoring\n'; failures=$((failures + 1)); }
else
  printf 'skip  the bridge probe needs the rust rig (RUST_ANYTHING, RUST_CRATE)\n'
fi

# 10. the counts in PARITY: a document that states a number the code no longer has. The tool list grew
#     from seven to fifteen and the line said seven for nine rounds.
cp PARITY.md /tmp/negative-parity.md
python3 - PARITY.md <<'PY'
import sys
p = sys.argv[1]
s = open(p).read()
s = s.replace('**15 个工具**', '**7 个工具**')
open(p, 'w').write(s)
PY
./scripts/doccheck.sh >/dev/null 2>&1
expect_fail "doccheck notices a stated count that the code no longer has" $?
cp /tmp/negative-parity.md PARITY.md

# 11. the startup axis only means something when it measures the fast build. A cgo binary in its place adds
#     ~0.15 ms and reads as a regression in the tool; that happened twice, so the guard has a control.
cp bin/golang-anything /tmp/negative-fast-build 2>/dev/null || true
CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o bin/golang-anything .
./scripts/perf.sh >/dev/null 2>&1
expect_fail "perf refuses a cgo build in the fast binary's place" $?
cp /tmp/negative-fast-build bin/golang-anything 2>/dev/null || ./scripts/build.sh bin >/dev/null 2>&1

# 12. the front binary: without its engine it must say so, not answer as if it were the whole tool.
cp bin/golang-anything-engine /tmp/negative-engine
rm -f bin/golang-anything-engine
./bin/golang-anything diff testdata/sample/port-rs >/dev/null 2>&1
expect_fail "the front says it cannot find its engine" $?
cp /tmp/negative-engine bin/golang-anything-engine
./bin/golang-anything diff testdata/sample/port-rs >/dev/null 2>&1 || { printf 'FAIL  the front does not recover\n'; failures=$((failures + 1)); }

# 13. the smoke test: a command that cannot run must be reported. Renaming the engine makes every engine
#     command fail, and the smoke test has to say so rather than pass on the front's two answers.
cp bin/golang-anything-engine /tmp/negative-engine2
mv bin/golang-anything-engine /tmp/negative-engine-moved
./scripts/smoke.sh >/dev/null 2>&1
expect_fail "smoke notices a tool that cannot run its commands" $?
mv /tmp/negative-engine-moved bin/golang-anything-engine
./scripts/smoke.sh >/dev/null 2>&1 || { printf 'FAIL  smoke does not recover\n'; failures=$((failures + 1)); }

# 22. enable in a language with no block must refuse, not write the JavaScript one into a .rs file. The
#     exit code alone would also pass on a crate that is merely unbuilt, so the message has to name the
#     missing takeover and the file has to survive byte for byte.
mkdir -p /tmp/negative-takeover/src
cat > /tmp/negative-takeover/src/text.rs <<'RS'
pub fn range_of(s: &str, sub: &str) -> Option<(usize, usize)> {
    let start = s.find(sub)?;
    Some((start, start + sub.len()))
}
RS
cat > /tmp/negative-takeover/golang-anything.json <<'JSON'
{"unit":"src/text.rs","crate":"neg/takeover","exports":[{"name":"range_of","params":["string"],"returns":""}]}
JSON
# The pre-fix tool reached the write with these present, which is what made the corruption silent: a
# control that stops at "index.cjs is missing" would report a refusal it never earned.
: > /tmp/negative-takeover/index.cjs
: > /tmp/negative-takeover/addon.node
cp /tmp/negative-takeover/src/text.rs /tmp/negative-takeover/original.rs
"$GO" enable /tmp/negative-takeover >/tmp/negative-takeover/enable.err 2>&1
expect_fail "enable refuses a language with no block" $?
grep -q 'no takeover' /tmp/negative-takeover/enable.err ||
  { printf 'FAIL  enable refused for some other reason: %s\n' "$(head -1 /tmp/negative-takeover/enable.err)"; failures=$((failures + 1)); }
cmp -s /tmp/negative-takeover/src/text.rs /tmp/negative-takeover/original.rs ||
  { printf 'FAIL  the refused enable wrote the JavaScript block into the Rust file anyway\n'; failures=$((failures + 1)); }
rm -rf /tmp/negative-takeover

# 24. bench: a call written with literal arguments is a constant, and clang folds it out of the timing loop.
#     The row then measures the loop, and the report says the Go port is 13.46x slower than a function whose
#     body is `return 42` (1 ns vs 3 ns for the same crate). The check is on the harness source, not on
#     nanoseconds: 1 vs 3 is a clock reading and this is the mechanism that produces it.
mkdir -p /tmp/negative-bench && cd /tmp/negative-bench
printf 'int magic(int n) { return 42; }\n' > m.c
printf 'module negbench\n\ngo 1.24\n' > go.mod
"$GO" scaffold --unit m.c go-ports/m-go >/dev/null 2>&1
sed -i '' "s|example.com/m-go|negbench/go-ports/m-go|g" go-ports/m-go/*.go go-ports/m-go/golang-anything.json go-ports/m-go/cmd/*/main.go 2>/dev/null
"$GO" record --unit m.c go-ports/m-go >/dev/null 2>&1
"$GO" bench go-ports/m-go >/dev/null 2>&1
python3 - go-ports/m-go/work/benchh.c <<'PYB'
import re, sys
try:
    src = open(sys.argv[1]).read()
except OSError as e:
    sys.exit('no harness: %s' % e)
calls = re.findall(r'\bmagic\(([^;]*)\)\s*;', src)
if not calls:
    sys.exit('the harness never calls magic: this control would pass on nothing')
bad = [c for c in calls if not c.lstrip().startswith('*(volatile')]
sys.exit(0 if not bad else 'literal argument reaches the call: %s' % bad[0])
PYB
expect_ok "bench keeps literals out of the timing loop" $?
cd "$ROOT"
rm -rf /tmp/negative-bench

# 25. bench must refuse a scaffold stub. The port body scaffold writes is `return nil, fmt.Errorf("not
#     implemented: port …")`, and timing that is timing fmt.Errorf: the same C crate read 17 ns / 8.83x
#     "slower" as a stub and 5 ns / 3.0x slower with one line filled in. A ratio for a function that answers
#     nothing is the report inventing a result.
mkdir -p /tmp/negative-stub && cd /tmp/negative-stub
printf 'int twice(int n) { return n * 2; }\n' > m.c
printf 'module negstub\n\ngo 1.24\n' > go.mod
"$GO" scaffold --unit m.c go-ports/m-go >/dev/null 2>&1
sed -i '' "s|example.com/m-go|negstub/go-ports/m-go|g" go-ports/m-go/*.go go-ports/m-go/golang-anything.json go-ports/m-go/cmd/*/main.go 2>/dev/null
"$GO" record --unit m.c go-ports/m-go >/dev/null 2>&1
"$GO" bench go-ports/m-go >/tmp/negative-stub/bench.err 2>&1
expect_fail "bench refuses a scaffold stub instead of timing fmt.Errorf" $?
grep -q 'not implemented: port' /tmp/negative-stub/bench.err ||
  { printf 'FAIL  bench refused for some other reason: %s\n' "$(head -1 /tmp/negative-stub/bench.err)"; failures=$((failures + 1)); }
cd "$ROOT"
rm -rf /tmp/negative-stub

# 26. check has to say which differences are the scaffold, and the number has to be the real one. Counting
#     from the five mismatches the replay prints reported "3 of them" for a crate with seventeen unported
#     cases — wrong in the direction that looks better — so this control requires a count above five and the
#     function that is missing.
mkdir -p /tmp/negative-unported && cd /tmp/negative-unported
printf 'int twice(int n) { return n * 2; }\nint thrice(int n) { return n * 3; }\n' > m.c
printf 'module negunported\n\ngo 1.24\n' > go.mod
"$GO" scaffold --unit m.c go-ports/m-go >/dev/null 2>&1
sed -i '' "s|example.com/m-go|negunported/go-ports/m-go|g" go-ports/m-go/*.go go-ports/m-go/golang-anything.json go-ports/m-go/cmd/*/main.go 2>/dev/null
"$GO" record --unit m.c go-ports/m-go >/dev/null 2>&1
# one function ported, one left as the scaffold
python3 - go-ports/m-go/port.go <<'PYU'
import sys
p = sys.argv[1]
s = open(p).read()
s = s.replace('return nil, fmt.Errorf("not implemented: port twice")', 'return int(args[0].(float64)) * 2, nil')
open(p, 'w').write(s)
PYU
"$GO" bridge go-ports/m-go >/dev/null 2>&1
"$GO" build --fast go-ports/m-go >/dev/null 2>&1
"$GO" check go-ports/m-go >/tmp/negative-unported/check.out 2>&1
expect_fail "check refuses a crate with an unported function" $?
grep -q 'not implemented: port' /tmp/negative-unported/check.out ||
  { printf 'FAIL  check did not say the port is the scaffold: %s\n' "$(tail -1 /tmp/negative-unported/check.out)"; failures=$((failures + 1)); }
grep -qE '; ([6-9]|[1-9][0-9]+) of them because' /tmp/negative-unported/check.out ||
  { printf 'FAIL  the scaffold count is wrong or capped at five: %s\n' "$(tail -1 /tmp/negative-unported/check.out)"; failures=$((failures + 1)); }
grep -q '(thrice)' /tmp/negative-unported/check.out ||
  { printf 'FAIL  check did not name the missing function: %s\n' "$(tail -1 /tmp/negative-unported/check.out)"; failures=$((failures + 1)); }
cd "$ROOT"
rm -rf /tmp/negative-unported

# 27. promote needs a route into the instance: Ruby reopens the class, PHP gets a proxy class, and a language with
#     neither must be refused rather than given the Ruby block — which is what it did once, into a .php file, and
#     reported as success.
mkdir -p /tmp/negative-promote && cd /tmp/negative-promote
printf 'export const twice = (n: number): number => n * 2\n' > text.ts
printf 'module negpromote\n\ngo 1.24\n' > go.mod
cp text.ts original.ts
"$GO" promote --unit text.ts . >/tmp/negative-promote/promote.err 2>&1
expect_fail "promote refuses a language with no route into the instance" $?
grep -q 'no other language is written yet' /tmp/negative-promote/promote.err ||
  { printf 'FAIL  promote refused for some other reason: %s\n' "$(head -1 /tmp/negative-promote/promote.err)"; failures=$((failures + 1)); }
cmp -s text.ts original.ts ||
  { printf 'FAIL  the refused promote wrote to the unit anyway\n'; failures=$((failures + 1)); }
cd "$ROOT"
rm -rf /tmp/negative-promote

# 30. promote must not offer a method that reads state. A proxy for `return $this->offset + $n` would produce a
#     second instance with a default offset, and every recorded answer would be wrong in a way the gate cannot see.
before=$failures
mkdir -p /tmp/negative-stateful && cd /tmp/negative-stateful
printf '<?php\nclass Timer {\n    private int $offset = 5;\n    public function shifted(int $n): int { return $n + $this->offset; }\n}\n' > timer.php
printf 'module negstateful\n\ngo 1.24\n' > go.mod
"$GO" promote --unit timer.php . >/tmp/negative-stateful/promote.err 2>&1
expect_fail "promote refuses a class whose only method reads state" $?
grep -q 'read state' /tmp/negative-stateful/promote.err ||
  { printf 'FAIL  promote refused for some other reason: %s\n' "$(head -1 /tmp/negative-stateful/promote.err)"; failures=$((failures + 1)); }
[ ! -f promoted.php ] ||
  { printf 'FAIL  a proxy was written for a stateful method\n'; failures=$((failures + 1)); }
[ "$failures" -eq "$before" ] && printf 'ok    promote leaves a stateful method alone\n'
cd "$ROOT"
rm -rf /tmp/negative-stateful

# 31. the PHP proxy has to be typed and has to load its class. Both were missing once: a variadic proxy has no
#     bridgeable export, and a proxy without `require_once` records the class error as the case.
mkdir -p /tmp/negative-proxy && cd /tmp/negative-proxy
printf '<?php\nclass Timer {\n    public function double(int $n): int { return $n * 2; }\n}\n' > timer.php
printf 'module negproxy\n\ngo 1.24\n' > go.mod
"$GO" promote --unit timer.php . >/dev/null 2>&1
before=$failures
grep -q 'public static function double(int $n): int' promoted.php ||
  { printf 'FAIL  the proxy does not declare the method types\n'; failures=$((failures + 1)); }
grep -q "require_once __DIR__ . '/timer.php';" promoted.php ||
  { printf 'FAIL  the proxy does not load the class it instantiates\n'; failures=$((failures + 1)); }
"$GO" analyze . 2>/dev/null | grep -q 'TimerPromoted.double' ||
  { printf 'FAIL  analyze does not read the proxy as an export\n'; failures=$((failures + 1)); }
[ "$failures" -eq "$before" ] && printf 'ok    the PHP proxy is typed, loaded and readable\n'
cd "$ROOT"
rm -rf /tmp/negative-proxy

# 32. the Ruby shim has to reach the instance through allocate, not new: new runs initialize, which takes arguments
#     the shim does not have and which the promoted method does not need. A `new` shim raises ArgumentError on the
#     first call and the recorder files that as the answer.
mkdir -p /tmp/negative-allocate && cd /tmp/negative-allocate
printf 'class Timer\n  def initialize(unit)\n    @unit = unit\n  end\n\n  def double(n)\n    n * 2\n  end\nend\n' > timer.rb
printf 'module negallocate\n\ngo 1.24\n' > go.mod
"$GO" promote --unit timer.rb . >/dev/null 2>&1
before=$failures
grep -q 'allocate.double(\*args)' timer.rb ||
  { printf 'FAIL  the shim does not go through allocate\n'; failures=$((failures + 1)); }
if grep -q 'new.double' timer.rb; then
  printf 'FAIL  the shim calls new, which runs a constructor the method does not need\n'
  failures=$((failures + 1))
fi
[ "$failures" -eq "$before" ] && printf 'ok    the Ruby shim reaches the instance through allocate\n'
cd "$ROOT"
rm -rf /tmp/negative-allocate

# 33. the same rule on a JVM language: a class that holds a field is not offered, and no proxy file appears. Java's
#     proxy instantiates the class without its constructor state, so a recorded answer would be right only where that
#     state did not matter — and `gate` cannot see the difference.
mkdir -p /tmp/negative-jvmstate && cd /tmp/negative-jvmstate
printf 'public final class Needs {\n    private int offset = 5;\n\n    public int doubled(int n) { return n * 2; }\n\n    public static int twice(int n) { return n * 2; }\n}\n' > Needs.java
printf 'module negjvmstate\n\ngo 1.24\n' > go.mod
before=$failures
"$GO" promote --unit Needs.java . >/tmp/negative-jvmstate/promote.err 2>&1
expect_fail "promote refuses a Java class that holds a field" $?
grep -q 'read state' /tmp/negative-jvmstate/promote.err ||
  { printf 'FAIL  promote refused for some other reason: %s\n' "$(head -1 /tmp/negative-jvmstate/promote.err)"; failures=$((failures + 1)); }
[ ! -f NeedsPromoted.java ] ||
  { printf 'FAIL  a proxy file was written for a stateful class\n'; failures=$((failures + 1)); }
# The same rule on the two other proxy languages that can hold state in a constructor.
# A body property, not a constructor parameter: the constructor rule would refuse a parameter anyway, and a control
# that cannot fail is decoration.
printf 'class Needs {\n    private val offset = 5\n\n    fun doubled(n: Int): Int = n * 2\n}\n' > Needs.kt
"$GO" promote --unit Needs.kt . >/tmp/negative-jvmstate/kt.err 2>&1
expect_fail "promote refuses a Kotlin class that holds a property" $?
[ ! -f NeedsPromoted.kt ] || { printf 'FAIL  a Kotlin proxy was written for a stateful class\n'; failures=$((failures + 1)); }
printf 'public class Needs {\n    private int offset = 5;\n\n    public int doubled(int n) { return n * 2; }\n}\n' > Needs.cs
"$GO" promote --unit Needs.cs . >/tmp/negative-jvmstate/cs.err 2>&1
expect_fail "promote refuses a C# class that holds a field" $?
[ ! -f NeedsPromoted.cs ] || { printf 'FAIL  a C# proxy was written for a stateful class\n'; failures=$((failures + 1)); }
[ "$failures" -eq "$before" ] && printf 'ok    promote leaves a stateful class alone in Java, Kotlin and C#\n'
cd "$ROOT"
rm -rf /tmp/negative-jvmstate

# 14. perf: no rig, no numbers.
RUST_ANYTHING=/nonexistent ./scripts/perf.sh >/dev/null 2>&1
expect_fail "perf refuses a missing rig" $?

# 19. the state rule: a static method that touches class variables must be refused. Carbon's static
#     factories are this shape, and before this rule they were offered as portable.
mkdir -p /tmp/negative-state
cat > /tmp/negative-state/counter.php <<'PHP'
<?php
class Counter {
    private static int $count = 0;
    public static function bump(int $n): int { self::$count += $n; return self::$count; }
}
PHP
"$GO" analyze --json /tmp/negative-state 2>/dev/null |
  python3 -c 'import json,sys; d=json.load(sys.stdin); sys.exit(0 if d["units"][0]["exports"][0].get("blocked") else 1)'
expect_ok "analyze refuses a method that touches class state" $?
rm -rf /tmp/negative-state

# 23. the other half of the state rule: `$this` is the token that will decide whether an instance method can
#     ever be promoted (ROADMAP one), and it is not `self::`. A detector that only knows class variables
#     passes every test that mentions `self::` and still lets `$this->offset` through the day instance
#     methods are read. The pure method is in the same file so a rule that blocks everything fails too.
mkdir -p /tmp/negative-state-this
cat > /tmp/negative-state-this/clock.php <<'PHP'
<?php
class Clock {
    private int $offset = 0;
    public static function shifted(int $now): int { return $now + $this->offset; }
    public static function plain(int $n): int { return $n * 2; }
}
PHP
"$GO" analyze --json /tmp/negative-state-this 2>/dev/null |
  python3 -c 'import json,sys
d = json.load(sys.stdin)
blocked = {e["name"]: bool(e.get("blocked")) for e in d["units"][0]["exports"]}
sys.exit(0 if blocked.get("shifted") and not blocked.get("plain") else 1)'
expect_ok "analyze refuses a method that reaches for \$this, and keeps the pure one" $?
rm -rf /tmp/negative-state-this

# 21. disable must put the file back exactly. A takeover that leaves one changed byte is a takeover nobody
#     can undo, so a hand-edited block is refused rather than half-removed. Three claims, because the exit
#     code alone would also accept "crate not found": the message names the edit, the file survives untouched,
#     and an intact block still comes out byte for byte.
mkdir -p /tmp/negative-rb && cd /tmp/negative-rb && rm -rf crate && mkdir crate
cat > crate/util.rb <<'RB'
module Util
  def self.double(n)
    n * 2
  end
end
RB
cd /tmp/negative-rb/crate
printf 'module negrb\n\ngo 1.24\n' > go.mod
"$GO" scaffold --unit util.rb go-ports/u-go >/dev/null 2>&1
sed -i '' "s|example.com/u-go|negrb/go-ports/u-go|g" go-ports/u-go/*.go go-ports/u-go/golang-anything.json go-ports/u-go/cmd/*/main.go 2>/dev/null
"$GO" record --unit util.rb go-ports/u-go >/dev/null 2>&1
"$GO" bridge go-ports/u-go >/dev/null 2>&1
"$GO" build go-ports/u-go >/dev/null 2>&1
cp util.rb /tmp/negative-rb/original.rb
"$GO" enable go-ports/u-go >/dev/null 2>&1
# A silent enable failure would make the rest of this control test nothing: say so here.
grep -q '^# >>> golang-anything' util.rb || { printf 'FAIL  enable did not write a block\n'; failures=$((failures + 1)); }
cp util.rb /tmp/negative-rb/on.rb
# delete the closing marker: disable must refuse, not swallow the rest of the file
sed '/^# <<< golang-anything/d' util.rb > /tmp/negative-rb/edited.rb
cp /tmp/negative-rb/edited.rb util.rb
"$GO" disable go-ports/u-go >/tmp/negative-rb/disable.err 2>&1
expect_fail "disable refuses a hand-edited block" $?
grep -q 'edited by hand' /tmp/negative-rb/disable.err ||
  { printf 'FAIL  disable refused for some other reason: %s\n' "$(head -1 /tmp/negative-rb/disable.err)"; failures=$((failures + 1)); }
cmp -s util.rb /tmp/negative-rb/edited.rb ||
  { printf 'FAIL  the refused disable half-removed the file anyway\n'; failures=$((failures + 1)); }
# The undo path itself still has to work, and byte for byte: an intact block goes back to the original.
cp /tmp/negative-rb/on.rb util.rb
"$GO" disable go-ports/u-go >/dev/null 2>&1
cmp -s util.rb /tmp/negative-rb/original.rb ||
  { printf 'FAIL  disable of an intact block did not restore the original byte for byte\n'; failures=$((failures + 1)); }
cd "$ROOT"
rm -rf /tmp/negative-rb

# 15. npm install: a loader that answers a hard-coded wrong value, built from a patched template.
cp internal/pkg/pkg.go /tmp/negative-pkg.go
python3 - internal/pkg/pkg.go <<'PY2'
import sys
p = sys.argv[1]
s = open(p).read()
s = s.replace("""let port
try {
  port = require(PACKAGES[key] || PACKAGES[process.platform + '-' + process.arch])
} catch (e) {
  throw new Error('golang-anything: no port package for ' + key + '; the release workflow builds ' +
    Object.keys(PACKAGES).join(', '))
}

module.exports = port""", """const port = { jsonPointerTokens: () => [], escapeJsonPointerToken: () => "" }
module.exports = port""")
open(p, 'w').write(s)
PY2
CGO_ENABLED=1 go build -o /tmp/negative-go-badloader .
cp /tmp/negative-pkg.go internal/pkg/pkg.go
GO=/tmp/negative-go-badloader ./scripts/npm-install-check.sh >/dev/null 2>&1
expect_fail "npm-install-check notices a loader that answers wrongly" $?

echo
if [ "$failures" -eq 0 ]; then
  echo "every check in this folder fails when it should"
else
  echo "$failures check(s) cannot fail"
fi
exit $((failures > 0))
