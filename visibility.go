// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"errors"
	"fmt"
)

// ErrNotSupported is what a back-end answers for something its platform will
// not let it do. It is wrapped with the reason, so errors.Is finds it and the
// message still says why.
var ErrNotSupported = errors.New("window: not supported by this back-end")

// ErrClosed is returned by a request made of a window that has been closed.
var ErrClosed = errors.New("window: the window is closed")

// Visibility is an optional [Backend] capability: take an open window off the
// screen without closing it, put it back, and bring it to the front.
//
// It is what an application with a system-tray icon needs. Its "Open" item
// raises the window that is already there rather than opening a second one,
// and its window can be hidden while the application carries on in the tray.
// Hiding is not closing: [Backend.Run] keeps running, the widget tree keeps its
// state, and Show puts back exactly what was there.
//
// All three are safe from any goroutine -- a tray's menu runs on its own -- and
// return once the request is on its way to the window's own thread, without
// waiting for the platform to act on it.
//
//   - Show makes a hidden window visible again. It does not ask for the focus,
//     and does nothing to a window that is already visible.
//   - Hide takes the window off the screen, and out of the taskbar or Dock
//     where the platform lists windows there.
//   - Raise shows the window if it is hidden, restores it if it is minimised,
//     brings it to the front and asks for the keyboard focus. The platform may
//     refuse the focus -- focus-stealing prevention is a desktop's prerogative
//     -- and Raise does not report that, because nothing tells it.
//
// Implemented by the X11, macOS (Cocoa) and Windows (Win32) back-ends. On
// Wayland, Show and Hide are implemented and Raise answers [ErrNotSupported]:
// xdg-shell gives a client no request to raise or focus its own window. The
// wasmbox, Android and GTK back-ends do not implement it; [Show], [Hide] and
// [Raise] answer ErrNotSupported for them.
type Visibility interface {
	Show() error
	Hide() error
	Raise() error
}

// Show makes b's window visible again; see [Visibility]. A back-end without
// the capability answers [ErrNotSupported].
func Show(b Backend) error {
	if v, ok := b.(Visibility); ok {
		return v.Show()
	}
	return noVisibility(b, "show")
}

// Hide takes b's window off the screen without closing it; see [Visibility].
// A back-end without the capability answers [ErrNotSupported].
func Hide(b Backend) error {
	if v, ok := b.(Visibility); ok {
		return v.Hide()
	}
	return noVisibility(b, "hide")
}

// Raise brings b's window to the front and asks for the focus; see
// [Visibility]. A back-end without the capability answers [ErrNotSupported].
func Raise(b Backend) error {
	if v, ok := b.(Visibility); ok {
		return v.Raise()
	}
	return noVisibility(b, "raise")
}

func noVisibility(b Backend, what string) error {
	return fmt.Errorf("%w: %v cannot %s its window", ErrNotSupported, b, what)
}
