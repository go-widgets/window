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
