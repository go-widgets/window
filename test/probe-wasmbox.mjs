// Live browser proof for the go-widgets `window` wasmbox client backend.
//
// Horodate: 2026-08-09 21:12 CEST
//
// Strategy (mirrors wasmbox's own probe recipe):
//   1. Serve the repo with wasmbox's (unmodified) cmd/serve, which sets the
//      COOP/COEP headers SharedArrayBuffer needs.
//   2. Load test/harness.html — a protocol-faithful wasmbox compositor stand-in
//      that spawns clients/gowidgets/worker.js as an external client, hands it a
//      MessagePort, answers hello→welcome, and blits each commit's damage out of
//      the client's SharedArrayBuffer onto a real <canvas>.
//   3. Assert the go-widgets tree actually RENDERED: the canvas has non-black
//      pixels (background is black; widgets paint the theme + label + button).
//   4. Inject a click on the button and assert it ROUND-TRIPPED to a
//      toolkit.Event: the client repaints (commit count rises) and the label
//      region's pixels change (counter text 0→1).
//   5. Save a screenshot of the composited canvas.
//
// Exit non-zero on any failed assertion.

import { chromium } from "playwright";
import { writeFileSync } from "node:fs";

const base = process.env.WASMBOX_BASE_URL || "http://127.0.0.1:8097";
const out = process.env.WASMBOX_SHOT || "test/wasmbox-live-proof.png";
const executablePath = process.env.CHROMIUM_PATH || undefined;

let failed = false;
function check(cond, msg) {
  if (cond) console.log(`ok   ${msg}`);
  else { console.error(`FAIL ${msg}`); failed = true; }
}

// Count pixels whose RGB is not the harness background (black), i.e. painted by
// the go-widgets tree.
function nonBlack(rgba) {
  let n = 0;
  for (let i = 0; i < rgba.length; i += 4) {
    if (rgba[i] || rgba[i + 1] || rgba[i + 2]) n++;
  }
  return n;
}
function differs(a, b) {
  if (a.length !== b.length) return true;
  for (let i = 0; i < a.length; i++) if (a[i] !== b[i]) return true;
  return false;
}

const browser = await chromium.launch({ headless: true, executablePath });
try {
  const page = await browser.newPage();
  page.on("console", (m) => console.log(`  [page] ${m.text()}`));
  page.on("pageerror", (e) => console.error(`  [pageerror] ${e}`));

  await page.goto(`${base}/test/harness.html`, { waitUntil: "load" });

  // Cross-origin isolation must be active or the SAB handshake cannot work.
  const isolated = await page.evaluate(() => self.crossOriginIsolated === true);
  check(isolated, "page is cross-origin isolated (COOP/COEP via cmd/serve)");

  // Wait for the client to connect + render its first frame.
  await page.waitForFunction(
    () => window.__state && window.__state.welcomed && window.__state.rendered && window.__state.commits > 0,
    { timeout: 30000 },
  );
  const s0 = await page.evaluate(() => ({ ...window.__state }));
  check(s0.welcomed, "client completed hello→welcome handshake");
  check(s0.commits > 0, `client committed a frame (commits=${s0.commits})`);
  check(s0.title === "go-widgets on wasmbox", `hello carried the app title (${s0.title})`);

  // The go-widgets tree rendered: the canvas is no longer all-black.
  const full0 = await page.evaluate(() => window.sampleRegion(0, 0, 260, 160));
  const painted = nonBlack(full0);
  check(painted > 260 * 160 * 0.2, `go-widgets content painted (${painted} non-background px)`);

  // Sample the label region (top strip) before the click.
  const label0 = await page.evaluate(() => window.sampleRegion(0, 0, 260, 80));

  // Inject a click on the button (bottom half of the VBox) and wait for the
  // resulting repaint — proof the input round-tripped to a toolkit.Event that
  // the widget tree acted on.
  const before = s0.commits;
  await page.evaluate(() => window.injectClick(130, 120));
  await page.waitForFunction(
    (b) => window.__state.commits > b,
    before,
    { timeout: 10000 },
  );
  const s1 = await page.evaluate(() => ({ ...window.__state }));
  check(s1.commits > before, `click produced a new commit (${before}→${s1.commits})`);

  const label1 = await page.evaluate(() => window.sampleRegion(0, 0, 260, 80));
  check(differs(label0, label1), "label pixels changed after click (counter 0→1) — input reached the widget tree");

  const shot = await page.locator("#screen").screenshot();
  writeFileSync(out, shot);
  console.log(`saved ${out}`);
} finally {
  await browser.close();
}

process.exit(failed ? 1 : 0);
