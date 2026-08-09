<!-- Copyright (c) the go-widgets/window authors. SPDX-License-Identifier: BSD-3-Clause -->
# go-widgets wasmbox external client

A self-contained [wasmdesk/wasmbox](https://github.com/wasmdesk/wasmbox)
external client that runs a go-widgets application inside the browser
compositor. It is the **same** program shape as a native go-widgets app
(`window.Open` → `Run`); only the build target differs.

Files:

- `worker.js` — the Web Worker entry a wasmbox compositor spawns
  (`wasmboxSpawnExternal`). It loads Go's `wasm_exec.js`, buffers the
  compositor's port handoff until the Go runtime is up, and runs `gowidgets.wasm`.
  It does **not** import the wasmbox JS SDK: the go-widgets `window` backend
  (`internal/wasmbox`, `//go:build js && wasm`) implements the wasmbox client
  wire protocol itself.
- `build.sh` — compiles `cmd/gowidgetsclient` to `gowidgets.wasm` (CGO-free
  `js/wasm`) and copies `wasm_exec.js` next to `worker.js`. The two build
  artifacts are git-ignored.

The app itself is [`cmd/gowidgetsclient`](../../cmd/gowidgetsclient): a VBox of a
Label + Button whose click increments a counter and `Invalidate`s the label
through `scene.HostRoot`, so the backend commits only the damaged rectangle.

## Build + spawn

```sh
clients/gowidgets/build.sh
# Then, from a wasmbox compositor page (served same-origin):
#   wasmboxSpawnExternal("<origin>/clients/gowidgets/worker.js")
```

## Live browser proof (reproduce)

`test/probe-wasmbox.mjs` drives a headless Chromium against
`test/harness.html` — a protocol-faithful wasmbox compositor stand-in — served
with the COOP/COEP headers `SharedArrayBuffer` needs by wasmbox's **unmodified**
`cmd/serve`. It asserts the go-widgets tree rendered (sampled canvas pixels) and
that an injected click round-tripped to a `toolkit.Event` (counter 0→1), then
saves a screenshot.

```sh
clients/gowidgets/build.sh
# COOP/COEP dev server from a read-only wasmbox checkout:
( cd /path/to/wasmdesk/wasmbox && go build -o /tmp/wbserve ./cmd/serve )
/tmp/wbserve -addr 127.0.0.1:8097 -dir "$PWD" &
# Playwright (uses an already-installed Chromium via $CHROMIUM_PATH):
WASMBOX_BASE_URL=http://127.0.0.1:8097 node test/probe-wasmbox.mjs
```
