#!/usr/bin/env bash
# Build the go-widgets wasmbox external client: compile cmd/gowidgetsclient to
# gowidgets.wasm (CGO-free js/wasm) and copy Go's wasm_exec.js runtime shim next
# to worker.js. The result — worker.js + wasm_exec.js + gowidgets.wasm — is a
# self-contained client directory a wasmdesk/wasmbox compositor can spawn via
# wasmboxSpawnExternal("<origin>/clients/gowidgets/worker.js").
#
# Copyright (c) the go-widgets/window authors. All rights reserved.
# SPDX-License-Identifier: BSD-3-Clause
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
root="$(cd "$here/../.." && pwd)"

echo "build: compiling cmd/gowidgetsclient -> gowidgets.wasm (js/wasm, CGO=0)"
( cd "$root" && CGO_ENABLED=0 GOOS=js GOARCH=wasm go build -o "$here/gowidgets.wasm" ./cmd/gowidgetsclient )

# wasm_exec.js ships with the Go toolchain (lib/wasm since Go 1.24, misc/wasm
# before). Copy whichever exists.
goroot="$(go env GOROOT)"
if [ -f "$goroot/lib/wasm/wasm_exec.js" ]; then
  cp "$goroot/lib/wasm/wasm_exec.js" "$here/wasm_exec.js"
elif [ -f "$goroot/misc/wasm/wasm_exec.js" ]; then
  cp "$goroot/misc/wasm/wasm_exec.js" "$here/wasm_exec.js"
else
  echo "build: could not find wasm_exec.js under $goroot" >&2
  exit 1
fi

echo "build: wrote $here/gowidgets.wasm ($(wc -c < "$here/gowidgets.wasm") bytes) + wasm_exec.js"
