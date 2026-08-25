// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import "errors"

// ErrScreensUnsupported is returned by [Screens] on a back-end that cannot yet
// enumerate displays. It is not a failure to handle defensively so much as a
// statement of coverage: today macOS answers through Cocoa and X11 through
// RANDR, and Wayland, Windows and js/wasm do not.
var ErrScreensUnsupported = errors.New("window: this back-end cannot enumerate screens yet")

// Screen describes one attached display, in LOGICAL points — the unit the
// toolkit lays out and the user reads in, not device pixels.
//
// X and Y are a TOP-LEFT origin with Y growing downwards, so a screen sitting
// above the primary one has a negative Y. That is the convention the X11,
// Wayland and Win32 back-ends use; macOS's own space is bottom-left with Y
// growing up, and the Cocoa back-end converts. A caller therefore never has to
// know which platform it is on to reason about the layout of the desktop.
//
// Placement does not go through these numbers. Pass the Screen value itself
// back through [Config.Screen] and the back-end re-resolves it against the
// displays attached at that moment, so a window lands on the panel the caller
// picked rather than at coordinates that may have stopped describing it.
type Screen struct {
	// Name is the display's human-readable name, e.g. "Color LCD" or
	// "VITURE Beast". It is what to show a user choosing an output, and may be
	// empty on a display that publishes none.
	//
	// It is the panel's OWN name wherever the platform offers one and that name
	// identifies the display — on Linux the product string out of its EDID. It
	// falls back to the connector ("HDMI-1", "DP-2", "HEADLESS-1") when the
	// display publishes no product name, and ALSO when it publishes one that
	// two attached displays share: two identical monitors say the identical
	// thing about themselves, and a name that cannot tell them apart is not a
	// name.
	Name string
	// X, Y, Width, Height are the display's full bounds.
	X, Y          int
	Width, Height int
	// Visible* is the usable area, with the menu bar and Dock (or their
	// platform equivalents) excluded. On a secondary display it is normally the
	// full bounds.
	VisibleX, VisibleY          int
	VisibleWidth, VisibleHeight int
	// Scale is the display's backing factor: device pixels per logical point. A
	// Retina panel reports 2.
	Scale float64
	// Primary reports the display that owns the desktop's origin — the one
	// carrying the menu bar or task bar. Exactly one screen has it set.
	//
	// It is deliberately not "the active screen": which display holds the
	// focused window changes as the user clicks around, and a caller choosing an
	// output wants the stable answer.
	Primary bool
}

// IsZero reports whether s names no display, which is what a caller gets from
// the zero value.
func (s Screen) IsZero() bool { return s == Screen{} }
