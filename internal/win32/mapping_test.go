// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package win32

import (
	"reflect"
	"testing"

	"github.com/go-widgets/toolkit"
)

func TestScaleForDpi(t *testing.T) {
	cases := []struct {
		dpi  uint32
		want float64
	}{
		{0, 1}, // unavailable monitor → 1.0
		{96, 1},
		{120, 1.25},
		{144, 1.5},
		{192, 2},
	}
	for _, c := range cases {
		if got := ScaleForDpi(c.dpi); got != c.want {
			t.Fatalf("ScaleForDpi(%d) = %v, want %v", c.dpi, got, c.want)
		}
	}
}

func TestPhysicalFromLogical(t *testing.T) {
	cases := []struct {
		logical int
		scale   float64
		want    int
	}{
		{800, 1, 800},
		{800, 1.5, 1200},
		{100, 2, 200},
		{101, 1.5, 152}, // 151.5 rounds to 152
		{800, 0, 800},   // non-positive scale defaults to 1
		{800, -1, 800},
		{0, 1, 1}, // never below 1
	}
	for _, c := range cases {
		if got := PhysicalFromLogical(c.logical, c.scale); got != c.want {
			t.Fatalf("PhysicalFromLogical(%d,%v) = %d, want %d", c.logical, c.scale, got, c.want)
		}
	}
}

func TestLogicalFromPhysical(t *testing.T) {
	cases := []struct {
		phys  int
		scale float64
		want  int
	}{
		{800, 1, 800},
		{1200, 1.5, 800},
		{200, 2, 100},
		{800, 0, 800}, // non-positive scale defaults to 1
		{800, -2, 800},
		{0, 1, 1}, // never below 1
	}
	for _, c := range cases {
		if got := LogicalFromPhysical(c.phys, c.scale); got != c.want {
			t.Fatalf("LogicalFromPhysical(%d,%v) = %d, want %d", c.phys, c.scale, got, c.want)
		}
	}
}

func TestDefaultContentSize(t *testing.T) {
	cases := []struct {
		name         string
		workW, workH float64
		wantW, wantH int
	}{
		{"unknown-both", 0, 0, defaultFallbackW, defaultFallbackH},
		{"unknown-w", 0, 900, defaultFallbackW, defaultFallbackH},
		{"unknown-h", 1440, 0, defaultFallbackW, defaultFallbackH},
		{"negative", -10, -10, defaultFallbackW, defaultFallbackH},
		// 1512*0.85=1285.2→1285 ; 945*0.85=803.25→803.
		{"laptop-in-band", 1512, 945, 1285, 803},
		{"huge-clamp-max", 6000, 4000, maxContentW, maxContentH},
		{"small-clamp-min", 1080, 700, minContentW, minContentH},
		{"tiny-cap-avail", 800, 500, 800, 500},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			w, h := DefaultContentSize(c.workW, c.workH)
			if w != c.wantW || h != c.wantH {
				t.Fatalf("DefaultContentSize(%v,%v) = (%d,%d), want (%d,%d)",
					c.workW, c.workH, w, h, c.wantW, c.wantH)
			}
			if w <= 0 || h <= 0 {
				t.Fatalf("non-positive default size (%d,%d)", w, h)
			}
			if c.workW > 0 && c.workH > 0 && (float64(w) > c.workW || float64(h) > c.workH) {
				t.Fatalf("default size (%d,%d) exceeds work area (%v,%v)", w, h, c.workW, c.workH)
			}
		})
	}
}

func TestCenterOffset(t *testing.T) {
	cases := []struct {
		avail, win, want int
	}{
		{1000, 800, 100},
		{1920, 1280, 320},
		{800, 800, 0},
		{600, 800, 0}, // window larger than avail → pinned to 0, not negative
	}
	for _, c := range cases {
		if got := CenterOffset(c.avail, c.win); got != c.want {
			t.Fatalf("CenterOffset(%d,%d) = %d, want %d", c.avail, c.win, got, c.want)
		}
	}
}

func TestDecodeMouseMods(t *testing.T) {
	cases := []struct {
		name        string
		wparam      uintptr
		shift, ctrl bool
	}{
		{"none", 0, false, false},
		{"shift", mkShift, true, false},
		{"control", mkControl, false, true},
		{"both", mkShift | mkControl, true, true},
		{"button-only", mkLButton, false, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, ct := DecodeMouseMods(c.wparam)
			if s != c.shift || ct != c.ctrl {
				t.Fatalf("DecodeMouseMods(%#x) = (%v,%v), want (%v,%v)", c.wparam, s, ct, c.shift, c.ctrl)
			}
		})
	}
}

func TestAnyButtonDown(t *testing.T) {
	cases := []struct {
		wparam uintptr
		want   bool
	}{
		{0, false},
		{mkShift, false},
		{mkLButton, true},
		{mkRButton, true},
		{mkMButton, true},
		{mkLButton | mkControl, true},
	}
	for _, c := range cases {
		if got := AnyButtonDown(c.wparam); got != c.want {
			t.Fatalf("AnyButtonDown(%#x) = %v, want %v", c.wparam, got, c.want)
		}
	}
}

func TestDecodeVK(t *testing.T) {
	named := map[uint32]string{
		vkReturn: "Enter",
		vkTab:    "Tab",
		vkBack:   "Backspace",
		vkDelete: "Delete",
		vkEscape: "Escape",
		vkHome:   "Home",
		vkEnd:    "End",
		vkPrior:  "PageUp",
		vkNext:   "PageDown",
		vkLeft:   "ArrowLeft",
		vkRight:  "ArrowRight",
		vkUp:     "ArrowUp",
		vkDown:   "ArrowDown",
	}
	for vk, want := range named {
		if got := DecodeVK(vk); got != want {
			t.Fatalf("DecodeVK(%#x) = %q, want %q", vk, got, want)
		}
	}
	// 'A' (0x41) is a printable key, not a named one.
	if got := DecodeVK(0x41); got != "" {
		t.Fatalf("DecodeVK('A') = %q, want empty", got)
	}
}

func TestMapKeyDown(t *testing.T) {
	// Named key → single EventKeyDown with the name.
	got := MapKeyDown(vkLeft, true, false)
	want := []toolkit.Event{{Kind: toolkit.EventKeyDown, Code: "ArrowLeft", Shift: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MapKeyDown(Left) = %+v, want %+v", got, want)
	}
	// Non-named (printable) key → nothing (WM_CHAR carries it).
	if got := MapKeyDown(0x41, false, true); got != nil {
		t.Fatalf("MapKeyDown('A') = %+v, want nil", got)
	}
}

func TestMapKeyUp(t *testing.T) {
	got := MapKeyUp(vkReturn, false, true)
	want := []toolkit.Event{{Kind: toolkit.EventKeyUp, Code: "Enter", Ctrl: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MapKeyUp(Return) = %+v, want %+v", got, want)
	}
	if got := MapKeyUp(0x41, false, false); got != nil {
		t.Fatalf("MapKeyUp('A') = %+v, want nil", got)
	}
}

func TestMapCharDown(t *testing.T) {
	got := MapCharDown('a', false, false)
	want := []toolkit.Event{
		{Kind: toolkit.EventKeyDown, Code: "a"},
		{Kind: toolkit.EventChar, Code: "a"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MapCharDown('a') = %+v, want %+v", got, want)
	}
	// A shifted printable carries the modifier through.
	got = MapCharDown('A', true, false)
	want = []toolkit.Event{
		{Kind: toolkit.EventKeyDown, Code: "A", Shift: true},
		{Kind: toolkit.EventChar, Code: "A", Shift: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MapCharDown('A',shift) = %+v, want %+v", got, want)
	}
	// Control code (^M etc.) → nothing.
	if got := MapCharDown('\r', false, false); got != nil {
		t.Fatalf("MapCharDown(CR) = %+v, want nil", got)
	}
	if got := MapCharDown(0x7f, false, false); got != nil {
		t.Fatalf("MapCharDown(DEL) = %+v, want nil", got)
	}
}

func TestMapCharUp(t *testing.T) {
	got := MapCharUp('z', false, true)
	want := []toolkit.Event{{Kind: toolkit.EventKeyUp, Code: "z", Ctrl: true}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MapCharUp('z') = %+v, want %+v", got, want)
	}
	if got := MapCharUp('\t', false, false); got != nil {
		t.Fatalf("MapCharUp(TAB) = %+v, want nil", got)
	}
}

func TestMapMouseDownUp(t *testing.T) {
	if got := MapMouseDown(10, 20, true, false); got != (toolkit.Event{Kind: toolkit.EventClick, X: 10, Y: 20, Shift: true}) {
		t.Fatalf("MapMouseDown = %+v", got)
	}
	if got := MapMouseUp(10, 20, false, true); got != (toolkit.Event{Kind: toolkit.EventMouseUp, X: 10, Y: 20, Ctrl: true}) {
		t.Fatalf("MapMouseUp = %+v", got)
	}
}

func TestMapMouseMove(t *testing.T) {
	if got := MapMouseMove(3, 4, false, false, false); got.Kind != toolkit.EventMouseMove {
		t.Fatalf("MapMouseMove no-button = %+v, want EventMouseMove", got)
	}
	if got := MapMouseMove(3, 4, true, false, false); got.Kind != toolkit.EventMouseDrag {
		t.Fatalf("MapMouseMove button-held = %+v, want EventMouseDrag", got)
	}
}

func TestMapWheel(t *testing.T) {
	// Forward (positive) wheel → scroll up (Delta -1).
	if got := MapWheel(1, 2, wheelDelta, false, false); got.Delta != -1 || got.Kind != toolkit.EventScroll {
		t.Fatalf("MapWheel(+) = %+v, want Delta -1", got)
	}
	// Backward (negative) → scroll down (Delta +1).
	if got := MapWheel(1, 2, -wheelDelta, false, false); got.Delta != 1 {
		t.Fatalf("MapWheel(-) = %+v, want Delta 1", got)
	}
	// Zero → Delta 0.
	if got := MapWheel(1, 2, 0, false, false); got.Delta != 0 {
		t.Fatalf("MapWheel(0) = %+v, want Delta 0", got)
	}
}

func TestClientCoords(t *testing.T) {
	cases := []struct {
		px, py       int
		scale        float64
		wantX, wantY int
	}{
		{100, 50, 1, 100, 50},
		{300, 150, 1.5, 200, 100},
		{100, 50, 0, 100, 50}, // non-positive scale defaults to 1
		{-5, -8, 1, 0, 0},     // negative clamped to 0
		{200, 100, 2, 100, 50},
	}
	for _, c := range cases {
		x, y := ClientCoords(c.px, c.py, c.scale)
		if x != c.wantX || y != c.wantY {
			t.Fatalf("ClientCoords(%d,%d,%v) = (%d,%d), want (%d,%d)", c.px, c.py, c.scale, x, y, c.wantX, c.wantY)
		}
	}
}

func TestInvalidRect(t *testing.T) {
	// scale 1: identity.
	x, y, w, h := InvalidRect(toolkit.Rect{X: 10, Y: 20, W: 30, H: 40}, 1)
	if x != 10 || y != 20 || w != 30 || h != 40 {
		t.Fatalf("InvalidRect scale1 = (%d,%d,%d,%d)", x, y, w, h)
	}
	// scale 1.5 with fractional edges: floor origin, ceil far edge.
	// X=10→15, Y=0→0, far X=(10+31)*1.5=61.5→ceil 62, far Y=(0+7)*1.5=10.5→ceil 11.
	x, y, w, h = InvalidRect(toolkit.Rect{X: 10, Y: 0, W: 31, H: 7}, 1.5)
	if x != 15 || y != 0 || w != 62-15 || h != 11 {
		t.Fatalf("InvalidRect scale1.5 = (%d,%d,%d,%d), want (15,0,%d,11)", x, y, w, h, 62-15)
	}
	// non-positive scale defaults to 1.
	x, y, w, h = InvalidRect(toolkit.Rect{X: 1, Y: 2, W: 3, H: 4}, 0)
	if x != 1 || y != 2 || w != 3 || h != 4 {
		t.Fatalf("InvalidRect scale0 = (%d,%d,%d,%d)", x, y, w, h)
	}
	// negative origin clamped to 0.
	x, y, _, _ = InvalidRect(toolkit.Rect{X: -4, Y: -6, W: 2, H: 2}, 1)
	if x != 0 || y != 0 {
		t.Fatalf("InvalidRect negative origin = (%d,%d), want (0,0)", x, y)
	}
}

func TestPackBGRA(t *testing.T) {
	// Two pixels RGBA: (10,20,30,40) and (50,60,70,80).
	src := []byte{10, 20, 30, 40, 50, 60, 70, 80}
	dst := make([]byte, len(src))
	PackBGRA(dst, src)
	want := []byte{30, 20, 10, 40, 70, 60, 50, 80} // R/B swapped, G/A kept
	if !reflect.DeepEqual(dst, want) {
		t.Fatalf("PackBGRA = %v, want %v", dst, want)
	}
	// dst shorter than src → packs only what fits (one pixel).
	dst2 := make([]byte, 4)
	PackBGRA(dst2, src)
	if !reflect.DeepEqual(dst2, []byte{30, 20, 10, 40}) {
		t.Fatalf("PackBGRA short dst = %v", dst2)
	}
	// Tail shorter than a whole pixel is left untouched.
	src3 := []byte{1, 2, 3, 4, 5, 6} // 6 bytes: one whole pixel + 2 tail
	dst3 := make([]byte, 6)
	PackBGRA(dst3, src3)
	if !reflect.DeepEqual(dst3, []byte{3, 2, 1, 4, 0, 0}) {
		t.Fatalf("PackBGRA tail = %v", dst3)
	}
}

func TestPackBGRARect(t *testing.T) {
	// 2x2 RGBA surface, pixels (r,g,b,a) row-major:
	// (0,0)=(1,2,3,4)   (1,0)=(5,6,7,8)
	// (0,1)=(9,10,11,12)(1,1)=(13,14,15,16)
	src := []byte{
		1, 2, 3, 4, 5, 6, 7, 8,
		9, 10, 11, 12, 13, 14, 15, 16,
	}
	// Pack only the bottom-right pixel (1,1).
	dst := make([]byte, len(src))
	PackBGRARect(dst, src, 2, 2, 1, 1, 1, 1)
	// dst pixel (1,1) at byte offset 12: BGRA of (13,14,15,16) = 15,14,13,16.
	if !(dst[12] == 15 && dst[13] == 14 && dst[14] == 13 && dst[15] == 16) {
		t.Fatalf("PackBGRARect single = %v", dst[12:16])
	}
	// The other pixels stay zero (untouched).
	for i := 0; i < 12; i++ {
		if dst[i] != 0 {
			t.Fatalf("PackBGRARect touched byte %d: %d", i, dst[i])
		}
	}

	// Negative origin is clamped: (-1,-1,3,3) packs the whole 2x2 surface.
	dst2 := make([]byte, len(src))
	PackBGRARect(dst2, src, 2, 2, -1, -1, 3, 3)
	wantFull := make([]byte, len(src))
	PackBGRA(wantFull, src)
	if !reflect.DeepEqual(dst2, wantFull) {
		t.Fatalf("PackBGRARect clamp-neg = %v, want %v", dst2, wantFull)
	}

	// Far edges clamped to width/height: (1,1,5,5) → only bottom-right pixel.
	dst3 := make([]byte, len(src))
	PackBGRARect(dst3, src, 2, 2, 1, 1, 5, 5)
	if !(dst3[12] == 15 && dst3[13] == 14 && dst3[14] == 13 && dst3[15] == 16) {
		t.Fatalf("PackBGRARect clamp-far = %v", dst3[12:16])
	}

	// Fully off-surface rect packs nothing.
	dst4 := make([]byte, len(src))
	PackBGRARect(dst4, src, 2, 2, 5, 5, 2, 2)
	for i, b := range dst4 {
		if b != 0 {
			t.Fatalf("PackBGRARect off-surface touched byte %d: %d", i, b)
		}
	}

	// Truncated dst/src trips the in-loop bounds guard (returns early).
	shortDst := make([]byte, 4)
	PackBGRARect(shortDst, src, 2, 2, 0, 0, 2, 2)
	if !(shortDst[0] == 3 && shortDst[1] == 2 && shortDst[2] == 1 && shortDst[3] == 4) {
		t.Fatalf("PackBGRARect short dst = %v", shortDst)
	}
}
