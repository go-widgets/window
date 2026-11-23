// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package gtk

import (
	"bytes"
	"testing"

	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/toolkit/scene"
)

// TestFitSize drives the decision Window.fit makes on every frame tick.
func TestFitSize(t *testing.T) {
	for _, tc := range []struct {
		name       string
		aw, ah     int
		scale      float64
		curW, curH int
		w, h       int
		changed    bool
	}{
		{"grow", 400, 300, 1, 200, 160, 400, 300, true},
		{"grow width only", 400, 160, 1, 200, 160, 400, 160, true},
		{"grow height only", 200, 300, 1, 200, 160, 200, 300, true},
		{"shrink", 100, 80, 1, 200, 160, 100, 80, true},
		{"shrink width only", 100, 160, 1, 200, 160, 100, 160, true},
		{"shrink height only", 200, 80, 1, 200, 160, 200, 80, true},
		{"transposed", 160, 200, 1, 200, 160, 160, 200, true},
		{"same", 200, 160, 1, 200, 160, 200, 160, false},
		{"same at scale 2", 100, 80, 2, 200, 160, 200, 160, false},
		{"scale change", 200, 160, 2, 200, 160, 400, 320, true},
		{"unallocated", 0, 0, 1, 200, 160, 200, 160, false},
		{"zero width", 0, 300, 1, 200, 160, 200, 160, false},
		{"zero height", 400, 0, 1, 200, 160, 200, 160, false},
		{"negative", -5, -5, 1, 200, 160, 200, 160, false},
		{"zero scale", 400, 300, 0, 200, 160, 200, 160, false},
		{"tiny scale clamps to 1x1", 3, 3, 0.1, 200, 160, 1, 1, true},
		{"tiny stays 1x1", 3, 3, 0.1, 1, 1, 1, 1, false},
	} {
		w, h, changed := fitSize(tc.aw, tc.ah, tc.scale, tc.curW, tc.curH)
		if w != tc.w || h != tc.h || changed != tc.changed {
			t.Errorf("%s: fitSize(%d,%d,%v,%d,%d) = %d,%d,%v, want %d,%d,%v",
				tc.name, tc.aw, tc.ah, tc.scale, tc.curW, tc.curH, w, h, changed, tc.w, tc.h, tc.changed)
		}
	}
}

// TestPresentAfterFitEqualsFullRepaint drives the decision fit makes, then does
// what realloc does (a new buffer, framer.resize) and checks that the present
// after each kind of resize equals a full repaint at the new size, pixel for
// pixel, that damage then resumes, and that an unchanged size neither
// reallocates nor re-seeds.
func TestPresentAfterFitEqualsFullRepaint(t *testing.T) {
	th := toolkit.DefaultDark()
	for _, tc := range []struct {
		name   string
		aw, ah int
	}{
		{"grow", 2 * gw, 2 * gh},
		{"shrink", gw / 2, gh / 2},
		{"narrower", gw / 2, gh},
		{"taller", gw, 2 * gh},
	} {
		app, cells := grid()
		hr := scene.NewHostRoot(app)
		var f framer
		f.bind(hr)
		m := &mirror{}
		w, h := gw, gh
		buf := make([]byte, w*h*4)
		f.present(hr, th, buf, w, h, m)

		if _, _, changed := fitSize(gw, gh, 1, w, h); changed {
			t.Fatalf("%s: fit reallocated at an unchanged size", tc.name)
		}
		cells[3].on = true
		hr.Invalidate(cells[3])
		if res := f.present(hr, th, buf, w, h, m); res.Whole {
			t.Fatalf("%s: unchanged size presented whole", tc.name)
		}

		nw, nh, changed := fitSize(tc.aw, tc.ah, 1, w, h)
		if !changed || nw != tc.aw || nh != tc.ah {
			t.Fatalf("%s: fitSize = %d,%d,%v", tc.name, nw, nh, changed)
		}
		w, h = nw, nh
		buf = make([]byte, w*h*4)
		f.resize()
		if res := f.present(hr, th, buf, w, h, m); !res.Whole {
			t.Fatalf("%s: first present after the resize: %+v, want whole", tc.name, res)
		}
		if !bytes.Equal(m.shown, refAt(app, th, w, h)) {
			t.Fatalf("%s: present after the resize differs from a full repaint", tc.name)
		}
		cells[40].on = true
		hr.Invalidate(cells[40])
		if res := f.present(hr, th, buf, w, h, m); res.Whole {
			t.Fatalf("%s: damage did not resume: %+v", tc.name, res)
		}
		if !bytes.Equal(m.shown, refAt(app, th, w, h)) {
			t.Fatalf("%s: partial present after the resize differs from a full repaint", tc.name)
		}
	}
}
