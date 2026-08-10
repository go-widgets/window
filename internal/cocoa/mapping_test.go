// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package cocoa

import (
	"reflect"
	"testing"

	"github.com/go-widgets/toolkit"
)

func TestDecodeMods(t *testing.T) {
	cases := []struct {
		name        string
		flags       uint64
		shift, ctrl bool
	}{
		{"none", 0, false, false},
		{"shift", modShift, true, false},
		{"control", modControl, false, true},
		{"command", modCommand, false, true},
		{"option-only", modOption, false, false},
		{"shift+cmd", modShift | modCommand, true, true},
		{"shift+ctrl", modShift | modControl, true, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, ct := DecodeMods(c.flags)
			if s != c.shift || ct != c.ctrl {
				t.Fatalf("DecodeMods(%#x) = (%v,%v), want (%v,%v)", c.flags, s, ct, c.shift, c.ctrl)
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
			got := MapKey(c.code, c.chars, false, false, c.press)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("MapKey = %+v, want %+v", got, c.want)
			}
		})
	}
}

func TestMapKeyModifiersFlow(t *testing.T) {
	got := MapKey(0, "c", true, true, true)
	want := []toolkit.Event{
		{Kind: toolkit.EventKeyDown, Code: "c", Shift: true, Ctrl: true},
		{Kind: toolkit.EventChar, Code: "c", Shift: true, Ctrl: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MapKey with mods = %+v, want %+v", got, want)
	}
}

func TestMapMouse(t *testing.T) {
	if got := MapMouseDown(3, 4, true, false); got != (toolkit.Event{Kind: toolkit.EventClick, X: 3, Y: 4, Shift: true}) {
		t.Fatalf("MapMouseDown = %+v", got)
	}
	if got := MapMouseUp(5, 6, false, true); got != (toolkit.Event{Kind: toolkit.EventMouseUp, X: 5, Y: 6, Ctrl: true}) {
		t.Fatalf("MapMouseUp = %+v", got)
	}
	if got := MapMouseMove(7, 8, false, false, false); got != (toolkit.Event{Kind: toolkit.EventMouseMove, X: 7, Y: 8}) {
		t.Fatalf("MapMouseMove(move) = %+v", got)
	}
	if got := MapMouseMove(7, 8, true, false, false); got != (toolkit.Event{Kind: toolkit.EventMouseDrag, X: 7, Y: 8}) {
		t.Fatalf("MapMouseMove(drag) = %+v", got)
	}
}

func TestMapScrollAndSign(t *testing.T) {
	// AppKit positive scrollingDeltaY (upward swipe) → toolkit Delta -1 (up/back).
	if got := MapScroll(1, 2, 3.0, false, false); got.Delta != -1 {
		t.Fatalf("MapScroll(+dy).Delta = %d, want -1", got.Delta)
	}
	if got := MapScroll(1, 2, -3.0, false, false); got.Delta != 1 {
		t.Fatalf("MapScroll(-dy).Delta = %d, want 1", got.Delta)
	}
	if got := MapScroll(1, 2, 0.0, false, false); got.Delta != 0 {
		t.Fatalf("MapScroll(0).Delta = %d, want 0", got.Delta)
	}
	if got := MapScroll(9, 10, -1, true, true); got.X != 9 || got.Y != 10 || !got.Shift || !got.Ctrl {
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
		v          float64
		fl, ce     float64
	}{
		{2.0, 2, 2},   // exact integer: neither adjusts
		{2.5, 2, 3},   // positive fractional
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
