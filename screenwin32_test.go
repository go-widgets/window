// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import "testing"

// The Windows projection, tested on every platform.
//
// What the OS says is not what [Screen] promises: Windows states physical
// pixels and a DPI, [Screen] is logical points and a factor. The arithmetic
// between them is where the mistakes are — a scale applied twice, a work area
// that was not converted with the rest, a left-of-origin monitor whose
// negative coordinate got rounded the wrong way — and none of them need a
// Windows machine to catch. The live half is screen_windows.go, and the VM
// proof that goes with it is in the pull request.

// A mixed-DPI desktop with the secondary panel to the LEFT of the primary, at
// a different scale. This is the layout that makes the Windows back-end
// different from every other one, and the comment on winScreensOf explains why
// the two rectangles do not tile in points: they tile in PIXELS, which is the
// only space Windows has.
func TestWinScreensOfMixedDPI(t *testing.T) {
	got := winScreensOf([]winMonitor{
		// A 4K panel at 200%, sitting to the left of the origin.
		{
			Device: `\\.\DISPLAY2`, Model: "VITURE Beast",
			X: -3840, Y: 0, W: 3840, H: 2160,
			WorkX: -3840, WorkY: 0, WorkW: 3840, WorkH: 2160,
			DPI: 192,
		},
		// The primary, 1080p at 100%, with a 48-pixel taskbar along the
		// bottom.
		{
			Device: `\\.\DISPLAY1`, Model: "DELL U2720Q",
			X: 0, Y: 0, W: 1920, H: 1080,
			WorkX: 0, WorkY: 0, WorkW: 1920, WorkH: 1032,
			DPI: 96, Primary: true,
		},
	})
	if len(got) != 2 {
		t.Fatalf("got %d screens, want 2", len(got))
	}
	// Primary first, whatever order the OS walked them in.
	if !got[0].Primary || got[0].Name != "DELL U2720Q" {
		t.Fatalf("first screen is %+v, want the primary DELL", got[0])
	}
	if got[1].Primary {
		t.Errorf("%q also carries the primary flag", got[1].Name)
	}
	want := []Screen{
		{
			Name: "DELL U2720Q", X: 0, Y: 0, Width: 1920, Height: 1080,
			VisibleX: 0, VisibleY: 0, VisibleWidth: 1920, VisibleHeight: 1032,
			Scale: 1, Primary: true,
		},
		{
			// 3840 physical pixels at 192 dpi is 1920 points, and the panel's
			// left edge is at -1920 points, not -3840.
			Name: "VITURE Beast", X: -1920, Y: 0, Width: 1920, Height: 1080,
			VisibleX: -1920, VisibleY: 0, VisibleWidth: 1920, VisibleHeight: 1080,
			Scale: 2,
		},
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("screen %d =\n  %+v\nwant\n  %+v", i, got[i], want[i])
		}
	}
}

// Windows produces the "two displays with the same name" hazard more often
// than any other platform: the inbox monitor driver describes every panel it
// handles identically, so a machine with two ordinary monitors has two
// "Generic PnP Monitor"s and nothing else to tell them apart.
func TestWinScreensOfTwoIdenticalPanels(t *testing.T) {
	got := winScreensOf([]winMonitor{
		{Device: `\\.\DISPLAY1`, Model: "Generic PnP Monitor", W: 1920, H: 1080, DPI: 96, Primary: true},
		{Device: `\\.\DISPLAY2`, Model: "Generic PnP Monitor", X: 1920, W: 1920, H: 1080, DPI: 96},
	})
	for i, want := range []string{`\\.\DISPLAY1`, `\\.\DISPLAY2`} {
		if got[i].Name != want {
			t.Errorf("screen %d is named %q, want the device name %q", i, got[i].Name, want)
		}
	}
	// One of them alone keeps its model: the name is only useless when it is
	// shared.
	one := winScreensOf([]winMonitor{
		{Device: `\\.\DISPLAY1`, Model: "Generic PnP Monitor", W: 1920, H: 1080, DPI: 96, Primary: true},
	})
	if one[0].Name != "Generic PnP Monitor" {
		t.Errorf("a lone panel is named %q, want its model", one[0].Name)
	}
}

// A monitor the OS reported no work area for keeps all of itself. An empty
// work rectangle projected as-is would make the display's usable area zero,
// and a caller sizing a window to it would get nothing.
func TestWinScreensOfWithoutAWorkArea(t *testing.T) {
	got := winScreensOf([]winMonitor{
		{Device: `\\.\DISPLAY1`, X: 10, Y: 20, W: 1920, H: 1080, DPI: 96, Primary: true},
	})
	s := got[0]
	if s.VisibleX != 10 || s.VisibleY != 20 || s.VisibleWidth != 1920 || s.VisibleHeight != 1080 {
		t.Errorf("visible area %d,%d %dx%d, want the full bounds 10,20 1920x1080",
			s.VisibleX, s.VisibleY, s.VisibleWidth, s.VisibleHeight)
	}
}

// A desktop with nothing on it is a list with nothing in it, not a panic.
func TestWinScreensOfNothing(t *testing.T) {
	if got := winScreensOf(nil); len(got) != 0 {
		t.Errorf("winScreensOf(nil) = %+v, want no screens", got)
	}
}

func TestWinPointsAndScale(t *testing.T) {
	for _, tc := range []struct {
		name  string
		px    int
		dpi   int
		want  int
		scale float64
	}{
		{"100% is one point per pixel", 1920, 96, 1920, 1},
		{"200%", 3840, 192, 1920, 2},
		{"150%", 2880, 144, 1920, 1.5},
		// 1080 physical at 150% is 720 exactly; 1081 is 720.67, which must
		// round up rather than truncate down to 720.
		{"rounds to nearest, not down", 1081, 144, 721, 1.5},
		// A monitor to the LEFT of the origin is at a negative X, which is
		// ordinary and must not creep inwards: -1441 at 150% is -960.67.
		{"a negative coordinate rounds away from zero", -1441, 144, -961, 1.5},
		{"an unknown DPI is 100%", 1920, 0, 1920, 1},
		{"a nonsensical DPI is 100%", 1920, -96, 1920, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := winPoints(tc.px, tc.dpi); got != tc.want {
				t.Errorf("winPoints(%d, %d) = %d, want %d", tc.px, tc.dpi, got, tc.want)
			}
			if got := winScale(tc.dpi); got != tc.scale {
				t.Errorf("winScale(%d) = %v, want %v", tc.dpi, got, tc.scale)
			}
		})
	}
}

// The path from a monitor's device id to its EDID. The first case is the
// interface path a real machine produced (the Win11 ARM64 VM, 2026-08-25), and
// the key it names is the one that machine actually has.
func TestEDIDRegistryPath(t *testing.T) {
	for _, tc := range []struct {
		name string
		id   string
		want string
	}{
		{
			"a real device interface path",
			`\\?\DISPLAY#Default_Monitor#1&8713bca&0&UID0#{e6f07b5f-ee97-4a90-b076-33f57bf4eaa7}`,
			`SYSTEM\CurrentControlSet\Enum\DISPLAY\Default_Monitor\1&8713bca&0&UID0\Device Parameters`,
		},
		{
			"a panel that publishes a real PnP id",
			`\\?\DISPLAY#DELA0FF#5&2a5a2f3&0&UID4353#{e6f07b5f-ee97-4a90-b076-33f57bf4eaa7}`,
			`SYSTEM\CurrentControlSet\Enum\DISPLAY\DELA0FF\5&2a5a2f3&0&UID4353\Device Parameters`,
		},
		// Without EDD_GET_DEVICE_INTERFACE_NAME the field holds the hardware
		// id instead, which names the driver's class key: three parts, but the
		// third is a GUID and the second a PnP id, and the key it would build
		// does not exist. It must be refused rather than looked up.
		{
			"the hardware id, which is not an interface path",
			`MONITOR\DELA0FF\{4d36e96e-e325-11ce-bfc1-08002be10318}\0002`,
			"",
		},
		{"an adapter, not a monitor",
			`\\?\PCI#VEN_1234#3&11583659&0&10#{guid}`, ""},
		{"nothing at all", "", ""},
		{"too few parts", `\\?\DISPLAY#DELA0FF`, ""},
		{"an empty instance", `\\?\DISPLAY#DELA0FF##{guid}`, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := edidRegistryPath(tc.id); got != tc.want {
				t.Errorf("edidRegistryPath(%q) =\n  %q\nwant\n  %q", tc.id, got, tc.want)
			}
		})
	}
}
