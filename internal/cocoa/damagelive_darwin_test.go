// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin && integration

// Live pixel proof for incremental present on macOS, the one the X11 and
// Wayland backends already had and this one did not.
//
// It matters more here than there. This backend draws only the CHANGED ROWS —
// each run of rows wrapped as a bitmap of its own, because AppKit converts
// whatever rep it is handed to a CGImage over every row that rep spans — so an
// off-by-one in the row arithmetic, or two bands that fail to meet, leaves a
// stale line across a window that nothing later repairs: the framebuffer
// persists between frames.
//
// The capture is read from the WINDOW SERVER (screencapture -l), not from
// -cacheDisplayInRect:, because that asks the view to draw itself afresh — an
// AppKit-initiated draw, which takes the whole-buffer fallback and would prove
// nothing about the banded path.
//
// ⛔ WHAT THIS DOES NOT CATCH, measured by breaking it on purpose: a band drawn
// at the WRONG DESTINATION passes here. AppKit redraws the window for its own
// reasons between the change and the photograph, and that redraw takes the
// whole-buffer fallback and repairs the evidence. Reading a band from the wrong
// SOURCE rows is caught, because the fallback repaints the same wrong thing.
// The destination arithmetic is held by TestBandDest and its seam case instead,
// which fail on that very ablation. A live proof that heals itself is worth
// saying out loud rather than trusting.
//
// Colours are compared BETWEEN the two captures, never against the bytes that
// were written: the window server colour-manages what it composites, so a
// buffer holding pure red photographs as (234,51,36) here.
package cocoa

import (
	"fmt"
	"image"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// quadRoot paints four quadrants and reports the one that changed. It is the
// smallest thing that is both a widget and a damageRenderer.
type quadRoot struct {
	toolkit.Base
	col     [4]toolkit.RGBA
	dirty   int // -1 for "all of it"
	w, h    int
	painted []toolkit.Rect
}

func (q *quadRoot) quad(i int) toolkit.Rect {
	x, y := (i%2)*(q.w/2), (i/2)*(q.h/2)
	return toolkit.Rect{X: x, Y: y, W: q.w / 2, H: q.h / 2}
}

func (q *quadRoot) Draw(p painter.Painter, _ *toolkit.Theme) {
	for i := range q.col {
		r := q.quad(i)
		p.FillRect(painter.Rect{X: r.X, Y: r.Y, W: r.W, H: r.H}, q.col[i])
	}
}

// RenderDamaged paints and says what it painted, which is what makes this root
// drive the incremental path.
func (q *quadRoot) RenderDamaged(p painter.Painter, th *toolkit.Theme) []toolkit.Rect {
	q.Draw(p, th)
	if q.dirty < 0 {
		q.painted = []toolkit.Rect{{X: 0, Y: 0, W: q.w, H: q.h}}
	} else {
		q.painted = []toolkit.Rect{q.quad(q.dirty)}
	}
	return q.painted
}

// captureWindow photographs one window through the window server, which is the
// only reader that sees what was actually composited.
func captureWindow(t *testing.T, number uint32, name string) image.Image {
	t.Helper()
	if number == 0 {
		t.Fatal("the window has no number, so it cannot be photographed on its own")
	}
	path := filepath.Join(captureDir(t), name)
	cmd := exec.Command("screencapture", "-x", "-o", "-l", fmt.Sprint(number), path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("screencapture refused (%v: %s); grant Screen Recording to run this proof", err, out)
	}
	data, err := os.ReadFile(path)
	if err != nil || len(data) == 0 {
		t.Skipf("screencapture wrote nothing to %s (%v)", path, err)
	}
	img, err := png2img(data)
	if err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return img
}

// sampleAt reads the middle of a quadrant, in the captured image's own pixels.
func sampleAt(img image.Image, i int) (r, g, b uint8) {
	bo := img.Bounds()
	x := bo.Min.X + bo.Dx()/4 + (i%2)*bo.Dx()/2
	y := bo.Min.Y + bo.Dy()/4 + (i/2)*bo.Dy()/2
	cr, cg, cb, _ := img.At(x, y).RGBA()
	return uint8(cr >> 8), uint8(cg >> 8), uint8(cb >> 8)
}

func TestLiveCocoaDamageShowsTheRightPixels(t *testing.T) {
	if os.Getenv("WINDOW_COCOA_INTEGRATION") == "" {
		t.Skip("set WINDOW_COCOA_INTEGRATION=1 to run the live macOS damage proof")
	}
	want := [4]toolkit.RGBA{
		toolkit.RGB(255, 0, 0), toolkit.RGB(0, 255, 0),
		toolkit.RGB(0, 0, 255), toolkit.RGB(255, 255, 255),
	}
	const flipped = 3 // bottom right
	after := toolkit.RGB(20, 20, 20)

	var (
		win    *Window
		root   *quadRoot
		err    error
		number uint32
	)
	callOnMain(func() {
		win, err = New("go-widgets/window damage proof", 240, 200, toolkit.DefaultDark())
		if err != nil {
			return
		}
		root = &quadRoot{col: want, dirty: -1}
		root.SetBounds(toolkit.Rect{X: 0, Y: 0, W: win.w, H: win.h})
		root.w, root.h = win.w, win.h
		win.bindAndSeed(root)
		win.presentFull()
		number = win.Number()
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	t.Cleanup(func() { callOnMain(func() { _ = win.Close() }) })
	callOnMain(func() { spin(0.4) })

	before := captureWindow(t, number, "cocoa-damage-before.png")
	// The four quadrants are compared with EACH OTHER and with themselves after
	// the change, never with the bytes that were written: the window server
	// colour-manages what it composites, so a buffer holding pure red reads back
	// as (234,51,36) on this display. Absolute colours would measure the
	// display profile, not the present path.
	var was [4][3]uint8
	for i := range want {
		r, g, b := sampleAt(before, i)
		was[i] = [3]uint8{r, g, b}
	}
	// The premise: we are photographing the window and not a blank surface.
	for i := range want {
		for j := i + 1; j < len(want); j++ {
			if near(was[i][0], was[j][0]) && near(was[i][1], was[j][1]) && near(was[i][2], was[j][2]) {
				t.Fatalf("quadrants %d and %d photograph alike (%v vs %v); this is not the "+
					"window, so the rest of the test would prove nothing", i, j, was[i], was[j])
			}
		}
	}

	// One quadrant changes, and ONLY its rectangle is presented. This is the
	// banded path: the rows of that quadrant, converted and blitted alone.
	callOnMain(func() {
		root.col[flipped] = after
		root.dirty = flipped
		win.paintFrame(false)
		spin(0.4)
	})

	got := captureWindow(t, number, "cocoa-damage-after.png")
	// The changed quadrant really changed: the damaged band reached the screen.
	r, g, b := sampleAt(got, flipped)
	w := was[flipped]
	if near(r, w[0]) && near(g, w[1]) && near(b, w[2]) {
		t.Errorf("the changed quadrant still reads %v; the damaged band did not reach "+
			"the screen", w)
	}
	// And every other one is untouched. A band drawn at the wrong offset, or a
	// seam between two that fail to meet, shows up here and nowhere else — the
	// framebuffer persists, so a stale strip is stale for good.
	for i := range want {
		if i == flipped {
			continue
		}
		r, g, b := sampleAt(got, i)
		w := was[i]
		if !near(r, w[0]) || !near(g, w[1]) || !near(b, w[2]) {
			t.Errorf("quadrant %d went from %v to (%d,%d,%d); incremental present "+
				"disturbed pixels outside the damage", i, w, r, g, b)
		}
	}
	t.Logf("captures kept in %s", captureDir(t))
}
