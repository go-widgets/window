// Copyright (c) the go-widgets authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package nativeclip_test

import (
	"testing"

	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/window/internal/nativeclip"
)

func rect(x, y, w, h int) toolkit.Rect { return toolkit.Rect{X: x, Y: y, W: w, H: h} }

func TestClippedOnlyWhenPartOfTheControlIsHidden(t *testing.T) {
	full := rect(10, 20, 100, 30)
	for _, c := range []struct {
		name string
		clip toolkit.Rect
		want bool
	}{
		{"fully in view: the clip IS the control", full, false},
		{"scrolled off the top: only its bottom shows", rect(10, 20, 100, 12), true},
		{"scrolled off the bottom: only its top shows", rect(10, 38, 100, 12), true},
		{"narrowed: only its left shows", rect(10, 20, 40, 30), true},
		{"out of view entirely (empty height)", rect(10, 20, 100, 0), false},
		{"out of view entirely (empty width)", rect(10, 20, 0, 30), false},
		{"a negative extent is not a rectangle", rect(10, 20, 100, -4), false},
	} {
		if got := nativeclip.Clipped(full, c.clip); got != c.want {
			t.Errorf("%s: Clipped = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestFramesKeepTheControlWholeInsideTheVisibleBox(t *testing.T) {
	// A control 30 tall at y=20, of which only the bottom 12 rows still show:
	// the box takes the visible rows, and the control hangs 18 rows above it.
	outer, inner := nativeclip.Frames(rect(10, 20, 100, 30), rect(10, 38, 100, 12))

	if want := rect(10, 38, 100, 12); outer != want {
		t.Errorf("outer = %+v, want the visible rectangle %+v", outer, want)
	}
	if want := rect(0, -18, 100, 30); inner != want {
		t.Errorf("inner = %+v, want %+v", inner, want)
	}
	if inner.W != 100 || inner.H != 30 {
		t.Errorf("the control was resized to %dx%d: it must keep its full size, "+
			"or it redraws its label to fit and looks like a different control",
			inner.W, inner.H)
	}
}

func TestRegionIsTheVisiblePartInTheControlsOwnCoordinates(t *testing.T) {
	got := nativeclip.Region(rect(10, 20, 100, 30), rect(10, 38, 100, 12))
	if want := rect(0, 18, 100, 12); got != want {
		t.Errorf("Region = %+v, want %+v", got, want)
	}
}

// TestFramesAndRegionDisagreeInSign holds the two answers to the relation the
// package claims: one offsets the control inside the visible box, the other
// offsets the visible box inside the control, so their origins must be exact
// negations. This is the drift the package exists to prevent -- a backend fixed
// on its own could satisfy its own test and contradict the other two.
func TestFramesAndRegionDisagreeInSign(t *testing.T) {
	for _, c := range []struct{ ctl, clip toolkit.Rect }{
		{rect(10, 20, 100, 30), rect(10, 38, 100, 12)},
		{rect(10, 20, 100, 30), rect(10, 20, 100, 12)},
		{rect(-5, -7, 40, 40), rect(3, 11, 9, 9)},
		{rect(0, 0, 1, 1), rect(0, 0, 1, 1)},
	} {
		_, inner := nativeclip.Frames(c.ctl, c.clip)
		reg := nativeclip.Region(c.ctl, c.clip)
		if inner.X != -reg.X || inner.Y != -reg.Y {
			t.Errorf("control %+v clip %+v: inner origin (%d,%d) is not the negation "+
				"of the region origin (%d,%d)", c.ctl, c.clip, inner.X, inner.Y, reg.X, reg.Y)
		}
	}
}
