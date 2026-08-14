// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

// The Wayland half of the Repainter capability.
//
// A Wayland client cannot send itself an event: everything on the connection
// comes from the compositor, and no request asks for a message back. So where
// X11 satisfies the wait, this one interrupts it — a read deadline in the past,
// which makes the blocked read return at once and the run loop come round to
// paint a frame. The mechanics, including why nothing buffered is lost, are in
// internal/wayland's wake.go.

// Repaint asks the run loop for a frame. Implements the Repainter capability;
// safe to call from any goroutine, returns without waiting for the frame.
//
// A failure to arm the wakeup is a connection that is going away, which the run
// loop is about to discover for itself — there is nothing useful to tell a
// caller that only asked for a frame.
func (w *wlWindow) Repaint() { _ = w.conn.Wake() }
