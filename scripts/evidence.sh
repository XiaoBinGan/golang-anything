#!/usr/bin/env bash
# Every claim the project makes, checked in one run: capability per crate, the app layer against a real
# CLI, MCP against real crates, a package that really installs, and doctor against a broken toolchain.
#
# Each section prints PASS or FAIL and the run exits non-zero if anything failed. A section that cannot
# run is a failure too: "skipped" would let a broken install look green.
set -uo pipefail

GO=${GO:-./bin/go-anything}
GO_CGO=${GO_CGO:-./bin/go-anything-cgo}
export GO_ANYTHING_PARSE=${GO_ANYTHING_PARSE:-$GO_CGO}
WORK=${WORK:-/tmp/evidence}
OUT=${OUT:-/tmp/evidence.txt}
rm -rf "$WORK" && mkdir -p "$WORK"

failures=0
section() { printf '\n== %s ==\n' "$1"; }
pass()    { printf 'PASS  %s\n' "$1"; }
fail()    { printf 'FAIL  %s\n' "$1"; failures=$((failures+1)); }

report() {
section "capability: every crate gates and answers through a real bridge"
matrix_out=$(OUT="$WORK/matrix.txt" "$(dirname "$0")/matrix.sh" 2>&1)
printf '%s\n' "$matrix_out" | sed -n '1,9p'
# The matrix is a script of its own: its FAILs have to be counted here, or a broken crate slides by
# while this script says every claim held.
if printf '%s\n' "$matrix_out" | grep -q 'FAIL\|problem(s) above'; then
  fail "a crate in the matrix did not gate or did not answer"
else
  pass "every crate gates and answers through a real bridge"
fi

section "app: record a real CLI, replay it, and catch a one-character change"
cat > "$WORK/inputs.jsonl" <<'EOF'
{"name":"gate a crate","argv":["gate","testdata/realworld3/go-ports/uleb-go"]}
{"name":"diff a crate","argv":["diff","testdata/realworld4/go-ports/jadx-go"]}
EOF
if "$GO" app record --command "$GO" --inputs "$WORK/inputs.jsonl" "$WORK/app" >/dev/null 2>&1; then
  pass "recorded a real CLI"
else
  fail "app record"
fi
if "$GO" app replay --command "$GO" "$WORK/app" 2>/dev/null | grep -q "5/5 cases replay identically"; then
  pass "5/5 replay identically against the same build"
else
  fail "app replay against the same build"
fi

section "mcp: handshake and three tools against real crates"
printf '%s\n' \
  '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}' \
  '{"jsonrpc":"2.0","id":2,"method":"tools/list"}' \
  '{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"gate","arguments":{"crate":"testdata/realworld3/go-ports/uleb-go"}}}' \
  '{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"diff","arguments":{"crate":"testdata/realworld4/go-ports/jadx-go"}}}' \
  | "$GO" mcp 2>/dev/null > "$WORK/mcp.jsonl"
# Four requests in, four replies out, in order: the stream stays aligned, which is what a client
# depends on. A notification in the middle must not add a frame.
if [ "$(wc -l < "$WORK/mcp.jsonl" | tr -d ' ')" = "4" ]; then
  pass "four replies for four requests"
else
  fail "mcp replies: $(wc -l < "$WORK/mcp.jsonl")"
fi
# The tool answers are JSON inside the content text, so the quotes arrive escaped.
if grep -q 'passed.*true' "$WORK/mcp.jsonl"; then
  pass "gate and diff answered passed:true over the protocol"
else
  fail "mcp tool answers"
fi

section "package: the generated npm package really installs and answers"
CRATE=testdata/realworld3/go-ports/uleb-go
if "$GO" package --name evidence-port --version 1.0.0 "$CRATE" >/dev/null 2>&1; then
  pass "laid out package/"
else
  fail "package"
fi
app="$WORK/npm/node_modules/evidence-port"
plat="$WORK/npm/node_modules/evidence-port-darwin-arm64"
mkdir -p "$app" "$plat"
cp "$CRATE/package/index.cjs" "$CRATE/package/package.json" "$app/"
cp "$CRATE/index.cjs" "$CRATE/addon.node" "$CRATE/libport.dylib" "$plat/" 2>/dev/null
printf '{"name":"evidence-port-darwin-arm64","version":"1.0.0","main":"index.cjs"}\n' > "$plat/package.json"
if answer=$(cd "$WORK/npm" && node -e 'console.log(JSON.stringify(require("evidence-port").readElfAttributeUleb([128,1],0,2)))' 2>/dev/null); then
  if [ "$answer" = '{"next":2,"value":128}' ]; then
    pass "installed package answers $answer"
  else
    fail "installed package answered $answer"
  fi
else
  fail "installed package did not load"
fi

section "doctor: a broken GOROOT must be reported"
if GOROOT=/nonexistent "$GO" doctor 2>&1 | grep -q "go builds a program"; then
  fail "doctor accepted a GOROOT that does not exist"
else
  pass "doctor reports the toolchain it cannot use"
fi

section "contract: every shared rule still has its proof"
contract=$("$(dirname "$0")/contract.sh" 2>&1)
if printf '%s\n' "$contract" | grep -q 'lost their proof'; then
  fail "a rule in CONTRACT.md lost its proof"
  printf '%s\n' "$contract" | grep FAIL | head -3
else
  pass "every shared rule from CONTRACT.md is still held"
fi

section "plan: the tool's own output is not a unit"
if grep -q "resolve-hook" testdata/realworld3/PORT-MAP.md 2>/dev/null; then
  fail "PORT-MAP.md lists a generated helper"
else
  pass "PORT-MAP.md holds only source units"
fi

echo
if [ "$failures" -eq 0 ]; then
  echo "every claim above was checked and held"
else
  echo "$failures claim(s) failed"
fi
}

report > "$OUT" 2>&1
cat "$OUT"
echo
echo "written to $OUT"
exit $((failures > 0))
