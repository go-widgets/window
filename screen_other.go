// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !darwin && !linux

package window

// VisibleScreenSize returns the usable area of the primary screen in LOGICAL
// points (see screen_darwin.go for the full contract). On the platforms left
// here it reports ok=false: the Windows and js/wasm backends have no
// screen-size query yet, so a caller must fall back to its own default — or
// leave Config.Width/Height ≤ 0 and let the backend choose a readable size.
//
// macOS answers in screen_darwin.go and X11 in screen_x11.go.
func VisibleScreenSize() (w, h int, ok bool) {
	return 0, 0, false
}

// Screens reports [ErrScreensUnsupported] on the platforms left here. macOS
// answers through Cocoa and Linux through RANDR; Windows has
// EnumDisplayMonitors and a browser has the Screen Detail API, so this remains
// a gap to be filled per back-end and not a limit of the API.
func Screens() ([]Screen, error) {
	return nil, ErrScreensUnsupported
}
