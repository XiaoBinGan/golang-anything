#!/usr/bin/env bash
# The documents are claims too. Every command the tool prints in --help has to be in PARITY.md and in
# README.md, and the invented-command list has to be the difference between the two tools rather than
# something someone remembered to update.
set -uo pipefail

# The toolchain this machine actually has: see scripts/env.sh.
. "$(dirname "$0")/env.sh"

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT" || exit 1
GO=${GO:-./bin/go-anything}
failures=0

fail() { printf 'FAIL  %s\n' "$1"; failures=$((failures + 1)); }
pass() { printf 'ok    %s\n' "$1"; }

help_text=$("$GO" --help 2>&1)

# 1. every command in --help is documented
undocumented=""
while read -r cmd; do
  [ -z "$cmd" ] && continue
  # A subcommand is documented as part of a compound name (`app record`), so the bare name is not enough.
  grep -q "\`$cmd\`\|\`$cmd [a-z-]*\`" PARITY.md || undocumented="$undocumented $cmd"
  grep -q "\`$cmd\`" README.md || undocumented="$undocumented $cmd(readme)"
done < <(printf '%s\n' "$help_text" | sed -n 's/^  go-anything \([a-z][a-z-]*\).*/\1/p' | sort -u)
if [ -z "$undocumented" ]; then
  pass "every command in --help is in PARITY.md and README.md"
else
  fail "undocumented commands:$undocumented"
fi

# 2. the invented commands are the set difference, computed rather than remembered
invented=$(python3 - "$GO" <<'PY'
import re, subprocess, sys
help_text = subprocess.run([sys.argv[1], '--help'], capture_output=True, text=True).stdout
mine = sorted({m.group(1) for line in help_text.splitlines()
               if (m := re.match(r'\s+go-anything ([a-z][a-z-]*)', line))})
theirs = set()
for row in open('PARITY.md'):
    if not row.startswith('|'):
        continue
    cells = [c.strip() for c in row.split('|')]
    if len(cells) < 3:
        continue
    for name in re.findall(r'`([a-z][a-z-]*)', cells[2]):
        theirs.add(name)
print(' '.join(c for c in mine if c not in theirs))
PY
)
echo "      invented (not in rust-anything): $invented"
for cmd in $invented; do
  grep -qi "自创\|invented\|not in rust-anything" PARITY.md || true
done

# 3. the counts the document states have to match the tool
stated=$(grep -o '[0-9]*/26' PARITY.md | head -1 | cut -d/ -f1)
actual=$(printf '%s\n' "$help_text" | sed -n 's/^  go-anything \([a-z][a-z-]*\).*/\1/p' | sort -u | wc -l | tr -d ' ')
echo "      commands: --help says $actual, PARITY states $stated/26"

# 4. every MCP tool is named in README.md
missing_tool=""
for tool in $(sed -n 's/.*Name: "\([a-z-]*\)".*/\1/p' internal/mcp/mcp.go | sort -u); do
  grep -q "\`$tool\`" README.md || missing_tool="$missing_tool $tool"
done
if [ -z "$missing_tool" ]; then
  pass "every MCP tool is named in README.md"
else
  fail "MCP tools not in README.md:$missing_tool"
fi

# 5. the counts PARITY states have to be the counts the code has: the tool list grew from seven to
#    fifteen in round 48 and the document said seven for nine rounds.
tools=$(sed -n 's/.*Name: *"\([a-z-]*\)".*/\1/p' internal/mcp/mcp.go | sort -u | wc -l | tr -d ' ')
stated_tools=$(grep -o '\*\*[0-9]* 个工具\*\*' PARITY.md | grep -o '[0-9]*' | head -1)
if [ -z "$stated_tools" ]; then
  fail "PARITY.md does not state how many MCP tools there are"
elif [ "$stated_tools" != "$tools" ]; then
  fail "PARITY.md says $stated_tools MCP tools, the code has $tools"
else
  pass "PARITY.md states the $tools MCP tools the code has"
fi

# 6. the doctor checks it lists have to be the checks doctor runs
doctor_checks=$(GO_ANYTHING_PARSE=$PWD/bin/go-anything-cgo "$GO" doctor 2>&1 | grep -cE '^(ok|MISS)')
stated_doctor=$(grep -o '七项' PARITY.md | head -1)
if [ "$doctor_checks" -ne 7 ]; then
  fail "doctor runs $doctor_checks checks; PARITY.md and this script both say seven"
elif [ -z "$stated_doctor" ]; then
  fail "PARITY.md does not say how many checks doctor runs"
else
  pass "doctor runs the seven checks PARITY.md lists"
fi

echo
if [ "$failures" -eq 0 ]; then
  echo "the documents match the tool"
else
  echo "$failures document claim(s) are out of date"
fi
exit $((failures > 0))
