// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package window

import "github.com/go-widgets/window/internal/cocoa"

// VisibleScreenSize returns the usable area of the primary screen in LOGICAL
// points — the same unit Config.Width/Height are expressed in — with the menu
// bar and Dock excluded on macOS. ok is false when the size cannot be
// determined (a headless build, or a display server with no screen).
//
// It is safe to call before Open, so a desktop shell that wants to launch at,
// say, the full screen height can query the size here and pass it back through
// Config.Height: NewScaled honours an explicit (> 0) size verbatim, applying no
// readability clamp. Today only the macOS (Cocoa) backend reports a size; the
// X11, Wayland, Windows and js/wasm backends return ok=false (see
// screen_other.go) until they grow an equivalent query.
func VisibleScreenSize() (w, h int, ok bool) {
	return cocoa.VisibleScreenSize()
}
