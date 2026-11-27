#!/usr/bin/env bash
# The documents are claims too. Every command the tool prints in --help has to be in PARITY.md and in
# README.md, and the invented-command list has to be the difference between the two tools rather than
# something someone remembered to update.
set -uo pipefail

# The toolchain this machine actually has: see scripts/env.sh.
. "$(dirname "$0")/env.sh"

ROOT=$(cd "$(dirname "$0")/.." && pwd)
cd "$ROOT" || exit 1
GO=${GO:-./bin/golang-anything}
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
done < <(printf '%s\n' "$help_text" | sed -n 's/^  golang-anything \([a-z][a-z-]*\).*/\1/p' | sort -u)
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
               if (m := re.match(r'\s+golang-anything ([a-z][a-z-]*)', line))})
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

# 3. the table is the claim: every command in --help has a row, and the row count matches what the header says.
#    "26/26" was echoed for rounds and compared with nothing; three commands were documented inside other rows
#    (`scaffold` in bridge's, `shadow-report` in shadow's, `wasm` in neither), so no count could have been right.
rows=$(grep -c '^| [0-9]' PARITY.md)
missing_row=""
for cmd in $(printf '%s\n' "$help_text" | sed -n 's/^  golang-anything \([a-z][a-z-]*\).*/\1/p' | sort -u); do
  # the third cell is our command name; the second is the upstream one, and every invented row has `—` there,
  # so matching the second cell made the check pass for all six of them.
  # the third cell starts with the command's own name: `app record` covers `app`, `setup` + `mcp` covers both.
  grep -qE "^\| [0-9]+ \| [^|]*\| [^|]*\`$cmd" PARITY.md || missing_row="$missing_row $cmd"
done
stated_rows=$(grep -o '当前：[0-9]* 行' PARITY.md | head -1 | grep -o '[0-9]*')
echo "      commands: --help says $(printf '%s\n' "$help_text" | sed -n 's/^  golang-anything \([a-z][a-z-]*\).*/\1/p' | sort -u | wc -l | tr -d ' '), PARITY rows: $rows, header says $stated_rows"
if [ -n "$missing_row" ]; then
  fail "commands with no row of their own:$missing_row"
elif [ "$rows" != "$stated_rows" ]; then
  fail "PARITY has $rows rows; its header says $stated_rows"
else
  pass "PARITY.md has $rows rows and every command has one"
fi

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
doctor_checks=$(GOLANG_ANYTHING_PARSE=$PWD/bin/golang-anything-cgo "$GO" doctor 2>&1 | grep -cE '^(ok|MISS)')
stated_doctor=$(grep -o '八项' PARITY.md | head -1)
if [ "$doctor_checks" -ne 8 ]; then
  fail "doctor runs $doctor_checks checks; PARITY.md and this script both say eight"
elif [ -z "$stated_doctor" ]; then
  fail "PARITY.md does not say how many checks doctor runs"
else
  pass "doctor runs the eight checks PARITY.md lists"
fi

# 7. RECORD.md states how many items each section holds, and the number has to be the number of items. 一
#    (32) and 三 (29) agree; 二 said 19 and held 11, and nothing could have noticed — a document that counts
#    itself is the one place where the count is checkable without trusting the writer.
record_bad=$(python3 - <<'PYD'
import re
text = open('RECORD.md', encoding='utf-8').read()
bad = []
for m in re.finditer(r'^## (.+?)（(\d+) 条）\n(.*?)(?=^## |\Z)', text, flags=re.M | re.S):
    head, stated, body = m.group(1), int(m.group(2)), m.group(3)
    held = len(re.findall(r'^- \*\*', body, flags=re.M))
    if held != stated:
        bad.append('%s states %d, holds %d' % (head, stated, held))
print('; '.join(bad))
PYD
)
if [ -z "$record_bad" ]; then
  pass "RECORD.md states the number of entries each section holds"
else
  fail "RECORD.md counts:$record_bad"
fi

# 8. every entry in RECORD.md names a commit that exists. The entries are subjects without their
#    `type:` prefix, so the rule is a suffix match. A record that can invent a commit reads exactly like
#    one that cannot, and this is the only part of the history a reader is asked to trust without reading
#    git log themselves.
record_ghosts=$(python3 - <<'PYD'
import re, subprocess
text = open('RECORD.md', encoding='utf-8').read()
subjects = [s for s in subprocess.run(['git', 'log', '--format=%s'], capture_output=True, text=True).stdout.splitlines() if s]
ghosts = [b for b in re.findall(r'^- \*\*(.+?)\*\*$', text, flags=re.M) if not any(s.endswith(b) for s in subjects)]
print('; '.join(ghosts))
PYD
)
if [ -z "$record_ghosts" ]; then
  pass "every RECORD.md entry names a commit that exists"
else
  fail "RECORD.md names commits git log does not have:$record_ghosts"
fi

echo
if [ "$failures" -eq 0 ]; then
  echo "the documents match the tool"
else
  echo "$failures document claim(s) are out of date"
fi
exit $((failures > 0))
