// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import "sync/atomic"

// The X11 half of the Repainter capability.
//
// The run loop is blocked on the next event, and only the server can end that
// wait — so a repaint asked for from another goroutine has to arrive as an
// event. SendEvent addressed to our own window, with an empty event-mask, is
// delivered by the server to us and nobody else. It is what Xlib applications
// have used to wake XNextEvent for thirty years, and it needs no second
// connection, no poll timeout and no thread affinity.
//
// Without this the reader shows its first frame for as long as the window stays
// idle: a fetch that completes while nobody is touching the mouse has no way to
// reach the screen.

// repaintAtomName is interned once per window. It is the message TYPE, which is
// how the loop tells our own wakeup from a window manager's ClientMessage
// without inspecting anything else.
const repaintAtomName = "_GO_WIDGETS_REPAINT"

// repaintState is the coalescing flag, held by the window.
//
// One wakeup can be in flight at a time. A caller repainting at 60 Hz against a
// loop busy with a slow frame would otherwise queue a message per tick and, on
// catching up, redraw once per queued message — spending the recovery on frames
// nobody will see. The flag is cleared when the loop takes the message, so the
// next tick after that queues a fresh one.
type repaintState struct{ pending atomic.Bool }

// Repaint asks the run loop for a frame. Implements the Repainter capability;
// safe to call from any goroutine, returns without waiting for the frame.
func (w *Window) Repaint() {
	if w.repaintAtom == 0 {
		return // the atom could not be interned at bring-up; see newWindow
	}
	if !w.repaint.pending.CompareAndSwap(false, true) {
		return // a wakeup is already on its way; a second would only queue a frame
	}
	// Writes on the connection are serialised, so this is safe from a goroutine
	// that is not the one in Run. A failure means the connection is going away,
	// which the loop is about to discover for itself.
	if err := w.conn.SendClientMessage(w.win, w.repaintAtom, 0); err != nil {
		w.repaint.pending.Store(false)
	}
}

// tookRepaint reports whether xe is our own wakeup, clearing the coalescing flag
// when it is. A window manager's ClientMessage is not ours and is left alone.
func (w *Window) tookRepaint(atom uint32, format byte) bool {
	if w.repaintAtom == 0 || format != 32 || atom != w.repaintAtom {
		return false
	}
	w.repaint.pending.Store(false)
	return true
}
