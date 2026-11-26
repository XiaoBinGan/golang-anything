#!/usr/bin/env bash
# What this tool sees in a real project: units, exports, and what it refuses to port.
#
#   ./scripts/survey-real.sh /tmp/real-projects
#
# The directory holds checkouts; each subdirectory is analysed on its own. Nothing is downloaded here — a
# survey over a project you did not choose is not a survey.
set -uo pipefail
. "$(dirname "$0")/env.sh"

ROOT=$(cd "$(dirname "$0")/.." && pwd)
GO=${GO:-$ROOT/bin/golang-anything}
DIR=${1:?usage: survey-real.sh <directory of checkouts>}

printf '%-22s %6s %8s %10s %8s %8s %s\n' project units exports bridgeable blocked files time
for project in "$DIR"/*/; do
  name=$(basename "$project")
  start=$(date +%s%N 2>/dev/null || echo 0)
  json=$("$GO" analyze --json "$project" 2>/dev/null)
  end=$(date +%s%N 2>/dev/null || echo 0)
  [ -z "$json" ] && { printf '%-22s %s\n' "$name" "(analyze failed)"; continue; }
  printf '%s' "$json" | python3 -c "
import json, sys
d = json.load(sys.stdin)
units = d.get('units') or []
exports = sum(len(u['exports']) for u in units)
bridgeable = sum(1 for u in units for e in u['exports'] if not e.get('blocked'))
unsupported = sum((d.get('unsupported') or {}).values())
ms = ($end - $start) / 1e6
print(f\"{'$name':<22} {len(units):>6} {exports:>8} {bridgeable:>10} {exports - bridgeable:>8} {unsupported:>8} {ms:>7.0f}ms\")
"
done
