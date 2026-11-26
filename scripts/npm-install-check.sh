#!/usr/bin/env bash
# Install the package the way a consumer does: npm, a registry, optionalDependencies resolved for this
# platform, then require it and call it. A hand-made node_modules never exercised any of that.
#
#   ./scripts/npm-install-check.sh <crate>
set -uo pipefail

# The toolchain this machine actually has: see scripts/env.sh.
. "$(dirname "$0")/env.sh"

CRATE=${1:-testdata/realworld5/go-ports/jp-go}
NAME=${NAME:-jp}
VERSION=${VERSION:-1.0.0}
PORT=${PORT:-$((40000 + RANDOM % 20000))}
GO=${GO:-./bin/golang-anything}

work=$(mktemp -d)
trap 'pkill -f "registry.mjs --work $work" 2>/dev/null; rm -rf "$work"' EXIT

echo "== package =="
"$GO" package --name "$NAME" --version "$VERSION" "$CRATE" >/dev/null || exit 1

platform=$(node -e 'const p=process.platform,a=process.arch;
  console.log(p+"-"+a+(p==="linux"?"-gnu":"")+(p==="win32"?"-msvc":""))')
echo "platform: $platform"

# The platform package is what the loader requires; pack.mjs assembles it from a built artifact.
mkdir -p "$work/dist"
for f in addon.node index.cjs libport.dylib libport.so libport.dll; do
  [ -f "$CRATE/$f" ] && cp "$CRATE/$f" "$work/dist/"
done
(cd "$CRATE/package" && node pack.mjs "$platform" "$work/dist" >/dev/null) || exit 1

echo "== tarballs =="
(cd "$CRATE/package" && npm pack --silent . >/dev/null) || exit 1
(cd "$CRATE/package/$NAME-$platform" && npm pack --silent . >/dev/null) || exit 1
# npm pack writes the tarball into the folder it ran in.
for tarball in "$CRATE/package/$NAME-$VERSION.tgz" "$CRATE/package/$NAME-$platform/$NAME-$platform-$VERSION.tgz"; do
  if [ ! -f "$tarball" ]; then
    echo "FAIL npm pack did not write $tarball"
    exit 1
  fi
  mv "$tarball" "$work/"
done
ls -1 "$work"/*.tgz | sed 's|.*/|  |'

echo "== registry =="
node "$(dirname "$0")/npm-registry.mjs" --work "$work" --port "$PORT" --name "$NAME" --platform-package "$NAME-$platform" --version "$VERSION" &
server=$!
sleep 1

echo "== install =="
mkdir -p "$work/consumer"
printf '{"name":"consumer","version":"1.0.0","private":true}\n' > "$work/consumer/package.json"
(cd "$work/consumer" && npm install "$NAME" --registry "http://127.0.0.1:$PORT" --no-audit --no-fund 2>&1 | tail -2)

echo "== use it =="
failures=0
if ! (cd "$work/consumer" && node -e '
const port = require(process.argv[1])
const answer = port.jsonPointerTokens("/a/b")
const escaped = port.escapeJsonPointerToken("a/b~c")
const ok = JSON.stringify(answer) === "[\"a\",\"b\"]" && escaped === "a~1b~0c"
console.log(ok ? "ok   the installed package answers correctly" : "FAIL " + JSON.stringify({answer, escaped}))
process.exit(ok ? 0 : 1)
' "$NAME"); then
  failures=$((failures + 1))
fi

echo "== the platform package is required, not optional at runtime =="
mkdir -p "$work/plat"
mv "$work/consumer/node_modules/$NAME-$platform" "$work/plat/$NAME-$platform"
missing=$(cd "$work/consumer" && node -e 'require(process.argv[1])' "$NAME" 2>&1 | grep -c 'no port package')
mv "$work/plat/$NAME-$platform" "$work/consumer/node_modules/$NAME-$platform"
if [ "$missing" -gt 0 ]; then
  echo "ok   a missing platform package says so"
else
  echo "FAIL a missing platform package did not say so"
  failures=$((failures + 1))
fi

kill $server 2>/dev/null
wait $server 2>/dev/null
exit $((failures > 0))
