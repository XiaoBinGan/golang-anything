#!/usr/bin/env bash
# The checks can fail. Every script in this folder that reports a verdict is run once against a known
# break, and must come back non-zero; a check that cannot fail is decoration.
#
# This is slow and it breaks things on purpose, so it is not part of `make check`: it is the reason to
# believe `make check`.
set -uo pipefail

# The toolchain this machine actually has: see scripts/env.sh.
. "$(dirname "$0")/env.sh"

GO=${GO:-./bin/go-anything}
ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT" || exit 1

failures=0
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
cp bin/go-anything /tmp/negative-fast-build 2>/dev/null || true
CGO_ENABLED=1 go build -trimpath -ldflags="-s -w" -o bin/go-anything .
./scripts/perf.sh >/dev/null 2>&1
expect_fail "perf refuses a cgo build in the fast binary's place" $?
cp /tmp/negative-fast-build bin/go-anything 2>/dev/null || ./scripts/build.sh bin >/dev/null 2>&1

# 12. the front binary: without its engine it must say so, not answer as if it were the whole tool.
cp bin/go-anything-engine /tmp/negative-engine
rm -f bin/go-anything-engine
./bin/go-anything diff testdata/sample/port-rs >/dev/null 2>&1
expect_fail "the front says it cannot find its engine" $?
cp /tmp/negative-engine bin/go-anything-engine
./bin/go-anything diff testdata/sample/port-rs >/dev/null 2>&1 || { printf 'FAIL  the front does not recover\n'; failures=$((failures + 1)); }

# 13. the smoke test: a command that cannot run must be reported. Renaming the engine makes every engine
#     command fail, and the smoke test has to say so rather than pass on the front's two answers.
cp bin/go-anything-engine /tmp/negative-engine2
mv bin/go-anything-engine /tmp/negative-engine-moved
./scripts/smoke.sh >/dev/null 2>&1
expect_fail "smoke notices a tool that cannot run its commands" $?
mv /tmp/negative-engine-moved bin/go-anything-engine
./scripts/smoke.sh >/dev/null 2>&1 || { printf 'FAIL  smoke does not recover\n'; failures=$((failures + 1)); }

# 14. perf: no rig, no numbers.
RUST_ANYTHING=/nonexistent ./scripts/perf.sh >/dev/null 2>&1
expect_fail "perf refuses a missing rig" $?

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
  throw new Error('go-anything: no port package for ' + key + '; the release workflow builds ' +
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
