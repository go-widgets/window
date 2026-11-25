// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux && !android

package gtk

import (
	"errors"

	"github.com/go-gtk/gtk4"
)

// ErrClosed is returned for a request made of a closed window. The parent
// package maps it to its own window.ErrClosed.
var ErrClosed = errors.New("gtk: the window is closed")

// SetHideOnClose makes the window's own close control hide the window instead of
// ending Run: the user's close is answered by taking the window off the screen,
// and [Window.Close] still ends it. Call it before Run.
func (w *Window) SetHideOnClose(v bool) { w.hideOnClose.Store(v) }

// onCloseRequest answers GTK's close-request signal. It returns true to stop the
// close (the window was hidden instead) and false to let GTK close the window,
// which is what ends Run.
func (w *Window) onCloseRequest() bool {
	if w.hideOnClose.Load() && !w.closing.Load() {
		w.win.SetVisible(false)
		return true
	}
	w.loop.Quit()
	return false
}

// Show makes a hidden window visible again. It does not ask for the focus.
// Safe from any goroutine: the call is handed to the GTK thread.
func (w *Window) Show() error {
	return w.onGTKThread(func() { w.win.SetVisible(true); w.dirty.Store(true) })
}

// Hide takes the window off the screen without ending Run.
func (w *Window) Hide() error {
	return w.onGTKThread(func() { w.win.SetVisible(false) })
}

// Raise shows the window if it is hidden and brings it to the front
// (gtk_window_present). The compositor may refuse the focus.
func (w *Window) Raise() error {
	return w.onGTKThread(func() { w.win.Present(); w.dirty.Store(true) })
}

// onGTKThread runs fn on the GTK main thread, and refuses once the window has
// been closed. It returns once the request is queued, not once it is carried out.
func (w *Window) onGTKThread(fn func()) error {
	if w.closing.Load() {
		return ErrClosed
	}
	gtk4.IdleAdd(func() {
		if !w.closing.Load() {
			fn()
		}
	})
	return nil
}
