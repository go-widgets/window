// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package window

import (
	"errors"
	"image/color"
	"runtime"

	"github.com/go-widgets/window/internal/win32"
)

// Open returns the native Windows (Win32/GDI) backend: it declares
// Per-Monitor-V2 DPI awareness, registers a window class and creates a real
// titled, resizable top-level HWND, ready for Run to present the go-widgets
// framebuffer (packed BGRA and blitted with StretchDIBits) and route WM_* mouse/
// wheel/key input. The backend is pure-Go (CGO-free) via the Win32 syscall path
// (syscall.NewLazyDLL + a syscall.NewCallback WNDPROC over the process'
// user32/gdi32/kernel32 DLLs — no cgo), so a go-widgets app runs through
// Open→Run on Windows exactly as it does on X11/Wayland (open_linux.go), macOS
// (open_darwin.go) and inside wasmdesk (open_js.go).
//
// Win32 requires the message loop and all window work on one OS thread, so Open
// pins the calling goroutine with runtime.LockOSThread; the caller should invoke
// Open+Run from main (as cmd/windowdemo does), matching the Cocoa backend.
func Open(cfg Config) (Backend, error) {
	runtime.LockOSThread()
	// NativeScale asks for the panel's own pixels rather than a logical
	// framebuffer the OS up-samples. Opt-in, as on every other back-end: it
	// changes what Size reports and what a point is worth to the widget tree.
	w, err := win32.NewScaled(cfg.Title, cfg.Width, cfg.Height, cfg.Theme, cfg.RenderScale == NativeScale)
	if err != nil {
		return nil, err
	}
	applyMetricScale(cfg.RenderScale, w.RenderScale())
	return windowsBackend{w}, nil
}

// windowsBackend is the Win32 window wearing this package's public vocabulary.
// The back-end lives in an internal package that THIS one imports, so it cannot
// name Appearance without a cycle; it reports primitive values and the wrapper
// puts them in the public shape. Everything else -- Run, Close, Size, String,
// the clipboard -- is promoted from the embedded window unchanged.
type windowsBackend struct{ *win32.Window }

// Appearance implements AppearanceReader over the back-end's raw reading.
func (b windowsBackend) Appearance() Appearance {
	dark, r, g, bl, has := b.Window.AppearanceRaw()
	return Appearance{
		Dark:      dark,
		Accent:    color.RGBA{R: r, G: g, B: bl, A: 255},
		HasAccent: has,
	}
}

// Show, Hide and Raise implement Visibility over the back-end's own, putting
// its closed-window error in this package's vocabulary.
func (b windowsBackend) Show() error  { return win32Err(b.Window.Show()) }
func (b windowsBackend) Hide() error  { return win32Err(b.Window.Hide()) }
func (b windowsBackend) Raise() error { return win32Err(b.Window.Raise()) }

func win32Err(err error) error {
	if errors.Is(err, win32.ErrClosed) {
		return ErrClosed
	}
	return err
}

var (
	_ Visibility       = windowsBackend{}
	_ Backend          = windowsBackend{}
	_ AppearanceReader = windowsBackend{}
	_ Clipboard        = (*win32.Window)(nil)
	_ Repainter        = (*win32.Window)(nil)
	_ Scaler           = (*win32.Window)(nil)
)
