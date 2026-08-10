// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux && !darwin && !(js && wasm)

package window

// Open is unavailable on platforms with no native windowing backend — i.e.
// everything that is NOT Linux (X11/Wayland, open_linux.go), macOS (Cocoa/
// AppKit, open_darwin.go) or the js/wasm wasmbox environment (open_js.go): there
// is no display server to dial, so it returns ErrUnsupported. Those three
// backends are excluded from this stub, each supplying a real Open.
// The window-construction, presentation and event-translation logic of both
// native backends remains compiled and unit-tested on every platform via the
// transport-agnostic internal/x11 and internal/wayland connections; only
// this environment-driven entry point is gated, keeping cross-builds green.
func Open(cfg Config) (Backend, error) {
	_ = cfg
	return nil, ErrUnsupported
}
