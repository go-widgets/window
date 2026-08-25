// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package window

import (
	"os"
	"testing"
)

// The live display enumeration, on a real Windows machine.
//
// It is gated behind an environment variable so ordinary `go test` (and CI,
// which has no desktop) skips it; it is cross-compiled on the developer's
// machine (`go test -c`), copied to the Win11 ARM64 QEMU VM and run there.
//
// It must run in the INTERACTIVE session. Over ssh a Windows process is in
// session 0, which has a phantom 1024x768 desktop called `WinDisc` that no
// user can see — an enumeration there reports a display that does not exist,
// and passes. `schtasks /ru <desktop user> /it` puts it in session 1.
//
// What it prints is deliberately in a form that can be compared field by field
// against an instrument that is not this code: `[System.Windows.Forms.Screen]::AllScreens`
// reports the same device name, bounds, work area and primary flag through
// .NET's own binding of the same API.
func TestLiveScreens(t *testing.T) {
	if os.Getenv("WINDOW_LIVE_SCREENS") != "1" {
		t.Skip("set WINDOW_LIVE_SCREENS=1 to enumerate this machine's displays")
	}
	screens, err := Screens()
	if err != nil {
		t.Fatalf("Screens: %v", err)
	}
	if len(screens) == 0 {
		t.Fatal("Screens returned no display and no error")
	}
	for i, s := range screens {
		t.Logf("SCREEN %d name=%q bounds=%d,%d %dx%d visible=%d,%d %dx%d scale=%v primary=%v",
			i, s.Name, s.X, s.Y, s.Width, s.Height,
			s.VisibleX, s.VisibleY, s.VisibleWidth, s.VisibleHeight, s.Scale, s.Primary)
	}

	// The contract the API promises, asserted against whatever is plugged in
	// rather than against a machine the test hopes for.
	if !screens[0].Primary {
		t.Error("the first screen does not carry the primary flag")
	}
	seenPrimary, names := 0, map[string]int{}
	for _, s := range screens {
		if s.Primary {
			seenPrimary++
		}
		if s.Width <= 0 || s.Height <= 0 {
			t.Errorf("screen %q has a non-positive size %dx%d", s.Name, s.Width, s.Height)
		}
		if s.Scale <= 0 {
			t.Errorf("screen %q reports scale %v", s.Name, s.Scale)
		}
		// The usable area must be inside the panel, or a caller sizing a
		// window to it puts the window off the display.
		if s.VisibleWidth > s.Width || s.VisibleHeight > s.Height {
			t.Errorf("screen %q: visible %dx%d is larger than its bounds %dx%d",
				s.Name, s.VisibleWidth, s.VisibleHeight, s.Width, s.Height)
		}
		names[s.Name]++
	}
	if seenPrimary != 1 {
		t.Errorf("%d screens carry the primary flag, want exactly 1", seenPrimary)
	}
	// A name two displays share is not a name, and resolveNames exists to stop
	// that happening. With one display attached this asserts nothing, which is
	// worth saying rather than reading as coverage.
	for name, n := range names {
		if n > 1 {
			t.Errorf("%d screens are both called %q", n, name)
		}
	}
	if len(screens) == 1 {
		t.Log("ONE DISPLAY: nothing here exercised the multi-monitor rules")
	}

	w, h, ok := VisibleScreenSize()
	if !ok {
		t.Fatal("VisibleScreenSize reported nothing while Screens reported a display")
	}
	if w != screens[0].VisibleWidth || h != screens[0].VisibleHeight {
		t.Errorf("VisibleScreenSize = %dx%d, want the primary's usable area %dx%d",
			w, h, screens[0].VisibleWidth, screens[0].VisibleHeight)
	}
	t.Log("SCREENS_OK")
}
