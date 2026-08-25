// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !darwin && !linux && !windows

package window

// VisibleScreenSize returns the usable area of the primary screen in LOGICAL
// points (see screen_darwin.go for the full contract). On the platforms left
// here it reports ok=false: js/wasm has no screen-size query yet, so a caller
// must fall back to its own default — or leave Config.Width/Height ≤ 0 and let
// the backend choose a readable size.
//
// macOS answers in screen_darwin.go, Linux in screen_linux.go and Windows in
// screen_windows.go.
func VisibleScreenSize() (w, h int, ok bool) {
	return 0, 0, false
}

// Screens reports [ErrScreensUnsupported] on the platforms left here, which
// today means js/wasm alone. macOS answers through Cocoa, Linux through RANDR
// or wl_output and Windows through EnumDisplayMonitors; a browser has the
// Screen Detail API, so this remains a gap to be filled per back-end and not a
// limit of the API.
func Screens() ([]Screen, error) {
	return nil, ErrScreensUnsupported
}
