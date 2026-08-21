// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package cocoa

import "testing"

// TestFlipY pins the whole of the coordinate-space difference between AppKit
// (bottom-left origin, Y up) and the rest of this repo (top-left, Y down). It
// needs no display, which is the reason the conversion is a separate function:
// the one piece of screen handling that is pure arithmetic is also the one most
// likely to be silently wrong by the height of a menu bar.
func TestFlipY(t *testing.T) {
	const primary = 1000 // primary screen 1000 points tall

	for _, tc := range []struct {
		name            string
		y, height, want float64
	}{
		// The primary screen itself: AppKit puts it at y=0, and flipped it must
		// also start at 0 — the two spaces agree on the origin corner's screen.
		{"primary screen", 0, 1000, 0},
		// A screen of the same height placed to the side stays at 0.
		{"side screen, equal height", 0, 1000, 0},
		// A shorter screen bottom-aligned with the primary hangs DOWN from the
		// top in flipped space: 1000 - (0 + 400) = 600.
		{"shorter screen, bottom aligned", 0, 400, 600},
		// A screen ABOVE the primary has positive AppKit y and negative flipped
		// y: 1000 - (1000 + 800) = -800.
		{"screen above the primary", 1000, 800, -800},
		// A screen BELOW the primary has negative AppKit y: 1000 - (-600 + 600).
		{"screen below the primary", -600, 600, 1000},
		// A visible frame inset from the bottom by the Dock.
		{"visible frame above a dock", 80, 900, 20},
	} {
		if got := flipY(tc.y, tc.height, primary); got != tc.want {
			t.Errorf("%s: flipY(%v, %v, %v) = %v, want %v",
				tc.name, tc.y, tc.height, primary, got, tc.want)
		}
	}
}

// TestFlipYIsAnInvolution asserts the conversion is its own inverse, which is
// what makes it safe to apply in either direction without a second function to
// keep in step.
func TestFlipYIsAnInvolution(t *testing.T) {
	const primary = 1440
	for _, y := range []float64{-900, -1, 0, 1, 100, 1440, 2000} {
		for _, h := range []float64{1, 200, 1080, 1440} {
			back := flipY(flipY(y, h, primary), h, primary)
			if back != y {
				t.Errorf("flipY twice on (y=%v h=%v) = %v, want the original %v", y, h, back, y)
			}
		}
	}
}

// TestScreensReportsACoherentDesktop runs on any macOS, CI included, and asserts
// the invariants a caller is entitled to rely on. It deliberately asserts no
// particular display: what is attached is not this test's business.
func TestScreensReportsACoherentDesktop(t *testing.T) {
	screens, err := Screens()
	if err != nil {
		t.Skipf("no display available: %v", err)
	}
	if len(screens) == 0 {
		t.Skip("no display attached")
	}

	primaries := 0
	for i, s := range screens {
		if s.Primary {
			primaries++
			// AppKit's global origin is the primary screen's top-left corner, so
			// after flipping it must land exactly at (0,0). If this ever fails,
			// the conversion is wrong, not the display.
			if s.X != 0 || s.Y != 0 {
				t.Errorf("screen %d is primary but sits at (%d,%d), want (0,0)", i, s.X, s.Y)
			}
		}
		if s.Width <= 0 || s.Height <= 0 {
			t.Errorf("screen %d has non-positive bounds %dx%d", i, s.Width, s.Height)
		}
		if s.VisibleWidth <= 0 || s.VisibleHeight <= 0 {
			t.Errorf("screen %d has non-positive visible area %dx%d", i, s.VisibleWidth, s.VisibleHeight)
		}
		if s.VisibleWidth > s.Width || s.VisibleHeight > s.Height {
			t.Errorf("screen %d visible area %dx%d exceeds its bounds %dx%d",
				i, s.VisibleWidth, s.VisibleHeight, s.Width, s.Height)
		}
		if s.Scale <= 0 {
			t.Errorf("screen %d reports scale %v, want > 0", i, s.Scale)
		}
		if s.nativeFrame.Size.W != float64(s.Width) || s.nativeFrame.Size.H != float64(s.Height) {
			t.Errorf("screen %d native frame %vx%v disagrees with reported %dx%d",
				i, s.nativeFrame.Size.W, s.nativeFrame.Size.H, s.Width, s.Height)
		}
	}
	if primaries != 1 {
		t.Errorf("got %d primary screens among %d, want exactly 1", primaries, len(screens))
	}
	if !screens[0].Primary {
		t.Error("screens[0] is not the primary; the order must put the origin-owning display first")
	}
}

// TestFindScreenRejectsAnUnknownScreen covers the case that matters for an
// external panel: a value that no longer describes anything attached must be
// refused, not approximated to the nearest display.
func TestFindScreenRejectsAnUnknownScreen(t *testing.T) {
	if _, ok := FindScreen(ScreenInfo{Name: "no such display", Width: 1, Height: 1}); ok {
		t.Error("FindScreen matched a display that is not attached")
	}
}

// TestResolveScreenErrors covers the placement-resolution branches without
// opening a window.
func TestResolveScreenErrors(t *testing.T) {
	// No screen and no fullscreen: nothing to resolve, and that is not an error.
	s, err := Options{}.resolveScreen()
	if s != nil || err != nil {
		t.Errorf("Options{}.resolveScreen() = (%v, %v), want (nil, nil)", s, err)
	}

	// A named screen that is not attached must be refused.
	want := ScreenInfo{Name: "gone", Width: 123, Height: 456}
	if _, err := (Options{Screen: &want}).resolveScreen(); err == nil {
		t.Error("resolveScreen() accepted an unattached screen, want ErrScreenGone")
	}
}
