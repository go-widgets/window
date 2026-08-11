// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package window

import (
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
	w, err := win32.New(cfg.Title, cfg.Width, cfg.Height, cfg.Theme)
	if err != nil {
		return nil, err
	}
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

var (
	_ Backend          = windowsBackend{}
	_ AppearanceReader = windowsBackend{}
	_ Clipboard        = (*win32.Window)(nil)
)
