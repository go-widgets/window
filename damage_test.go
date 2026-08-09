// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// Date: 2026-08-09
//
// Proof for the damage-aware present seam: a root that implements DamageRenderer
// (here github.com/go-widgets/toolkit/scene.HostRoot) drives incremental
// present, which must be PIXEL-IDENTICAL to a full-surface repaint while
// touching only a small region. The draw+damage path is backend-agnostic (both
// backends paint the same RGBA framebuffer and blit sub-rectangles of it), so
// the buffer-level identity gate below covers X11 and Wayland alike; the
// Wayland-specific double-buffer packing is proven separately in
// wldamage_test.go, and the live compositors in the CI integration lanes.
package window

import (
	"testing"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/toolkit/scene"
)

// toggleCell is an opaque leaf that flips colour on click and invalidates
// itself through the scene, the canonical "one widget changed" damage source.
type toggleCell struct {
	toolkit.Base
	col, alt toolkit.RGBA
	on       bool
	hr       *scene.HostRoot
}

func (c *toggleCell) Draw(p painter.Painter, _ *toolkit.Theme) {
	col := c.col
	if c.on {
		col = c.alt
	}
	p.FillRect(c.Bounds(), col)
}

func (c *toggleCell) OpaqueRect() (toolkit.Rect, bool) { return c.Bounds(), true }

// OnEvent activates the cell on click (idempotent: repeated clicks keep it on,
// so an injected-click retry loop reaches a deterministic final colour) and
// invalidates it so the scene damages exactly this cell.
func (c *toggleCell) OnEvent(ev toolkit.Event) {
	if ev.Kind == toolkit.EventClick && !c.on {
		c.on = true
		if c.hr != nil {
			c.hr.Invalidate(c)
		}
	}
}

// wholeBox is a non-SelfDrawer container (wholesale fallback) holding children
// at fixed positions.
type wholeBox struct {
	toolkit.Base
	kids []toolkit.Widget
}

func (b *wholeBox) Children() []toolkit.Widget { return b.kids }
func (b *wholeBox) Draw(p painter.Painter, th *toolkit.Theme) {
	for _, k := range b.kids {
		k.Draw(p, th)
	}
}
func (b *wholeBox) OnEvent(ev toolkit.Event) {
	for _, k := range b.kids {
		if k.HitTest(ev.X, ev.Y) {
			k.OnEvent(ev)
		}
	}
}

// buildGridApp builds a wholeBox of rows×cols opaque toggle cells over a WxH
// surface, returning the app root and the flat cell list.
func buildGridApp(W, H, rows, cols int) (*wholeBox, []*toggleCell) {
	cw, ch := W/cols, H/rows
	var cells []*toggleCell
	var kids []toolkit.Widget
	for r := 0; r < rows; r++ {
		for c := 0; c < cols; c++ {
			cell := &toggleCell{
				col: toolkit.RGB(0x20, 0x40, byte(0x60+c*7)),
				alt: toolkit.RGB(0xC0, byte(0x30+r*5), 0x10),
			}
			cell.SetBounds(toolkit.Rect{X: c * cw, Y: r * ch, W: cw - 1, H: ch - 1})
			cells = append(cells, cell)
			kids = append(kids, cell)
		}
	}
	box := &wholeBox{kids: kids}
	box.SetBounds(toolkit.Rect{X: 0, Y: 0, W: W, H: H})
	return box, cells
}

// fullRefBuf renders the damage-UNAWARE reference frame: clear, fill background,
// lay the app out to the full surface and draw it — exactly what the plain
// window path (window.draw) produces.
func fullRefBuf(W, H int, app toolkit.Widget, th *toolkit.Theme) []byte {
	buf := make([]byte, 4*W*H)
	p := painter.NewPixelPainter(buf, W, H)
	full := toolkit.Rect{X: 0, Y: 0, W: W, H: H}
	p.FillRect(full, th.Background)
	app.SetBounds(full)
	app.Draw(p, th)
	return buf
}

func fnv1a(b []byte) uint64 {
	const off = 1469598103934665603
	const prime = 1099511628211
	h := uint64(off)
	for _, c := range b {
		h ^= uint64(c)
		h *= prime
	}
	return h
}

// TestIncrementalPixelIdentityAndRegion is the correctness gate: over a scripted
// change log, the framebuffer updated ONLY by incremental drawIncremental stays
// byte-identical to a fresh full repaint, while each frame's presented damage is
// a small fraction of the surface.
func TestIncrementalPixelIdentityAndRegion(t *testing.T) {
	const W, H, rows, cols = 200, 160, 8, 10
	app, cells := buildGridApp(W, H, rows, cols)
	hr := scene.NewHostRoot(app)
	for _, c := range cells {
		c.hr = hr
	}

	w, _ := dialFake(t, Config{Width: W, Height: H})
	w.root = hr
	w.dmg = hr

	// Initial full frame consumes the seed.
	w.drawIncremental()
	if got, want := fnv1a(w.buf), fnv1a(fullRefBuf(W, H, app, w.theme)); got != want {
		t.Fatalf("initial frame: incremental buffer != full repaint")
	}

	log := []int{0, 37, 5, 79, 44, 12, 63, 20, 0, 51, 8, 37}
	cellArea := (W/cols - 1) * (H/rows - 1)
	for step, idx := range log {
		cells[idx].on = !cells[idx].on
		hr.Invalidate(cells[idx])
		rects := w.drawIncremental()

		var area int
		for _, r := range rects {
			area += r.W * r.H
		}
		// Single opaque cell recolour in place: damage is exactly that cell.
		if area == 0 || area > 2*cellArea {
			t.Fatalf("step %d cell %d: damage area %d px, want ~one cell (%d px)", step, idx, area, cellArea)
		}
		if got, want := fnv1a(w.buf), fnv1a(fullRefBuf(W, H, app, w.theme)); got != want {
			t.Fatalf("step %d cell %d: incremental buffer diverged from full repaint", step, idx)
		}
	}
}

// TestX11RunIncrementalRouting drives the real X11 run loop with a HostRoot over
// a scripted event stream (click → invalidate → incremental present; a resize
// and an Expose → full present), proving Run routes an incremental root through
// paintFrame's incremental, resize and expose branches without error.
func TestX11RunIncrementalRouting(t *testing.T) {
	const W, H = 120, 90
	// A single full-window toggle cell: a click anywhere flips + invalidates it.
	cell := &toggleCell{col: toolkit.RGB(0x10, 0x80, 0x20), alt: toolkit.RGB(0x90, 0x20, 0x20)}
	cell.SetBounds(toolkit.Rect{X: 0, Y: 0, W: W, H: H})
	app := &wholeBox{kids: []toolkit.Widget{cell}}
	app.SetBounds(toolkit.Rect{X: 0, Y: 0, W: W, H: H})
	hr := scene.NewHostRoot(app)
	cell.hr = hr

	w, _ := dialFake(t, Config{Width: W, Height: H},
		buttonPress(1, 40, 30, 0), // click -> flip + invalidate -> incremental
		configureNotify(150, 120), // resize -> full present
		exposeEvent(),             // expose -> re-blit whole surface
		deleteMessage(),
	)
	if err := w.Run(hr); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gw, gh := w.Size(); gw != 150 || gh != 120 {
		t.Fatalf("size after resize = %dx%d, want 150x120", gw, gh)
	}
	// The click must have flipped the cell (proving the event reached it through
	// the HostRoot and drove an incremental frame).
	if !cell.on {
		t.Fatal("click did not reach the cell through the HostRoot")
	}
	// Final framebuffer must equal a full repaint at the resized dimensions.
	if got, want := fnv1a(w.buf), fnv1a(fullRefBuf(150, 120, app, w.theme)); got != want {
		t.Fatal("post-run framebuffer != full repaint at resized size")
	}
}

// TestPlainRootStillFullSurface proves a plain toolkit.Widget root (no
// DamageRenderer) keeps the full-surface path: w.dmg stays nil.
func TestPlainRootStillFullSurface(t *testing.T) {
	w, _ := dialFake(t, Config{Width: 40, Height: 30}, deleteMessage())
	if err := w.Run(&recWidget{}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if w.dmg != nil {
		t.Fatal("plain widget must not be detected as a DamageRenderer")
	}
}

// TestMeasureIncrementalVsFull reports, for a single-cell change on a dense
// scene, the per-frame bytes packed and ns spent by the incremental path versus
// a full-surface repaint — the real-use win the seam unlocks. It also asserts
// the invariants that make the seam worth shipping: incremental never packs more
// bytes, and is never slower, than full.
func TestMeasureIncrementalVsFull(t *testing.T) {
	const W, H, rows, cols = 800, 600, 30, 40
	app, cells := buildGridApp(W, H, rows, cols)
	hr := scene.NewHostRoot(app)
	for _, c := range cells {
		c.hr = hr
	}
	theme := toolkit.DefaultDark()
	full := toolkit.Rect{X: 0, Y: 0, W: W, H: H}

	// Full path: fresh painter, whole-surface fill+draw each op.
	fbuf := make([]byte, 4*W*H)
	fp := painter.NewPixelPainter(fbuf, W, H)
	fullRes := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			fp.FillRect(full, theme.Background)
			app.Draw(fp, theme)
		}
	})

	// Incremental path: warm up, then per op flip one cell + damage-render.
	ibuf := make([]byte, 4*W*H)
	ip := painter.NewPixelPainter(ibuf, W, H)
	hr.SetBounds(full)
	reg := hr.RenderDamaged(ip, theme) // consume the seed
	_ = reg
	target := cells[len(cells)/2]
	var rects []toolkit.Rect
	incRes := testing.Benchmark(func(b *testing.B) {
		for i := 0; i < b.N; i++ {
			target.on = !target.on
			hr.Invalidate(target)
			rects = hr.RenderDamaged(ip, theme)
		}
	})

	var incArea int
	for _, r := range rects {
		incArea += r.W * r.H
	}
	incBytes := incArea * 4
	fullBytes := W * H * 4
	t.Logf("surface %dx%d (%d cells): full=%d ns/frame %d B/frame  incremental=%d ns/frame %d B/frame  speedup=%.1fx  bytes-reduction=%.0fx",
		W, H, rows*cols, fullRes.NsPerOp(), fullBytes, incRes.NsPerOp(), incBytes,
		float64(fullRes.NsPerOp())/float64(incRes.NsPerOp()), float64(fullBytes)/float64(incBytes))

	if incBytes > fullBytes {
		t.Fatalf("incremental packs %d B > full %d B", incBytes, fullBytes)
	}
	if incRes.NsPerOp() > fullRes.NsPerOp() {
		t.Fatalf("incremental slower (%d ns) than full (%d ns)", incRes.NsPerOp(), fullRes.NsPerOp())
	}
}
