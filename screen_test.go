// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"errors"
	"testing"
)

// TestVisibleScreenSize asserts the platform-independent contract, so it holds
// whichever implementation is compiled in: on macOS it exercises the real
// Cocoa query (screen_darwin.go), on every other platform the ok=false stub
// (screen_other.go). Either way the invariant is the same — a reported size is
// strictly positive, and an unavailable size is exactly (0, 0) — which is all a
// caller may rely on.
func TestVisibleScreenSize(t *testing.T) {
	w, h, ok := VisibleScreenSize()
	if !ok {
		if w != 0 || h != 0 {
			t.Fatalf("VisibleScreenSize unavailable but returned %dx%d, want 0x0", w, h)
		}
		return
	}
	if w <= 0 || h <= 0 {
		t.Fatalf("VisibleScreenSize ok but returned non-positive size %dx%d", w, h)
	}
}

// TestScreenIsZero pins the zero-value predicate, which is how a caller tells
// "no display chosen" from a real one without a pointer dance.
func TestScreenIsZero(t *testing.T) {
	if !(Screen{}).IsZero() {
		t.Error("Screen{}.IsZero() = false, want true")
	}
	for _, s := range []Screen{
		{Name: "panel"},
		{Width: 1},
		{Height: 1},
		{X: -1},
		{Y: -1},
		{Scale: 1},
		{Primary: true},
		{VisibleWidth: 1},
	} {
		if s.IsZero() {
			t.Errorf("Screen%+v.IsZero() = true, want false", s)
		}
	}
}

// TestScreens asserts the platform-independent contract, so it holds on every
// back-end: either the enumeration is unsupported and the list is empty, or it
// succeeds and every screen it reports is coherent. It deliberately requires no
// particular display — what is plugged in is not the test's business.
func TestScreens(t *testing.T) {
	screens, err := Screens()
	if err != nil {
		if len(screens) != 0 {
			t.Fatalf("Screens() failed (%v) but returned %d screens, want none", err, len(screens))
		}
		if !errors.Is(err, ErrScreensUnsupported) {
			t.Logf("Screens() unavailable for a platform reason: %v", err)
		}
		return
	}
	primaries := 0
	for i, s := range screens {
		if s.IsZero() {
			t.Errorf("screen %d is the zero value", i)
		}
		if s.Width <= 0 || s.Height <= 0 {
			t.Errorf("screen %d has non-positive bounds %dx%d", i, s.Width, s.Height)
		}
		if s.Scale <= 0 {
			t.Errorf("screen %d reports scale %v, want > 0", i, s.Scale)
		}
		if s.Primary {
			primaries++
		}
	}
	if len(screens) > 0 && primaries != 1 {
		t.Errorf("got %d primary screens among %d, want exactly 1", primaries, len(screens))
	}
}
