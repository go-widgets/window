// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package window

import (
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
	return win32.New(cfg.Title, cfg.Width, cfg.Height, cfg.Theme)
}
