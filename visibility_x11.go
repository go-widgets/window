// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

// The X11 window's Visibility capability.
//
// Every request is one-way, so it can go out on the caller's goroutine: the
// connection serialises writes, and nothing here reads, which is the side the
// run loop owns. The one thing that would need an answer -- the
// _NET_ACTIVE_WINDOW atom -- is interned at bring-up, the way the repaint atom
// is.

var _ Visibility = (*Window)(nil)

// root is the window's parent, the screen's root window, which is where a
// request for the window manager is sent.
func (w *Window) rootWindow() uint32 { return w.conn.Setup().Screens[0].Root }

// isClosed reads the closed flag under the lock Close sets it under.
func (w *Window) isClosed() bool {
	w.fbmu.Lock()
	defer w.fbmu.Unlock()
	return w.closed
}

// Show maps the window again.
func (w *Window) Show() error {
	if w.isClosed() {
		return ErrClosed
	}
	return w.conn.MapWindow(w.win)
}

// Hide withdraws the window (ICCCM 4.1.4): it leaves the screen and, under a
// window manager, the taskbar, rather than becoming an icon there.
func (w *Window) Hide() error {
	if w.isClosed() {
		return ErrClosed
	}
	return w.conn.Withdraw(w.rootWindow(), w.win)
}

// Raise maps the window, puts it on top of the stack, and asks the window
// manager to activate it. The stacking request is what a window manager without
// EWMH -- or no window manager at all -- still honours; _NET_ACTIVE_WINDOW is
// what brings a minimised window back and gives it the focus under one that
// speaks EWMH.
func (w *Window) Raise() error {
	if w.isClosed() {
		return ErrClosed
	}
	if err := w.conn.MapWindow(w.win); err != nil {
		return err
	}
	if err := w.conn.RaiseWindow(w.win); err != nil {
		return err
	}
	if w.netActiveAtom == 0 {
		return nil // the server would not intern it; the stacking request stands
	}
	return w.conn.RequestActivate(w.rootWindow(), w.win, w.netActiveAtom)
}
