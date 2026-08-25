// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"fmt"
	"os"
)

// Screens enumerates the attached displays, primary first, in LOGICAL points.
// It is safe to call before Open — picking an output is something an
// application does on the way in.
//
// It asks the SAME display server Open would dial, and by the same rule:
// Wayland when $WAYLAND_DISPLAY is set, X11 otherwise. Anything else would let
// a caller pick a display off one server and open a window on another.
//
// See [Screen] for what the fields mean; the two back-ends fill them from very
// different protocols and are documented where they do it (screen_wayland.go
// and screen_x11.go).
func Screens() ([]Screen, error) {
	if name := os.Getenv("WAYLAND_DISPLAY"); name != "" {
		return waylandScreens(name)
	}
	disp := os.Getenv("DISPLAY")
	if disp == "" {
		return nil, fmt.Errorf("window: cannot enumerate screens: neither WAYLAND_DISPLAY nor DISPLAY is set")
	}
	return x11Screens(disp)
}

// VisibleScreenSize returns the usable area of the primary display in LOGICAL
// points — on X11 the full panel minus whatever the desktop reserved through
// _NET_WM_STRUT. ok is false when no display server can be reached, or when it
// reports no display.
//
// See [Screens], which supersedes it for anything multi-display: it reports
// every attached panel, not only the primary one.
func VisibleScreenSize() (w, h int, ok bool) {
	screens, err := Screens()
	if err != nil || len(screens) == 0 {
		return 0, 0, false
	}
	s := screens[0]
	if s.VisibleWidth <= 0 || s.VisibleHeight <= 0 {
		return 0, 0, false
	}
	return s.VisibleWidth, s.VisibleHeight, true
}
