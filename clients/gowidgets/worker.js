// go-widgets wasmbox external client (worker entry).
//
// A wasmdesk/wasmbox compositor spawns this script as a dedicated Web Worker
// (wasmboxSpawnExternal). Unlike the wasmbox in-tree clients, this shim does NOT
// import the wasmbox client SDK: the go-widgets `window` backend
// (internal/wasmbox, //go:build js && wasm) implements the wasmbox client wire
// protocol itself — it allocates the surface SharedArrayBuffer, posts `hello`
// over the per-client MessagePort, awaits `welcome`, paints the widget tree into
// the SAB and posts `commit`. So this worker only needs to:
//
//   1. load Go's wasm_exec.js runtime shim,
//   2. buffer any messages the compositor posts before the wasm boots — the
//      port handoff (`{type:"__wasmbox_port", port}`) can beat go.run — and
//      replay them once the Go backend installs its handler via
//      globalThis.__gowidgetsInstall, and
//   3. instantiate + run gowidgets.wasm.
//
// wasm_exec.js and gowidgets.wasm are expected alongside this file (see
// build.sh). SharedArrayBuffer requires the page to be served cross-origin
// isolated (COOP/COEP); wasmbox's cmd/serve does that.

"use strict";

importScripts("./wasm_exec.js");

// Buffer `self` messages until the Go backend installs its handler. The
// compositor's one-shot port handoff is the first message it sends after spawn
// and may arrive before go.run() starts, so it must not be dropped.
let _sink = [];
let _handler = null;
self.onmessage = (e) => {
  if (_handler) _handler(e.data);
  else _sink.push(e.data);
};

// The Go backend (internal/wasmbox.Dial) calls this to take over message
// delivery; we replay whatever was buffered, in order.
globalThis.__gowidgetsInstall = (fn) => {
  _handler = fn;
  const queued = _sink;
  _sink = [];
  for (const m of queued) fn(m);
};

const go = new Go();
WebAssembly.instantiateStreaming(fetch("./gowidgets.wasm"), go.importObject)
  .then((result) => go.run(result.instance))
  .catch((err) => {
    // Surface boot failures so a Playwright probe can assert on them.
    self.wasmboxError = String(err);
    throw err;
  });
