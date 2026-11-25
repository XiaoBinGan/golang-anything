#!/usr/bin/env bash
# Head-to-head evaluation. Needs both tools, both crates, and node.
set -uo pipefail
. "$(dirname "$0")/env.sh"
exec python3 "$(dirname "$0")/eval.py"
