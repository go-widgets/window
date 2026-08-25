// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package cocoa

import (
	"errors"
	"fmt"
	"reflect"
	"testing"
)

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
		f := s.appKitFrame(float64(screens[0].Height))
		if f.Size.W != float64(s.Width) || f.Size.H != float64(s.Height) {
			t.Errorf("screen %d AppKit frame %vx%v disagrees with reported %dx%d",
				i, f.Size.W, f.Size.H, s.Width, s.Height)
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
	_, err := FindScreen(ScreenInfo{Name: "no such display", Width: 1, Height: 1})
	if err == nil {
		t.Fatal("FindScreen matched a display that is not attached")
	}
	// And it says WHICH failure it is: gone, not unreadable. A caller that
	// cannot tell those apart cannot tell a user anything useful.
	if _, listErr := Screens(); listErr == nil && !errors.Is(err, ErrScreenGone) {
		t.Errorf("FindScreen(unattached) = %v, want an ErrScreenGone", err)
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

// TestScreenInfoHasNoHiddenState is the guard on the mistake that caused a
// full-screen window to open on the wrong display.
//
// ScreenInfo used to carry an unexported nativeFrame: AppKit's own rectangle,
// which the enumerated value had and a value REBUILT from the numbers a caller
// was given did not. window.Screen.toCocoa rebuilds one on every Open, so the
// window was placed from a zero rectangle whenever anything used that field
// directly. Nothing in the type said so, and no test could have noticed.
//
// This one notices, on any machine, with nothing plugged in.
func TestScreenInfoHasNoHiddenState(t *testing.T) {
	tp := reflect.TypeOf(ScreenInfo{})
	for i := 0; i < tp.NumField(); i++ {
		if f := tp.Field(i); !f.IsExported() {
			t.Errorf("ScreenInfo.%s is unexported: a caller rebuilding a ScreenInfo "+
				"out of the values it was given cannot carry it across a package "+
				"boundary, so anything placement depends on must not live there",
				f.Name)
		}
	}
}

// TestARebuiltScreenPlacesWhereTheEnumeratedOneDoes states the same property
// behaviourally: copy the exported fields, as the public wrapper does, and the
// window goes to exactly the same place.
func TestARebuiltScreenPlacesWhereTheEnumeratedOneDoes(t *testing.T) {
	const primary = 1329
	enumerated := ScreenInfo{
		Name: "VITURE Beast", X: -7680, Y: 0, Width: 1920, Height: 1080,
		VisibleX: -7680, VisibleY: 0, VisibleWidth: 1920, VisibleHeight: 1080,
		Scale: 1,
	}
	// Exactly what window.Screen.toCocoa hands back to the back-end.
	rebuilt := ScreenInfo{
		Name:  enumerated.Name,
		X:     enumerated.X,
		Y:     enumerated.Y,
		Width: enumerated.Width, Height: enumerated.Height,
	}
	if got, want := rebuilt.appKitFrame(primary), enumerated.appKitFrame(primary); got != want {
		t.Errorf("a rebuilt screen places at %+v, the enumerated one at %+v", got, want)
	}
}

// TestAppKitFrame pins the conversion the placement rides on, with no display
// attached. The numbers are the ones measured on the machine the reported bug
// happened on: a 1329-point primary, and a 1080-point panel bottom-aligned with
// it 1920 points to the left.
func TestAppKitFrame(t *testing.T) {
	const primary = 1329
	for _, tc := range []struct {
		name string
		in   ScreenInfo
		want nsRect
	}{{
		"the primary screen itself",
		ScreenInfo{X: 0, Y: 0, Width: 2056, Height: 1329},
		nsRect{Origin: nsPoint{X: 0, Y: 0}, Size: nsSize{W: 2056, H: 1329}},
	}, {
		"a shorter panel to the LEFT, at a negative X",
		ScreenInfo{X: -1920, Y: 0, Width: 1920, Height: 1080},
		nsRect{Origin: nsPoint{X: -1920, Y: 249}, Size: nsSize{W: 1920, H: 1080}},
	}, {
		"the same panel pushed further left by three more displays",
		ScreenInfo{X: -7680, Y: 0, Width: 1920, Height: 1080},
		nsRect{Origin: nsPoint{X: -7680, Y: 249}, Size: nsSize{W: 1920, H: 1080}},
	}, {
		"a panel ABOVE the primary, at a negative Y",
		ScreenInfo{X: 0, Y: -1080, Width: 1920, Height: 1080},
		nsRect{Origin: nsPoint{X: 0, Y: 1329}, Size: nsSize{W: 1920, H: 1080}},
	}} {
		if got := tc.in.appKitFrame(primary); got != tc.want {
			t.Errorf("%s: appKitFrame = %+v, want %+v", tc.name, got, tc.want)
		}
	}
}

// TestVisibleTopLeftInAppKit pins where a titled window's FRAME goes on a
// chosen display: the top-left of the usable area, in AppKit's space.
func TestVisibleTopLeftInAppKit(t *testing.T) {
	const primary = 1329
	// The primary, with a 38-point menu bar: the usable area starts below it.
	s := ScreenInfo{X: 0, Y: 0, Width: 2056, Height: 1329, VisibleX: 0, VisibleY: 38}
	if got, want := s.visibleTopLeftInAppKit(primary), (nsPoint{X: 0, Y: 1291}); got != want {
		t.Errorf("visibleTopLeftInAppKit = %+v, want %+v", got, want)
	}
	// A secondary panel to the left, usable to its own top edge.
	s = ScreenInfo{X: -1920, Y: 0, Width: 1920, Height: 1080, VisibleX: -1920, VisibleY: 0}
	if got, want := s.visibleTopLeftInAppKit(primary), (nsPoint{X: -1920, Y: 1329}); got != want {
		t.Errorf("visibleTopLeftInAppKit = %+v, want %+v", got, want)
	}
}

// TestVisibleInset pins the menu-bar/Dock inset that is carried over onto the
// window server's live rectangle. Insets rather than absolute coordinates is
// the whole point: they stay true when AppKit's idea of where the display sits
// does not.
func TestVisibleInset(t *testing.T) {
	a := appKitScreen{
		frame:   nsRect{Origin: nsPoint{X: 0, Y: 0}, Size: nsSize{W: 2056, H: 1329}},
		visible: nsRect{Origin: nsPoint{X: 0, Y: 0}, Size: nsSize{W: 2056, H: 1291}},
	}
	if left, top := a.visibleInset(); left != 0 || top != 38 {
		t.Errorf("visibleInset = (%v, %v), want (0, 38) for a 38-point menu bar", left, top)
	}
	// A Dock on the left edge insets the usable area from the left instead.
	a = appKitScreen{
		frame:   nsRect{Origin: nsPoint{X: -1920, Y: 249}, Size: nsSize{W: 1920, H: 1080}},
		visible: nsRect{Origin: nsPoint{X: -1856, Y: 249}, Size: nsSize{W: 1856, H: 1080}},
	}
	if left, top := a.visibleInset(); left != 64 || top != 0 {
		t.Errorf("visibleInset = (%v, %v), want (64, 0) for a 64-point Dock on the left", left, top)
	}
}

// TestAppKitAgreesWithWindowServer runs on any macOS: whatever is attached, the
// two lists must describe it the same way. On a machine where nothing has
// disturbed the arrangement they always do; the value of asserting it here is
// that a machine where they DO NOT is exactly the machine the reported bug
// happens on, and this says so in one line instead of through a wrong window.
func TestAppKitAgreesWithWindowServer(t *testing.T) {
	live, err := liveDisplays()
	if err != nil {
		t.Skipf("no window server available: %v", err)
	}
	if len(live) == 0 {
		t.Skip("no display attached")
	}
	if !appKitAgreesWithWindowServer() && !syncAppKitScreens(appKitScreenSyncTimeout) {
		ak := appKitScreens()
		t.Errorf("AppKit lists %d screen(s), the window server %d, and they did not "+
			"converge within %s", len(ak), len(live), appKitScreenSyncTimeout)
	}
}

// TestLiveDisplaysDescribeACoherentDesktop asserts what the window server's own
// list must always satisfy, so a machine whose answer is nonsense says so here
// rather than through a window in the wrong place.
func TestLiveDisplaysDescribeACoherentDesktop(t *testing.T) {
	live, err := liveDisplays()
	if err != nil {
		t.Skipf("no window server available: %v", err)
	}
	if len(live) == 0 {
		t.Skip("no display attached")
	}
	if !live[0].main {
		t.Error("liveDisplays did not put the main display first")
	}
	mains := 0
	for _, d := range live {
		if d.main {
			mains++
		}
		if d.bounds.W <= 0 || d.bounds.H <= 0 {
			t.Errorf("display %d has non-positive bounds %+v", d.id, d.bounds)
		}
	}
	if mains != 1 {
		t.Errorf("got %d main displays among %d, want exactly 1", mains, len(live))
	}
	p, err := primaryBounds()
	if err != nil {
		t.Fatalf("primaryBounds() = %v", err)
	}
	if p != live[0].bounds {
		t.Errorf("primaryBounds() = %+v, want the main display's %+v", p, live[0].bounds)
	}
}

// TestCoreGraphicsFailureIsReported covers the branch that says the display
// list could not be read at all, without breaking the machine to get there.
func TestCoreGraphicsFailureIsReported(t *testing.T) {
	// The binding is loaded once per process; this exercises the failure the
	// loader reports rather than re-running it.
	saved := cgErr
	defer func() { cgErr = saved }()
	cgErr = fmt.Errorf("%w: simulated", ErrDisplayList)
	if _, err := liveDisplays(); !errors.Is(err, ErrDisplayList) {
		t.Errorf("liveDisplays() with an unloadable framework = %v, want an ErrDisplayList", err)
	}
	if _, err := primaryBounds(); !errors.Is(err, ErrDisplayList) {
		t.Errorf("primaryBounds() with an unloadable framework = %v, want an ErrDisplayList", err)
	}
	if _, err := Screens(); !errors.Is(err, ErrDisplayList) {
		t.Errorf("Screens() with an unloadable framework = %v, want an ErrDisplayList", err)
	}
	if _, err := FindScreen(ScreenInfo{Name: "anything"}); !errors.Is(err, ErrDisplayList) {
		t.Errorf("FindScreen() with an unloadable framework = %v, want an ErrDisplayList", err)
	}
}
