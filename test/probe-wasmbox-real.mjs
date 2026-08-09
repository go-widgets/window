// Live browser proof for the go-widgets `window` wasmbox client backend,
// running against the REAL wasmdesk/wasmbox Ruby desktop compositor.
//
// Horodate: 2026-08-09 21:54 CEST
//
// Unlike test/probe-wasmbox.mjs (which drives test/harness.html, a
// protocol-faithful compositor stand-in — kept as the deterministic floor),
// this probe drives the ACTUAL wasmbox desktop: the pure-Go (CGO=0) rbgo
// interpreter running the Ruby compositor (compositor/*.rb) baked into
// wasmbox.wasm, served by wasmbox's own COOP/COEP cmd/serve. The go-widgets
// client is spawned as a genuine EXTERNAL client through the documented
// `globalThis.wasmboxSpawnExternal("clients/gowidgets/worker.js")` hook — a
// separate Web Worker + wasm instance talking to the compositor over the
// step-C.1 MessagePort + SharedArrayBuffer surface. The wasmbox repository is
// UNMODIFIED; the client files are served same-origin via a symlink overlay
// (see test/README-real-desktop.md).
//
// Strategy:
//   1. Boot the real compositor (window.wasmboxReady === true + the Ruby
//      "rbgo compositor: started with N windows" startup line).
//   2. Snapshot the composited desktop at the go-widgets window's deterministic
//      cascade slot BEFORE spawn (compositor's own OffscreenCanvas pixels, via
//      the __wasmboxReadRegion test hook — a page screenshot cannot see a
//      worker-owned OffscreenCanvas).
//   3. Spawn the go-widgets client and wait until that region CHANGES and shows
//      real structure (variance from the VBox+Label+Button), i.e. the widget
//      tree painted into the SAB and the compositor blitted it onto the desktop.
//   4. Inject a REAL click on the button with page.mouse.click — a genuine DOM
//      event that index.html relays to the compositor worker, which routes it to
//      the focused external window (the real input path). Assert the label
//      region changes (counter 0->1): the click round-tripped to a toolkit.Event
//      the widget tree acted on, THROUGH the real compositor.
//   5. Save a screenshot of the real composited desktop with the client window.
//
// Exit non-zero on any failed assertion.

import { chromium } from "playwright";
import { writeFileSync } from "node:fs";

const base = (process.env.WASMBOX_BASE_URL || "http://127.0.0.1:8098").replace(/\/+$/, "");
const out = process.env.WASMBOX_SHOT || "test/wasmbox-live-proof-real-desktop-2026-08-09.png";
const executablePath = process.env.CHROMIUM_PATH || undefined;
const BOOT_TIMEOUT_MS = 45000;

// The go-widgets client requests a 260x160 surface. We do NOT hardcode where
// the compositor cascade-places it (boot spawns xterm/editor/"about rbgo"
// in-process AND auto-spawns clients/hello + the dock as external clients, so
// the exact slot depends on the build); instead we read the real compositor's
// live geometry for the freshly-spawned, raised-and-focused window via the
// __wasmboxFocusedRect() test hook. At the time of writing that resolves to the
// deterministic cascade slot (172,172) — asserted exactly below.
const REQ_W = 260, REQ_H = 160;
const EXPECT = { x: 172, y: 172 }; // cascade slot 4: 60 + (4 % 6) * 28, both axes
// Button center in surface-local coordinates (VBox: Label top, Button bottom),
// matching the harness probe's (130,120) for the same client window.
const BTN_LOCAL = { x: 130, y: 120 };

let failed = false;
const check = (cond, msg) => {
  if (cond) console.log(`ok   ${msg}`);
  else { console.error(`FAIL ${msg}`); failed = true; }
};

// Find the compositor worker (the Web Worker exposing __wasmboxReadRegion —
// the compositor moved off the main thread in step C, so its OffscreenCanvas
// pixels are only reachable from inside its worker).
async function compositorWorker(page) {
  for (let i = 0; i < 100; i++) {
    for (const w of page.workers()) {
      try {
        if (await w.evaluate(() => typeof globalThis.__wasmboxReadRegion === "function")) return w;
      } catch { /* worker navigating */ }
    }
    await page.waitForTimeout(200);
  }
  return null;
}
const readRegion = (cw, r) => cw.evaluate(({ x, y, w, h }) => globalThis.__wasmboxReadRegion(x, y, w, h), r);
const focusedRect = (cw) => cw.evaluate(() => globalThis.__wasmboxFocusedRect());

const browser = await chromium.launch({ headless: true, executablePath });
try {
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 } });
  const consoleLines = [];
  const pageErrors = [];
  page.on("console", (m) => consoleLines.push(m.text()));
  page.on("pageerror", (e) => pageErrors.push(String(e)));

  await page.goto(`${base}/`, { waitUntil: "load" });

  // 1. Real compositor boots.
  await page.waitForFunction(() => {
    if (globalThis.wasmboxError) throw new Error(String(globalThis.wasmboxError));
    return globalThis.wasmboxReady === true;
  }, { timeout: BOOT_TIMEOUT_MS });
  check(await page.evaluate(() => self.crossOriginIsolated === true),
    "page is cross-origin isolated (COOP/COEP via wasmbox cmd/serve)");
  const startup = consoleLines.find((l) => /rbgo compositor: started with \d+ windows/.test(l));
  check(!!startup, `real Ruby compositor booted (${startup || "no startup line"})`);

  const cw = await compositorWorker(page);
  check(!!cw, "found the compositor Web Worker (real step-C off-main-thread compositor)");
  if (!cw) throw new Error("no compositor worker");

  // 2. Spawn the go-widgets client as a real external client, then locate it
  //    precisely: register_external raises+focuses it, so the compositor's
  //    live focused-window rect resolves to the go-widgets surface once its
  //    first frame is published.
  await page.evaluate(() => globalThis.wasmboxSpawnExternal("clients/gowidgets/worker.js"));

  let fr = null;
  for (let i = 0; i < 200; i++) {
    await page.waitForTimeout(150);
    fr = await focusedRect(cw);
    if (fr && fr.w === REQ_W && fr.h === REQ_H) break;
  }
  check(!!fr && fr.w === REQ_W && fr.h === REQ_H,
    `real compositor focused the go-widgets external window (rect=${fr ? `${fr.x},${fr.y} ${fr.w}x${fr.h}` : "null"})`);
  if (!fr || fr.w !== REQ_W || fr.h !== REQ_H) throw new Error("go-widgets window never became focused");
  // Precise bounds: the deterministic cascade slot.
  check(fr.x === EXPECT.x && fr.y === EXPECT.y,
    `go-widgets window at the expected cascade slot (${fr.x},${fr.y}) == (${EXPECT.x},${EXPECT.y})`);

  const BODY = { x: fr.x, y: fr.y, w: fr.w, h: fr.h };
  const LABEL = { x: fr.x, y: fr.y, w: fr.w, h: 80 };

  // 3. Assert the go-widgets tree actually rendered into the composited desktop
  //    at the located rect: near-full coverage (dark VBox theme) + structure
  //    (Label text + Button — a flat fill would have ~0 variance).
  const body = await readRegion(cw, BODY);
  check(body.nonblackPct > 90 && body.variance > 200,
    `go-widgets VBox+Label+Button rendered at (${BODY.x},${BODY.y}) (nonblackPct=${body.nonblackPct}, variance=${body.variance})`);

  // 4. Read the label strip, then inject a REAL click on the button through the
  //    real compositor input routing (DOM event -> page relay -> compositor
  //    worker -> focused external window -> toolkit.Event).
  const l0 = await readRegion(cw, LABEL);
  await page.mouse.click(BODY.x + BTN_LOCAL.x, BODY.y + BTN_LOCAL.y);

  let l1 = l0;
  for (let i = 0; i < 60; i++) {
    await page.waitForTimeout(200);
    l1 = await readRegion(cw, LABEL);
    if (l1.hash !== l0.hash) break;
  }
  check(l1.hash !== l0.hash,
    "click round-tripped through the REAL compositor: label pixels changed (counter 0->1) — input reached toolkit.Event");

  // 5. Save the real composited desktop screenshot.
  const shot = await page.screenshot({ type: "png" });
  writeFileSync(out, shot);
  console.log(`saved ${out} (${shot.length} bytes)`);

  const relevant = pageErrors.filter((e) => /gowidgets/i.test(e));
  check(relevant.length === 0, `no go-widgets page errors${pageErrors.length ? ` (${pageErrors.length} unrelated ignored)` : ""}`);
} finally {
  await browser.close();
}

console.log(failed ? "\nRESULT: FAIL" : "\nRESULT: PASS");
process.exit(failed ? 1 : 0);
