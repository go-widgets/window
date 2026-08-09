// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build js && wasm

package window

import (
	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/window/internal/wasmbox"
)

// Open returns the wasmbox client backend: on js/wasm the environment IS the
// wasmdesk/wasmbox browser compositor, so there is no X11 server or Wayland
// socket to dial — instead a go-widgets app runs as an external client of the
// compositor, painting into a SharedArrayBuffer surface and speaking wasmbox's
// client protocol over its per-client MessagePort (see internal/wasmbox). This
// makes ONE go-widgets application run unchanged natively (X11/Wayland via
// open_linux.go) AND inside wasmdesk (here): Open picks the backend, Run drives
// the same widget tree.
//
// Dial blocks until the compositor grants the surface (hello → welcome); the Go
// runtime yields to the JS event loop while it waits, so the port handoff and
// welcome callbacks fire. cfg.Width/Height default to 640×480 and cfg.Theme to
// toolkit.DefaultDark, matching the native backends.
func Open(cfg Config) (Backend, error) {
	theme := cfg.Theme
	if theme == nil {
		theme = toolkit.DefaultDark()
	}
	return wasmbox.Dial(cfg.Title, cfg.Width, cfg.Height, theme)
}
