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

// displayName holds the identifiers a back-end obtained for one display, in
// decreasing order of how much they say about the panel itself.
type displayName struct {
	// Model is the panel's own product name, out of its EDID: "DELL U2720Q".
	Model string
	// Connector is the socket it is plugged into: "HDMI-1", "DP-2",
	// "HEADLESS-1". Unique by construction, and stable across reboots.
	Connector string
	// Vendor is the manufacturer, the other half of the EDID.
	Vendor string
}

// resolveNames picks the name to show for each display.
//
// A model is not automatically a name. Two identical monitors publish the
// identical EDID string, and a compositor with no EDID to read publishes one
// placeholder for every output it has — wlroots emits the literal "Unknown".
// In both cases the model names the MODEL and not the DISPLAY, and a user
// asked to choose between two "DELL U2720Q" cannot; nor can an application
// asked to find one of them again. The connector is unique by construction,
// which is exactly what is wanted then.
//
// So: the model where it identifies exactly one display, the connector where
// it does not, the manufacturer where there is no connector either, and "" for
// a display that says nothing about itself at all.
func resolveNames(ids []displayName) []string {
	seen := make(map[string]int, len(ids))
	for _, id := range ids {
		if id.Model != "" {
			seen[id.Model]++
		}
	}
	out := make([]string, len(ids))
	for i, id := range ids {
		switch {
		case id.Model != "" && seen[id.Model] == 1:
			out[i] = id.Model
		case id.Connector != "":
			out[i] = id.Connector
		case id.Model != "":
			// Ambiguous, but it is all this display has said; a shared name is
			// still better than no name.
			out[i] = id.Model
		default:
			out[i] = id.Vendor
		}
	}
	return out
}
