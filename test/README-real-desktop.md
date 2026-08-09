<!-- Copyright (c) the go-widgets/window authors. SPDX-License-Identifier: BSD-3-Clause -->
# Real-desktop proof — go-widgets on the actual wasmbox compositor

`test/probe-wasmbox-real.mjs` proves the `internal/wasmbox` backend end-to-end
against the **real [wasmdesk/wasmbox](https://github.com/wasmdesk/wasmbox) Ruby
desktop** (not the `test/harness.html` stand-in): the pure-Go (CGO=0)
[`rbgo`](https://github.com/go-embedded-ruby/ruby) interpreter running the Ruby
compositor (`compositor/*.rb`, baked into `wasmbox.wasm`), served by wasmbox's
own COOP/COEP `cmd/serve`. This is the same path the Quake-in-wasmbox
integration uses.

**The wasmbox repository is never modified.** It is cloned read-only and built
and run as-is; the go-widgets client is made reachable *same-origin* through a
symlink overlay directory, so `cmd/serve` serves both the unmodified compositor
and the client without a single write into the wasmbox checkout.

## What the probe asserts

1. The **real Ruby compositor boots** (`window.wasmboxReady`, the
   `rbgo compositor: started with N windows` startup line, cross-origin
   isolation active).
2. The compositor runs **off the main thread** (step C) — its OffscreenCanvas
   pixels are reachable only from the compositor Web Worker, via the
   `__wasmboxReadRegion` test hook.
3. Spawning `clients/gowidgets/worker.js` through the documented
   `globalThis.wasmboxSpawnExternal(...)` hook makes the compositor **focus** the
   go-widgets external window; its live rect is read with `__wasmboxFocusedRect`
   (no hardcoded position) and asserted at its deterministic cascade slot.
4. The **VBox+Label+Button rendered** into the composited desktop at that rect
   (near-full coverage + non-zero variance = real structure, not a flat fill).
5. A **real `page.mouse.click`** on the button — a genuine DOM event the page
   relays to the compositor worker, which routes it to the focused window —
   round-trips to a `toolkit.Event`: the counter goes `0→1` and the label
   pixels change. This exercises the **real compositor input routing**.
6. A screenshot of the composited desktop with the client window is saved to
   `test/wasmbox-live-proof-real-desktop-2026-08-09.png`.

## Reproduce

```sh
# 1. Clone wasmbox READ-ONLY and build the real compositor + dev server as-is.
git clone git@github.com:wasmdesk/wasmbox.git /tmp/wasmbox-ro
cd /tmp/wasmbox-ro && GOWORK=off task build:compositor build:serve
#   -> wasmbox.wasm (~80 MB), wasm_exec.js, bin/wasmbox-serve   (all gitignored)

# 2. Build this client (CGO=0 js/wasm).
cd /path/to/go-widgets/window && clients/gowidgets/build.sh

# 3. Same-origin overlay: symlink the unmodified wasmbox tree, plus this
#    client dir at /clients/gowidgets/ — NO writes into the wasmbox checkout.
ROOT=$(mktemp -d)/serveroot; mkdir -p "$ROOT/clients"
for e in /tmp/wasmbox-ro/*; do [ "$(basename "$e")" = clients ] || ln -s "$e" "$ROOT/"; done
for c in /tmp/wasmbox-ro/clients/*; do ln -s "$c" "$ROOT/clients/"; done
ln -s "$PWD/clients/gowidgets" "$ROOT/clients/gowidgets"

# 4. Serve with wasmbox's own COOP/COEP server, and run the probe.
/tmp/wasmbox-ro/bin/wasmbox-serve -addr 127.0.0.1:8098 -dir "$ROOT" &
export CHROMIUM_PATH=/path/to/chromium          # a Playwright-installed build
WASMBOX_BASE_URL=http://127.0.0.1:8098 node test/probe-wasmbox-real.mjs
```

Expected tail:

```
ok   real Ruby compositor booted (rbgo compositor: started with 3 windows)
ok   real compositor focused the go-widgets external window (rect=172,172 260x160)
ok   go-widgets window at the expected cascade slot (172,172) == (172,172)
ok   go-widgets VBox+Label+Button rendered at (172,172) (nonblackPct=100, variance=330)
ok   click round-tripped through the REAL compositor: label pixels changed (counter 0->1) ...
RESULT: PASS
```

The `test/probe-wasmbox.mjs` + `test/harness.html` pair is kept as the
deterministic floor: it runs the identical wire/SAB/input assertions without
building the Ruby compositor, so CI can gate on it anywhere.
