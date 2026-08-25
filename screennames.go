// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

// What every back-end has to decide once it has the rectangles: what to CALL
// each display, and which one is the primary.
//
// Neither answer is platform-specific, and both were getting decided
// per-platform. A user choosing an output, and an application looking for a
// particular headset again, must not have to know which display server they
// are on to know what Screen.Name is holding -- so the rule lives here, and
// the X11, Wayland and Win32 back-ends all pass their identifiers through it.

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

// primaryFirst moves the primary display to the front and guarantees that
// EXACTLY ONE screen carries the flag, which is what [Screen.Primary]
// promises.
//
// Neither half is automatic. A bare X server with no window manager marks no
// output primary at all — an Xvfb reports its single monitor as automatic and
// not primary — and a caller looking for "the main display" would find none.
// The first monitor RANDR states is the desktop's origin in that case, which is
// what the flag is for.
func primaryFirst(screens []Screen) []Screen {
	first := -1
	for i := range screens {
		if screens[i].Primary {
			if first < 0 {
				first = i
				continue
			}
			screens[i].Primary = false // only one may claim it
		}
	}
	if first < 0 {
		if len(screens) == 0 {
			return screens
		}
		first = 0
		screens[0].Primary = true
	}
	if first > 0 {
		p := screens[first]
		copy(screens[1:first+1], screens[:first])
		screens[0] = p
	}
	return screens
}
