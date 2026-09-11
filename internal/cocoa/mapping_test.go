// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package cocoa

import (
	"reflect"
	"testing"

	"github.com/go-widgets/toolkit"
)

func TestDefaultContentSize(t *testing.T) {
	cases := []struct {
		name       string
		visW, visH float64
		wantW      int
		wantH      int
	}{
		// Unknown screen (either axis ≤ 0) → fixed fallback.
		{"unknown-both", 0, 0, defaultFallbackW, defaultFallbackH},
		{"unknown-w", 0, 900, defaultFallbackW, defaultFallbackH},
		{"unknown-h", 1440, 0, defaultFallbackW, defaultFallbackH},
		{"negative", -10, -10, defaultFallbackW, defaultFallbackH},
		// Typical laptop visible frame: 0.85 fraction lands inside the band.
		// 1512*0.85=1285.2→1285 ; 945*0.85=803.25→803.
		{"laptop-in-band", 1512, 945, 1285, 803},
		// Huge display: fraction exceeds the max on each axis → clamped to max.
		{"huge-clamp-max", 6000, 4000, maxContentW, maxContentH},
		// Small-ish display: fraction falls below the min but the screen still
		// has room → clamped up to the min band.
		{"small-clamp-min", 1080, 700, minContentW, minContentH},
		// Tiny display: min band exceeds the visible extent → capped at the
		// visible frame so the window never overflows the screen.
		{"tiny-cap-avail", 800, 500, 800, 500},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w, h := DefaultContentSize(c.visW, c.visH)
			if w != c.wantW || h != c.wantH {
				t.Fatalf("DefaultContentSize(%v,%v) = (%d,%d), want (%d,%d)",
					c.visW, c.visH, w, h, c.wantW, c.wantH)
			}
			// Post-conditions: a defaulted size is positive and, when the screen
			// is known, never exceeds the visible frame.
			if w <= 0 || h <= 0 {
				t.Fatalf("non-positive default size (%d,%d)", w, h)
			}
			if c.visW > 0 && c.visH > 0 && (float64(w) > c.visW || float64(h) > c.visH) {
				t.Fatalf("default size (%d,%d) exceeds visible frame (%v,%v)", w, h, c.visW, c.visH)
			}
		})
	}
}

func TestDecodeMods(t *testing.T) {
	cases := []struct {
		name  string
		flags uint64
		want  Mods
	}{
		{"none", 0, Mods{}},
		{"shift", modShift, Mods{Shift: true}},
		{"control", modControl, Mods{Ctrl: true}},
		// ⌘ sets BOTH Ctrl (platform-neutral fold) and Meta (the real ⌘).
		{"command", modCommand, Mods{Ctrl: true, Meta: true}},
		{"option-only", modOption, Mods{Alt: true}},
		{"shift+cmd", modShift | modCommand, Mods{Shift: true, Ctrl: true, Meta: true}},
		{"shift+ctrl", modShift | modControl, Mods{Shift: true, Ctrl: true}},
		// ⌘⌥ (paste-as-move accelerator): Ctrl+Meta+Alt, no Shift.
		{"cmd+option", modCommand | modOption, Mods{Ctrl: true, Alt: true, Meta: true}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := DecodeMods(c.flags); got != c.want {
				t.Fatalf("DecodeMods(%#x) = %+v, want %+v", c.flags, got, c.want)
			}
		})
	}
}

func TestDecodeKeyNamed(t *testing.T) {
	cases := map[uint16]string{
		keyReturn:      "Enter",
		keyKeypadEnter: "Enter",
		keyTab:         "Tab",
		keyDelete:      "Backspace",
		keyForwardDel:  "Delete",
		keyEscape:      "Escape",
		keyHome:        "Home",
		keyEnd:         "End",
		keyPageUp:      "PageUp",
		keyPageDown:    "PageDown",
		keyLeft:        "ArrowLeft",
		keyRight:       "ArrowRight",
		keyDownArrow:   "ArrowDown",
		keyUpArrow:     "ArrowUp",
	}
	for code, want := range cases {
		// chars carries a private-use arrow glyph, which MUST be ignored in
		// favour of the keyCode-derived name.
		name, r := DecodeKey(code, "")
		if name != want || r != 0 {
			t.Fatalf("DecodeKey(%d) = (%q,%q), want (%q,0)", code, name, r, want)
		}
	}
}

func TestDecodeKeyPrintableAndUnmapped(t *testing.T) {
	cases := []struct {
		name    string
		code    uint16
		chars   string
		outName string
		outRune rune
	}{
		{"letter a", 0, "a", "", 'a'},
		{"symbol", 0, "$", "", '$'},
		{"unicode", 0, "é", "", 'é'},
		{"control byte", 0, "\x01", "", 0},
		{"del", 0, "\x7f", "", 0},
		{"private-use (function key)", 0, "", "", 0},
		{"empty", 0, "", "", 0},
		{"multi-rune", 0, "ab", "", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			name, r := DecodeKey(c.code, c.chars)
			if name != c.outName || r != c.outRune {
				t.Fatalf("DecodeKey(%d,%q) = (%q,%q), want (%q,%q)", c.code, c.chars, name, r, c.outName, c.outRune)
			}
		})
	}
}

func TestIsPrintable(t *testing.T) {
	cases := []struct {
		r  rune
		ok bool
	}{
		{'a', true},
		{' ', true},
		{0x1f, false},
		{0x7f, false},
		{0xF700, false},
		{0xF8FF, false},
		{0xF6FF, true},
		{0xF900, true},
	}
	for _, c := range cases {
		if got := isPrintable(c.r); got != c.ok {
			t.Fatalf("isPrintable(%#x) = %v, want %v", c.r, got, c.ok)
		}
	}
}

func TestMapKey(t *testing.T) {
	cases := []struct {
		name  string
		code  uint16
		chars string
		press bool
		want  []toolkit.Event
	}{
		{"named down", keyReturn, "", true, []toolkit.Event{{Kind: toolkit.EventKeyDown, Code: "Enter"}}},
		{"named up", keyReturn, "", false, []toolkit.Event{{Kind: toolkit.EventKeyUp, Code: "Enter"}}},
		{"printable down", 0, "a", true, []toolkit.Event{
			{Kind: toolkit.EventKeyDown, Code: "a"},
			{Kind: toolkit.EventChar, Code: "a"},
		}},
		{"printable up", 0, "a", false, []toolkit.Event{{Kind: toolkit.EventKeyUp, Code: "a"}}},
		{"nothing", 0, "\x01", true, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := MapKey(c.code, c.chars, Mods{}, c.press)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("MapKey = %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestMapKeyModifiersFlow(t *testing.T) {
	// A ⌘⌥C chord: Ctrl (⌘ fold) + Meta (⌘) + Alt (⌥), on BOTH the KeyDown and
	// the Char, so a shell can read the accelerator off either.
	got := MapKey(0, "c", Mods{Ctrl: true, Alt: true, Meta: true}, true)
	want := []toolkit.Event{
		{Kind: toolkit.EventKeyDown, Code: "c", Ctrl: true, Alt: true, Meta: true},
		{Kind: toolkit.EventChar, Code: "c", Ctrl: true, Alt: true, Meta: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MapKey with mods = %+v, want %+v", got, want)
	}
}

func TestMapMouse(t *testing.T) {
	if got := MapMouseDown(3, 4, Mods{Shift: true}); got != (toolkit.Event{Kind: toolkit.EventClick, X: 3, Y: 4, Shift: true}) {
		t.Fatalf("MapMouseDown = %+v", got)
	}
	if got := MapMouseUp(5, 6, Mods{Ctrl: true}); got != (toolkit.Event{Kind: toolkit.EventMouseUp, X: 5, Y: 6, Ctrl: true}) {
		t.Fatalf("MapMouseUp = %+v", got)
	}
	if got := MapMouseMove(7, 8, false, Mods{}); got != (toolkit.Event{Kind: toolkit.EventMouseMove, X: 7, Y: 8}) {
		t.Fatalf("MapMouseMove(move) = %+v", got)
	}
	if got := MapMouseMove(7, 8, true, Mods{}); got != (toolkit.Event{Kind: toolkit.EventMouseDrag, X: 7, Y: 8}) {
		t.Fatalf("MapMouseMove(drag) = %+v", got)
	}
	if got := MapSecondaryClick(9, 10, Mods{Ctrl: true}); got != (toolkit.Event{Kind: toolkit.EventSecondaryClick, X: 9, Y: 10, Ctrl: true}) {
		t.Fatalf("MapSecondaryClick = %+v", got)
	}
}

func TestMapScrollAndSign(t *testing.T) {
	// AppKit positive scrollingDeltaY (upward swipe) → toolkit Delta -1 (up/back).
	if got := MapScroll(1, 2, 3.0, Mods{}); got.Delta != -1 {
		t.Fatalf("MapScroll(+dy).Delta = %d, want -1", got.Delta)
	}
	if got := MapScroll(1, 2, -3.0, Mods{}); got.Delta != 1 {
		t.Fatalf("MapScroll(-dy).Delta = %d, want 1", got.Delta)
	}
	if got := MapScroll(1, 2, 0.0, Mods{}); got.Delta != 0 {
		t.Fatalf("MapScroll(0).Delta = %d, want 0", got.Delta)
	}
	if got := MapScroll(9, 10, -1, Mods{Shift: true, Ctrl: true, Alt: true, Meta: true}); got.X != 9 || got.Y != 10 || !got.Shift || !got.Ctrl || !got.Alt || !got.Meta {
		t.Fatalf("MapScroll coords/mods = %+v", got)
	}
	for v, want := range map[float64]int{-2.5: -1, 2.5: 1, 0: 0} {
		if got := signf(v); got != want {
			t.Fatalf("signf(%v) = %d, want %d", v, got, want)
		}
	}
}

func TestViewCoords(t *testing.T) {
	// scale 1: a point at (10, 30) in a 100-tall view → top-left (10, 70).
	if x, y := ViewCoords(10, 30, 100, 1); x != 10 || y != 70 {
		t.Fatalf("ViewCoords scale1 = (%d,%d), want (10,70)", x, y)
	}
	// scale 2 (Retina): device pixels are doubled.
	if x, y := ViewCoords(10, 30, 100, 2); x != 20 || y != 140 {
		t.Fatalf("ViewCoords scale2 = (%d,%d), want (20,140)", x, y)
	}
}

func TestDirtyRect(t *testing.T) {
	// scale 1, whole-point rect: identity.
	if x, y, w, h := DirtyRect(toolkit.Rect{X: 4, Y: 8, W: 16, H: 32}, 1); x != 4 || y != 8 || w != 16 || h != 32 {
		t.Fatalf("DirtyRect scale1 = (%v,%v,%v,%v)", x, y, w, h)
	}
	// scale 2, odd extents: origin floors, far edge ceils to whole points.
	if x, y, w, h := DirtyRect(toolkit.Rect{X: 3, Y: 5, W: 3, H: 3}, 2); x != 1 || y != 2 || w != 2 || h != 2 {
		// x0=1.5→floor1, y0=2.5→floor2, x1=3→ceil3, y1=4→ceil4 → w=2,h=2
		t.Fatalf("DirtyRect scale2 = (%v,%v,%v,%v), want (1,2,2,2)", x, y, w, h)
	}
	// scale <= 0 is coerced to 1.
	if x, _, _, _ := DirtyRect(toolkit.Rect{X: 2, Y: 0, W: 2, H: 2}, 0); x != 2 {
		t.Fatalf("DirtyRect scale0 x = %v, want 2", x)
	}
	// negative origin exercises floor's v<0 branch.
	if x, y, _, _ := DirtyRect(toolkit.Rect{X: -1, Y: -3, W: 2, H: 6}, 2); x != -1 || y != -2 {
		// x0=-0.5→floor-1, y0=-1.5→floor-2
		t.Fatalf("DirtyRect negative = (%v,%v), want (-1,-2)", x, y)
	}
}

func TestFloorCeil(t *testing.T) {
	cases := []struct {
		v      float64
		fl, ce float64
	}{
		{2.0, 2, 2},    // exact integer: neither adjusts
		{2.5, 2, 3},    // positive fractional
		{-2.5, -3, -2}, // negative fractional
		{0, 0, 0},
	}
	for _, c := range cases {
		if got := floor(c.v); got != c.fl {
			t.Fatalf("floor(%v) = %v, want %v", c.v, got, c.fl)
		}
		if got := ceil(c.v); got != c.ce {
			t.Fatalf("ceil(%v) = %v, want %v", c.v, got, c.ce)
		}
	}
}

func TestUnitToByte(t *testing.T) {
	for _, tc := range []struct {
		in   float64
		want uint8
	}{
		{0, 0}, {1, 255}, {0.5, 128}, {0.25, 64},
		{-0.1, 0},       // below the range: clamp, not wrap
		{1.000001, 255}, // a hair above, as a colour-space conversion can land
		{0.999, 255},    // rounds up rather than truncating to 254
		{0.002, 1},      // and a value just off zero is not lost
	} {
		if got := unitToByte(tc.in); got != tc.want {
			t.Errorf("unitToByte(%v) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

func TestDrawBands(t *testing.T) {
	cases := []struct {
		name string
		in   []toolkit.Rect
		bufH int
		want []Band
	}{{
		name: "nothing to draw",
		in:   nil, bufH: 100, want: nil,
	}, {
		name: "a buffer with no rows",
		in:   []toolkit.Rect{{Y: 0, H: 10}}, bufH: 0, want: nil,
	}, {
		// The case the whole change exists for: a line of text changed, and the
		// conversion should cover those rows rather than the window.
		name: "one small rectangle",
		in:   []toolkit.Rect{{X: 40, Y: 120, W: 200, H: 14}}, bufH: 1000,
		want: []Band{{Y: 120, H: 14}},
	}, {
		name: "two apart stay apart",
		in:   []toolkit.Rect{{Y: 10, H: 5}, {Y: 100, H: 5}}, bufH: 1000,
		want: []Band{{Y: 10, H: 5}, {Y: 100, H: 5}},
	}, {
		name: "out of order",
		in:   []toolkit.Rect{{Y: 100, H: 5}, {Y: 10, H: 5}}, bufH: 1000,
		want: []Band{{Y: 10, H: 5}, {Y: 100, H: 5}},
	}, {
		name: "overlapping merge",
		in:   []toolkit.Rect{{Y: 10, H: 20}, {Y: 20, H: 20}}, bufH: 1000,
		want: []Band{{Y: 10, H: 30}},
	}, {
		// Edge to edge is one run: converting the seam twice costs more than
		// the row it saves.
		name: "touching merge",
		in:   []toolkit.Rect{{Y: 10, H: 10}, {Y: 20, H: 10}}, bufH: 1000,
		want: []Band{{Y: 10, H: 20}},
	}, {
		name: "one swallowed by another",
		in:   []toolkit.Rect{{Y: 10, H: 100}, {Y: 20, H: 5}}, bufH: 1000,
		want: []Band{{Y: 10, H: 100}},
	}, {
		name: "clamped to the buffer",
		in:   []toolkit.Rect{{Y: -20, H: 30}, {Y: 990, H: 40}}, bufH: 1000,
		want: []Band{{Y: 0, H: 10}, {Y: 990, H: 10}},
	}, {
		name: "wholly outside contributes nothing",
		in:   []toolkit.Rect{{Y: -50, H: 10}, {Y: 2000, H: 10}, {Y: 5, H: 0}}, bufH: 1000,
		want: nil,
	}}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := DrawBands(c.in, c.bufH)
			if len(got) != len(c.want) {
				t.Fatalf("DrawBands = %v, want %v", got, c.want)
			}
			for i := range got {
				if got[i] != c.want[i] {
					t.Fatalf("DrawBands = %v, want %v", got, c.want)
				}
			}
		})
	}
}

// Whatever the rectangles, every row one of them names must end up inside a
// band: a row left out is a row the screen keeps stale, because the
// framebuffer persists between frames.
func TestDrawBandsCoversEveryRowItWasGiven(t *testing.T) {
	const bufH = 200
	rects := []toolkit.Rect{
		{Y: 190, H: 30}, {Y: 3, H: 1}, {Y: 3, H: 40}, {Y: -5, H: 7}, {Y: 120, H: 0},
	}
	bands := DrawBands(rects, bufH)
	for _, r := range rects {
		for y := max(r.Y, 0); y < min(r.Y+r.H, bufH); y++ {
			in := false
			for _, b := range bands {
				if y >= b.Y && y < b.Y+b.H {
					in = true
					break
				}
			}
			if !in {
				t.Fatalf("row %d was asked for and lies outside every band %v", y, bands)
			}
		}
	}
}

func TestBandDest(t *testing.T) {
	// A retina window: 1600 buffer rows over 800 points, so two pixels a point.
	// The band at rows 200..214 lands at points 100..107.
	if x, y, w, h := BandDest(Band{Y: 200, H: 14}, 1600, 0, 0, 400, 800); x != 0 || y != 100 || w != 400 || h != 7 {
		t.Fatalf("BandDest retina = (%v,%v,%v,%v), want (0,100,400,7)", x, y, w, h)
	}
	// Scale 1: rows are points.
	if _, y, _, h := BandDest(Band{Y: 30, H: 10}, 600, 0, 0, 400, 600); y != 30 || h != 10 {
		t.Fatalf("BandDest scale1 = (y%v,h%v), want (y30,h10)", y, h)
	}
	// The bounds origin is carried, not assumed to be zero.
	if x, y, _, _ := BandDest(Band{Y: 0, H: 10}, 600, 12, 34, 400, 600); x != 12 || y != 34 {
		t.Fatalf("BandDest origin = (%v,%v), want (12,34)", x, y)
	}
	// The whole buffer as one band covers the whole bounds, which is what the
	// AppKit-initiated draw falls back to: it must be pixel-identical to the
	// draw this replaces.
	if x, y, w, h := BandDest(Band{Y: 0, H: 1600}, 1600, 0, 0, 400, 800); x != 0 || y != 0 || w != 400 || h != 800 {
		t.Fatalf("BandDest whole = (%v,%v,%v,%v), want the full bounds", x, y, w, h)
	}
	// Nothing to scale by: no height, so nothing is drawn.
	if _, _, _, h := BandDest(Band{Y: 0, H: 10}, 0, 0, 0, 400, 800); h != 0 {
		t.Fatalf("BandDest with no rows h = %v, want 0", h)
	}
	if _, _, _, h := BandDest(Band{Y: 0, H: 10}, 600, 0, 0, 400, 0); h != 0 {
		t.Fatalf("BandDest with no bounds h = %v, want 0", h)
	}
}

// The bands of one frame must tile the bounds exactly as the whole-buffer draw
// would: a gap between two adjacent bands is a line of stale pixels across the
// window.
func TestBandDestsOfAdjacentBandsMeetExactly(t *testing.T) {
	const bufH, oh = 1600, 800.0
	a := Band{Y: 100, H: 20}
	b := Band{Y: 120, H: 30}
	_, ay, _, ah := BandDest(a, bufH, 0, 0, 400, oh)
	_, by, _, _ := BandDest(b, bufH, 0, 0, 400, oh)
	if ay+ah != by {
		t.Fatalf("band ends at %v and the next starts at %v; the seam is stale", ay+ah, by)
	}
}
