// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package window

import (
	"errors"
	"image/color"
	"runtime"

	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/tray"
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
	//
	// Config.GPU is opt-in, as on macOS: gpu2d's Direct3D 11 renderer draws the
	// frame and a flip-model swap chain on the window presents it, falling back
	// to this CPU path on any failure (GPU and GPUError say which happened).
	w, err := win32.NewScaledGPU(cfg.Title, cfg.Width, cfg.Height, cfg.Theme, cfg.RenderScale == NativeScale, cfg.GPU)
	if err != nil {
		return nil, err
	}
	w.SetHideOnClose(cfg.HideOnClose)
	applyMetricScale(cfg.RenderScale, w.RenderScale())
	// And again when the window is dragged to a monitor of another DPI: the tree
	// lays out in the native framebuffer's pixels (see Window.SetOnScale).
	w.SetOnScale(func(s float64) { applyMetricScale(cfg.RenderScale, s) })
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

// OpenFile, SaveFile and Alert implement Dialogs over the back-end's own queue
// (internal/win32/dialog_windows.go); dialog_win32.go maps the options and the
// results.
func (b windowsBackend) OpenFile(o OpenOptions, done func(OpenResult)) error {
	return win32Err(b.Window.Dialog(win32OpenRequest(o, done)))
}

func (b windowsBackend) SaveFile(o SaveOptions, done func(SaveResult)) error {
	return win32Err(b.Window.Dialog(win32SaveRequest(o, done)))
}

func (b windowsBackend) Alert(o AlertOptions, done func(AlertResult)) error {
	return win32Err(b.Window.Dialog(win32AlertRequest(o, done)))
}

// SetMenuBar implements MenuBar. Windows has no application menu to name, so
// appName is not used.
func (b windowsBackend) SetMenuBar(_ string, m *tray.Menu) error {
	return win32Err(b.Window.SetMenuBar(m))
}

// windowsChild is a secondary Win32 window wearing this package's vocabulary.
type windowsChild struct{ *win32.Window }

// OpenChild implements [Windows]: a second HWND on the thread and message loop
// this one runs. Call it from a handler the loop runs.
func (b windowsBackend) OpenChild(cfg Config, root toolkit.Widget, onClose func()) (Child, error) {
	c, err := b.Window.OpenChild(cfg.Title, cfg.Width, cfg.Height, cfg.Theme, cfg.RenderScale == NativeScale, cfg.GPU, root, onClose)
	if err != nil {
		return nil, win32Err(err)
	}
	return windowsChild{c}, nil
}

func (c windowsChild) Close() error { return win32Err(c.Window.Close()) }
func (c windowsChild) Raise() error { return win32Err(c.Window.Raise()) }

func win32Err(err error) error {
	if errors.Is(err, win32.ErrClosed) {
		return ErrClosed
	}
	return err
}

var (
	_ MenuBar          = windowsBackend{}
	_ Visibility       = windowsBackend{}
	_ Dialogs          = windowsBackend{}
	_ Windows          = windowsBackend{}
	_ Child            = windowsChild{}
	_ Backend          = windowsBackend{}
	_ AppearanceReader = windowsBackend{}
	_ Clipboard        = (*win32.Window)(nil)
	_ Repainter        = (*win32.Window)(nil)
	_ Scaler           = (*win32.Window)(nil)
)
