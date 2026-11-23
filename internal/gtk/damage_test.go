// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package gtk

import (
	"bytes"
	"testing"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/toolkit/scene"
)

// mirror is a target that models GTK's contract for a partial update: after
// Update it shows the PREVIOUS image with ONLY the reported rectangles taken from
// the new frame (GTK may skip re-rendering everything outside update-region), so
// a damage report that misses a changed pixel leaves a stale pixel here.
//
// shared models the zero-copy target (a GBytes that IS the framebuffer): it
// reports 0 bytes copied. Unshared it models the copying target that preceded it
// (g_bytes_new over the whole frame, w*h*4 bytes on every present).
type mirror struct {
	shown     []byte
	full, upd int
	refuse    bool // Update reports it cannot (GTK older than 4.16)
	shared    bool
	copied    int // total bytes copied over every present
}

func (m *mirror) cost(buf []byte) int {
	if m.shared {
		return 0
	}
	m.copied += len(buf)
	return len(buf)
}

func (m *mirror) Full(buf []byte, w, h int) int {
	m.shown = append(m.shown[:0], buf...)
	m.full++
	return m.cost(buf)
}

func (m *mirror) Update(buf []byte, w, h int, rects []toolkit.Rect) (int, bool) {
	if m.refuse || len(m.shown) != len(buf) {
		return 0, false
	}
	for _, r := range rects {
		for y := r.Y; y < r.Y+r.H; y++ {
			copy(m.shown[(y*w+r.X)*4:(y*w+r.X+r.W)*4], buf[(y*w+r.X)*4:(y*w+r.X+r.W)*4])
		}
	}
	m.upd++
	return m.cost(buf), true
}

type cell struct {
	toolkit.Base
	a, b toolkit.RGBA
	on   bool
}

func (c *cell) Draw(p painter.Painter, _ *toolkit.Theme) {
	col := c.a
	if c.on {
		col = c.b
	}
	p.FillRect(c.Bounds(), col)
}
func (c *cell) OpaqueRect() (toolkit.Rect, bool) { return c.Bounds(), true }

type box struct {
	toolkit.Base
	kids []toolkit.Widget
}

func (b *box) Children() []toolkit.Widget { return b.kids }
func (b *box) Draw(p painter.Painter, th *toolkit.Theme) {
	for _, k := range b.kids {
		k.Draw(p, th)
	}
}

const gw, gh, gRows, gCols = 200, 160, 8, 10

func grid() (*box, []*cell) {
	cw, ch := gw/gCols, gh/gRows
	var cs []*cell
	var ks []toolkit.Widget
	for r := 0; r < gRows; r++ {
		for c := 0; c < gCols; c++ {
			x := &cell{a: toolkit.RGB(0x20, 0x40, byte(0x60+c*7)), b: toolkit.RGB(0xC0, byte(0x30+r*5), 0x10)}
			x.SetBounds(toolkit.Rect{X: c * cw, Y: r * ch, W: cw - 1, H: ch - 1})
			cs = append(cs, x)
			ks = append(ks, x)
		}
	}
	b := &box{kids: ks}
	b.SetBounds(toolkit.Rect{W: gw, H: gh})
	return b, cs
}

// reference renders the damage-unaware full repaint of app.
func reference(app toolkit.Widget, th *toolkit.Theme) []byte {
	buf := make([]byte, gw*gh*4)
	p := painter.NewPixelPainter(buf, gw, gh)
	full := toolkit.Rect{W: gw, H: gh}
	p.FillRect(full, th.Background)
	app.SetBounds(full)
	app.Draw(p, th)
	return buf
}

// TestDamagedPresentEqualsFullRepaint is the gate: over a scripted change log the
// image the target shows after each damaged present is byte-identical to a full
// repaint, the first frame is whole, and each later frame covers only the
// changed cell, never the surface.
func TestDamagedPresentEqualsFullRepaint(t *testing.T) {
	th := toolkit.DefaultDark()
	app, cells := grid()
	hr := scene.NewHostRoot(app)
	var f framer
	f.bind(hr)
	buf := make([]byte, gw*gh*4)
	m := &mirror{}

	res := f.present(hr, th, buf, gw, gh, m)
	if !res.Whole || m.full != 1 {
		t.Fatalf("first frame: whole=%v full=%d, want a whole present", res.Whole, m.full)
	}
	if !bytes.Equal(m.shown, reference(app, th)) {
		t.Fatal("first frame differs from a full repaint")
	}

	cellPixels := (gw/gCols - 1) * (gh/gRows - 1)
	for step, idx := range []int{0, 37, 5, 79, 44, 12, 63, 20, 0, 51, 8, 37} {
		cells[idx].on = !cells[idx].on
		hr.Invalidate(cells[idx])
		res = f.present(hr, th, buf, gw, gh, m)
		if res.Whole || res.Pixels == 0 || res.Pixels > 2*cellPixels {
			t.Fatalf("step %d: whole=%v pixels=%d, want one cell (%d pixels)", step, res.Whole, res.Pixels, cellPixels)
		}
		if !bytes.Equal(m.shown, reference(app, th)) {
			t.Fatalf("step %d cell %d: shown image differs from a full repaint", step, idx)
		}
	}
	if m.full != 1 || m.upd != 12 {
		t.Fatalf("full=%d upd=%d, want 1 and 12", m.full, m.upd)
	}
}

// TestNothingChangedPresentsNothing: an empty damage report is no upload at all.
func TestNothingChangedPresentsNothing(t *testing.T) {
	th := toolkit.DefaultDark()
	app, _ := grid()
	hr := scene.NewHostRoot(app)
	var f framer
	f.bind(hr)
	buf := make([]byte, gw*gh*4)
	m := &mirror{}
	f.present(hr, th, buf, gw, gh, m)
	res := f.present(hr, th, buf, gw, gh, m)
	if res.Whole || res.Pixels != 0 || len(res.Rects) != 0 || m.full != 1 || m.upd != 0 {
		t.Fatalf("idle frame: %+v full=%d upd=%d, want nothing presented", res, m.full, m.upd)
	}
}

// TestRefusedUpdateFallsBackToWhole: below GTK 4.16 the target refuses and the
// frame still reaches the screen, whole and identical to a full repaint.
func TestRefusedUpdateFallsBackToWhole(t *testing.T) {
	th := toolkit.DefaultDark()
	app, cells := grid()
	hr := scene.NewHostRoot(app)
	var f framer
	f.bind(hr)
	buf := make([]byte, gw*gh*4)
	m := &mirror{refuse: true}
	f.present(hr, th, buf, gw, gh, m)
	cells[3].on = true
	hr.Invalidate(cells[3])
	res := f.present(hr, th, buf, gw, gh, m)
	if !res.Whole || m.full != 2 || m.upd != 0 {
		t.Fatalf("refused update: %+v full=%d upd=%d, want a whole fallback", res, m.full, m.upd)
	}
	if !bytes.Equal(m.shown, reference(app, th)) {
		t.Fatal("fallback frame differs from a full repaint")
	}
}

// plainRoot is a widget that is not a damageRenderer.
type plainRoot struct{ cell }

// TestPlainRootIsNotIncremental: a root without RenderDamaged keeps the whole
// path (Window.frame repaints and presents it whole), and a damage renderer
// opts in.
func TestPlainRootIsNotIncremental(t *testing.T) {
	var f framer
	f.bind(&plainRoot{})
	if f.incremental() {
		t.Fatal("a plain widget must not be detected as a damage renderer")
	}
	app, _ := grid()
	f.bind(scene.NewHostRoot(app))
	if !f.incremental() {
		t.Fatal("scene.HostRoot must be detected as a damage renderer")
	}
}

// offRoot reports damage partly or wholly outside the surface.
type offRoot struct {
	cell
	rects []toolkit.Rect
}

func (o *offRoot) RenderDamaged(painter.Painter, *toolkit.Theme) []toolkit.Rect { return o.rects }

// TestDamageIsClampedToTheSurface: rectangles hanging off the frame are cut to it
// and ones fully outside are dropped, so the target never sees a bad rectangle.
func TestDamageIsClampedToTheSurface(t *testing.T) {
	o := &offRoot{}
	var f framer
	f.bind(o)
	buf := make([]byte, gw*gh*4)
	m := &mirror{}
	f.present(o, toolkit.DefaultDark(), buf, gw, gh, m)
	o.rects = []toolkit.Rect{
		{X: -5, Y: -5, W: 15, H: 15},         // top-left corner: 10x10 remains
		{X: gw - 4, Y: gh - 4, W: 20, H: 20}, // bottom-right: 4x4 remains
		{X: gw + 10, Y: 0, W: 5, H: 5},       // off to the right: dropped
		{X: 0, Y: -20, W: 5, H: 10},          // above: dropped
		{X: 3, Y: 3, W: 0, H: 7},             // empty: dropped
	}
	res := f.present(o, toolkit.DefaultDark(), buf, gw, gh, m)
	want := []toolkit.Rect{{X: 0, Y: 0, W: 10, H: 10}, {X: gw - 4, Y: gh - 4, W: 4, H: 4}}
	if len(res.Rects) != len(want) || res.Rects[0] != want[0] || res.Rects[1] != want[1] {
		t.Fatalf("clamped = %v, want %v", res.Rects, want)
	}
	if res.Pixels != 100+16 {
		t.Fatalf("pixels = %d, want %d", res.Pixels, 100+16)
	}
	o.rects = []toolkit.Rect{{X: gw, Y: gh, W: 3, H: 3}}
	if res = f.present(o, toolkit.DefaultDark(), buf, gw, gh, m); res.Whole || len(res.Rects) != 0 || m.upd != 1 {
		t.Fatalf("all-off-surface damage: %+v upd=%d, want nothing presented", res, m.upd)
	}
}

// TestRebindPresentsWholeAgain: a new root has not been shown, so it is whole.
func TestRebindPresentsWholeAgain(t *testing.T) {
	app, _ := grid()
	hr := scene.NewHostRoot(app)
	var f framer
	f.bind(hr)
	buf := make([]byte, gw*gh*4)
	m := &mirror{}
	th := toolkit.DefaultDark()
	f.present(hr, th, buf, gw, gh, m)
	f.bind(hr)
	if res := f.present(hr, th, buf, gw, gh, m); !res.Whole {
		t.Fatalf("after bind: %+v, want whole", res)
	}
}

// TestBytesCopiedPerFrame is the exact count behind the zero-copy present: on the
// 200x160 scene (128000 bytes) over the scripted 12-step change log, the copying
// target moved the whole frame on EVERY present (the first plus the 12 partial
// ones: 13 * 128000 = 1664000 bytes), the shared one moves none, and both show
// the pixels of a full repaint after every step. The copy cannot be narrowed to
// the damaged rectangles instead: GdkMemoryTextureBuilder needs a GBytes that
// spans the whole frame (gdk/gdkmemorytexturebuilder.c:726), which is why the
// target shares the buffer.
func TestBytesCopiedPerFrame(t *testing.T) {
	const frameBytes = gw * gh * 4
	for _, tc := range []struct {
		name      string
		shared    bool
		perUpdate int
		total     int
	}{
		{"copying", false, frameBytes, 13 * frameBytes},
		{"shared", true, 0, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			th := toolkit.DefaultDark()
			app, cells := grid()
			hr := scene.NewHostRoot(app)
			var f framer
			f.bind(hr)
			buf := make([]byte, frameBytes)
			m := &mirror{shared: tc.shared}
			if res := f.present(hr, th, buf, gw, gh, m); res.Copied != tc.perUpdate {
				t.Fatalf("first frame copied %d bytes, want %d", res.Copied, tc.perUpdate)
			}
			for step, idx := range []int{0, 37, 5, 79, 44, 12, 63, 20, 0, 51, 8, 37} {
				cells[idx].on = !cells[idx].on
				hr.Invalidate(cells[idx])
				res := f.present(hr, th, buf, gw, gh, m)
				if res.Whole || res.Copied != tc.perUpdate {
					t.Fatalf("step %d: whole=%v copied=%d, want a partial present copying %d", step, res.Whole, res.Copied, tc.perUpdate)
				}
				if !bytes.Equal(m.shown, reference(app, th)) {
					t.Fatalf("step %d: shown image differs from a full repaint", step)
				}
			}
			if m.copied != tc.total {
				t.Fatalf("copied %d bytes in total, want %d", m.copied, tc.total)
			}
		})
	}
}

// TestRefusedUpdateCopiesNothingWhenShared: the whole-frame fallback of a shared
// target is still a zero-copy SetFrame.
func TestRefusedUpdateCopiesNothingWhenShared(t *testing.T) {
	th := toolkit.DefaultDark()
	app, cells := grid()
	hr := scene.NewHostRoot(app)
	var f framer
	f.bind(hr)
	buf := make([]byte, gw*gh*4)
	m := &mirror{refuse: true, shared: true}
	f.present(hr, th, buf, gw, gh, m)
	cells[3].on = true
	hr.Invalidate(cells[3])
	if res := f.present(hr, th, buf, gw, gh, m); !res.Whole || res.Copied != 0 || m.copied != 0 {
		t.Fatalf("shared fallback: %+v copied=%d, want a whole present copying nothing", res, m.copied)
	}
}
