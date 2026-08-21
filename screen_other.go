// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !darwin

package window

// VisibleScreenSize returns the usable area of the primary screen in LOGICAL
// points (see screen_darwin.go for the full contract). On every non-macOS
// platform it reports ok=false for now: the X11, Wayland, Windows and js/wasm
// backends have no screen-size query yet, so a caller must fall back to its own
// default — or leave Config.Width/Height ≤ 0 and let the backend choose a
// readable size.
func VisibleScreenSize() (w, h int, ok bool) {
	return 0, 0, false
}

// Screens reports [ErrScreensUnsupported] on every non-macOS platform. The
// information exists on all of them — RandR, wl_output, EnumDisplayMonitors —
// so this is a gap to be filled per back-end, not a limit of the API.
func Screens() ([]Screen, error) {
	return nil, ErrScreensUnsupported
}
