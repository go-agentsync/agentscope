#!/usr/bin/env bash
# Build the agentscope wasm bundle into dist/ (index.html, app.wasm, wasm_exec.js,
# boot.js, fsguard.js, style.css), then serve it:
#
#   ./build.sh && agentscope serve --web dist
#
# CGO is disabled (pure Go). Only cmd/agentscope-web/main.go carries the
# js && wasm build tag; the ViewModel and the View (package ui) are native-tested.
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
dist="${1:-$here/dist}"
mkdir -p "$dist"

( cd "$here" && GOOS=js GOARCH=wasm CGO_ENABLED=0 go build -trimpath -o "$dist/app.wasm" ./cmd/agentscope-web )

goroot="$(go env GOROOT)"
if [ -f "$goroot/lib/wasm/wasm_exec.js" ]; then
  cp "$goroot/lib/wasm/wasm_exec.js" "$dist/wasm_exec.js"
elif [ -f "$goroot/misc/wasm/wasm_exec.js" ]; then
  cp "$goroot/misc/wasm/wasm_exec.js" "$dist/wasm_exec.js"
else
  echo "error: wasm_exec.js not found under $goroot" >&2
  exit 1
fi
cp "$here"/web/index.html "$here"/web/boot.js "$here"/web/fsguard.js "$here"/web/style.css "$dist/"
echo "built $dist"
