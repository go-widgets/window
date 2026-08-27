// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package window

import (
	"image/color"
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
	opts := cocoa.Options{
		Title:       cfg.Title,
		Width:       cfg.Width,
		Height:      cfg.Height,
		Theme:       cfg.Theme,
		RenderScale: cfg.RenderScale,
		Fullscreen:  cfg.Fullscreen,
		Immersive:   cfg.Immersive,
		FixedSize:   cfg.FixedSize,
	}
	if cfg.Screen != nil {
		s := cfg.Screen.toCocoa()
		opts.Screen = &s
	}
	w, err := cocoa.NewWithOptions(opts)
	if err != nil {
		return nil, err
	}
	applyMetricScale(cfg.RenderScale, w.RenderScale())
	return darwinBackend{w}, nil
}

// darwinBackend is the Cocoa window wearing this package's public vocabulary.
// The back-end lives in an internal package that THIS one imports, so it cannot
// name Appearance without a cycle; it reports primitive values and the wrapper
// puts them in the public shape. Everything else -- Run, Close, Size, String,
// the clipboard -- is promoted from the embedded window unchanged.
type darwinBackend struct{ *cocoa.Window }

// Appearance implements AppearanceReader over the back-end's raw reading.
func (b darwinBackend) Appearance() Appearance {
	dark, r, g, bl, has := b.Window.AppearanceRaw()
	return Appearance{
		Dark:      dark,
		Accent:    color.RGBA{R: r, G: g, B: bl, A: 255},
		HasAccent: has,
	}
}

// The Cocoa back-end carries the OS pasteboard. Asserting it here, where the
// capability is declared, is what stops a rename inside the back-end from
// silently turning `w.(window.Clipboard)` into a failed assertion and an app
// back on the in-process clipboard with nothing to show for it.
var (
	_ Clipboard        = (*cocoa.Window)(nil)
	_ AppearanceReader = darwinBackend{}
	_ Scaler           = (*cocoa.Window)(nil)
	_ Repainter        = (*cocoa.Window)(nil)
	_ Placement        = (*cocoa.Window)(nil)
	_ Backend          = darwinBackend{}
)
