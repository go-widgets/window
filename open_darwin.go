// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package window

import (
	"runtime"

	"github.com/go-widgets/window/internal/cocoa"
)

// Open returns the native macOS (Cocoa/AppKit) backend: it creates a real
// NSWindow with a flipped content NSView, ready for Run to present the
// go-widgets framebuffer and route NSEvent input. The backend is pure-Go
// (CGO-free) via github.com/go-macos/objc over purego, so a go-widgets app runs
// through Open→Run on macOS exactly as it does on X11/Wayland (open_linux.go)
// and inside wasmdesk (open_js.go).
//
// AppKit requires all window and run-loop work on the process main OS thread,
// so Open pins the calling goroutine with runtime.LockOSThread; the caller
// should therefore invoke Open+Run from main (as cmd/windowdemo does).
func Open(cfg Config) (Backend, error) {
	runtime.LockOSThread()
	return cocoa.New(cfg.Title, cfg.Width, cfg.Height, cfg.Theme)
}
