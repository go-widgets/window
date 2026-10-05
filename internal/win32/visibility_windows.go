// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package win32

import (
	"errors"

	"github.com/go-mswin/win32"
)

// ErrClosed is returned for a request made of a closed window. The parent
// package maps it to its own window.ErrClosed.
var ErrClosed = errors.New("win32: the window is closed")

// Show, Hide and Raise implement the window.Visibility capability. Safe from
// any goroutine: like Repaint and Close they POST to the window's own thread,
// the only one allowed to change it, and return without waiting.
func (w *Window) Show() error  { return w.postVisibility(OpShow) }
func (w *Window) Hide() error  { return w.postVisibility(OpHide) }
func (w *Window) Raise() error { return w.postVisibility(OpRaise) }

func (w *Window) postVisibility(op VisibilityOp) error {
	hwnd := w.hwnd
	if w.gone.Load() || hwnd == 0 {
		return ErrClosed
	}
	if !win32.PostMessage(win32.HWND(hwnd), WMAppVisibility, win32.WPARAM(op), 0) {
		// The queue is gone: the window is being destroyed.
		return ErrClosed
	}
	return nil
}

// onVisibility is the UI-thread half: WMAppVisibility's handler.
func (w *Window) onVisibility(op VisibilityOp) {
	cmd, foreground, ok := VisibilityPlan(op, win32.IsIconic(win32.HWND(w.hwnd)))
	if !ok {
		return
	}
	win32.ShowWindow(win32.HWND(w.hwnd), cmd)
	if foreground {
		win32.SetForegroundWindow(win32.HWND(w.hwnd))
	}
}
