#!/usr/bin/env bash
# CONTRACT.md as a check instead of a promise: one row per rule, and the command that proves it still
# holds. A rule whose proof no longer exists is a rule that quietly stopped being true.
set -uo pipefail

GO=${GO:-./bin/go-anything}
ROOT=${ROOT:-.}
failures=0
check() {
  local name="$1"
  shift
  if "$@" >/dev/null 2>&1; then
    printf 'ok    %s\n' "$name"
  else
    printf 'FAIL  %s\n' "$name"
    failures=$((failures + 1))
  fi
}

echo "== one implementation per rule =="
check "the generator pool is the only list of characters" grep -q 'internal/pool' "$ROOT/CONTRACT.md"
check "pool: every structural character is reachable in both generators" \
  go test ./internal/pool/ -run Structural
check "jswire: decode, encode and canon are the only copies" \
  sh -c "! grep -rq 'function decode(v)' --include=*.go $ROOT/internal/bridge && ! grep -rq 'const canon = v =>' --include=*.go $ROOT/internal/shadow"
check "jswire: the helpers behave in both module forms" go test ./internal/jswire/ -run ModuleForms
check "diff: one call key, used by fuzz too" \
  sh -c "! grep -q 'func key(' $ROOT/internal/fuzz/fuzz.go"
check "analyze: a callback, a Promise, a Map and an iterator all stay behind" \
  go test ./internal/analyze/ -run "Promise|Container|Lazy"
# A verdict that cannot come back non-zero is decoration: this one is checked by breaking it, in
# scripts/negative.sh, and the rule here is that the script exists and says it can fail.
check "the checks themselves can fail (make negative)" grep -q 'expect_fail' "$ROOT/scripts/negative.sh"

check "the documents match the tool" ./scripts/doccheck.sh

# The committed answer.go, index.cjs and cmd/portctl are generated: if regenerating them changes the tree,
# the committed copies are stale and a reader is looking at code the tool no longer writes.
stale=""
for crate in $(sed -n '/^CRATES=/,/^"}/p' "$ROOT/scripts/matrix.sh" | grep '^testdata'); do
  # bridge and build --fast both write tracked files; wasm is left out because its output is not
  # reproducible byte for byte, so it cannot be compared with git diff.
  "$GO" bridge "$ROOT/$crate" >/dev/null 2>&1 || stale="$stale $crate(cannot generate)"
  "$GO" build --fast "$ROOT/$crate" >/dev/null 2>&1 || stale="$stale $crate(cannot build)"
  if ! git -C "$ROOT" diff --quiet -- "$crate"; then stale="$stale $crate"; fi
done
if [ -z "$stale" ]; then
  printf 'ok    generated files: regenerating every crate changes nothing\n'
else
  printf 'FAIL  generated files: stale committed copies in%s\n' "$stale"
  failures=$((failures + 1))
fi

echo
echo "== both paths, per crate =="
matrix=$(OUT=/tmp/contract-matrix.txt "$ROOT/scripts/matrix.sh" 2>&1)
if printf '%s\n' "$matrix" | grep -q 'stdio!\|FAIL\|problem(s) above'; then
  printf 'FAIL  matrix: a crate did not gate, did not check, or fell back to the pipe\n'
  printf '%s\n' "$matrix" | grep 'stdio!\|FAIL' | head -3
  failures=$((failures + 1))
else
  printf 'ok    matrix: every crate gates, checks through the addon, and can be timed\n'
fi

echo
if [ "$failures" -eq 0 ]; then
  echo "every rule in CONTRACT.md still has something holding it"
else
  echo "$failures rule(s) lost their proof"
fi
exit $((failures > 0))
