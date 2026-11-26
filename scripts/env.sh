#!/usr/bin/env bash
# One place that finds the Go toolchain, because "works in my shell" is not a build.
#
# This machine has two Go installations: a goenv one whose binaries were built by 1.22 and a brew one at
# 1.26. A `go` from PATH that disagrees with GOROOT fails with "compile: version go1.22.5 does not match go
# tool version go1.26.2", which reads like a broken repository rather than a broken environment.
#
# Source this at the top of every script that builds something:
#   . "$(dirname "$0")/env.sh"
set -uo pipefail

# A candidate is usable when its own go command agrees with the GOROOT it sits in.
usable() {
  local root=$1
  [ -x "$root/bin/go" ] || return 1
  local vroot
  vroot=$(GOROOT="$root" "$root/bin/go" env GOROOT 2>/dev/null) || return 1
  [ "$vroot" = "$root" ]
}

pick_go() {
  local candidates=()
  [ -n "${GOROOT:-}" ] && candidates+=("$GOROOT")
  # brew keeps the real installation under libexec and symlinks bin/go to it.
  for root in /opt/homebrew/Cellar/go/*/libexec /usr/local/Cellar/go/*/libexec; do
    [ -d "$root" ] && candidates+=("$root")
  done
  if command -v go >/dev/null 2>&1; then
    local own
    own=$(go env GOROOT 2>/dev/null) && [ -n "$own" ] && candidates+=("$own")
  fi
  for root in "${candidates[@]}"; do
    if usable "$root"; then
      printf '%s\n' "$root"
      return 0
    fi
  done
  return 1
}

if ! root=$(pick_go); then
  echo "golang-anything: no usable Go toolchain." >&2
  echo "  Set GOROOT to the installation your go binary belongs to, for example:" >&2
  echo "    export GOROOT=\$(brew --prefix go)/libexec" >&2
  exit 2
fi

export GOROOT="$root"
case ":$PATH:" in
  *":$GOROOT/bin:"*) ;;
  *) export PATH="$GOROOT/bin:$PATH" ;;
esac
export CGO_ENABLED="${CGO_ENABLED:-1}"

# Almost every check here passes something through node: the wrappers, shadow, the package, the parity
# tools. A missing node made a control pass for the wrong reason once — nothing ran, so nothing differed —
# so it is a hard requirement with a sentence rather than a silent skip.
if [ "${REQUIRE_NODE:-1}" = "1" ] && ! command -v node >/dev/null 2>&1; then
  echo "golang-anything: node is required by the checks in this folder." >&2
  echo "  Put it on PATH, or set REQUIRE_NODE=0 for the ones that do not need it." >&2
  exit 2
fi
