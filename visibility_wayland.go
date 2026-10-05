// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"fmt"
	"sync/atomic"
)

// The Wayland window's Visibility capability.
//
// xdg-shell has no "hide" request; a client UNMAPS its toplevel by attaching
// no buffer and committing, and maps it again the way it mapped it the first
// time: a commit with no buffer, the compositor's configure, the ack, and only
// then a buffer. The toplevel and its xdg_surface stay alive in between, so the
// title, the app id and the input focus history the compositor keeps are not
// lost.
//
// It also has no request to raise or focus a toplevel, deliberately: the
// compositor decides what is in front. The protocol for it, xdg-activation,
// needs a token minted from the user's own recent input to this client, which
// a tray click (input to the PANEL, a different client) does not give. So
// Raise says so rather than pretend.

var _ Visibility = (*wlWindow)(nil)

// visibility requests, as handed from the caller's goroutine to the loop's.
const (
	visNone int32 = iota
	visShow
	visHide
)

// visReq is the latest Show or Hide not yet applied by the run loop; the last
// one asked for wins, which is what a person clicking twice means.
type visReq struct{ want atomic.Int32 }

// Show maps the window again. It returns once the loop has been woken; the
// window reappears when the compositor configures it.
func (w *wlWindow) Show() error { return w.requestVisibility(visShow) }

// Hide unmaps the window. Run keeps running.
func (w *wlWindow) Hide() error { return w.requestVisibility(visHide) }

// Raise is not something a Wayland client can do; see the comment above.
func (w *wlWindow) Raise() error {
	return fmt.Errorf("%w: xdg-shell has no request to raise or focus a toplevel, "+
		"and xdg-activation needs a token from the user's input to this window", ErrNotSupported)
}

func (w *wlWindow) requestVisibility(v int32) error {
	w.fbmu.Lock()
	closed := w.closed
	w.fbmu.Unlock()
	if closed {
		return ErrClosed
	}
	w.vis.want.Store(v)
	return w.conn.Wake()
}

// applyVisibility runs on the loop's goroutine, between dispatches: it is the
// only place the surface is committed from.
func (w *wlWindow) applyVisibility() error {
	switch w.vis.want.Swap(visNone) {
	case visHide:
		if w.hidden {
			return nil
		}
		w.fbmu.Lock()
		defer w.fbmu.Unlock()
		if err := w.surface.Attach(nil, 0, 0); err != nil {
			return err
		}
		if err := w.surface.Commit(); err != nil {
			return err
		}
		// Unmapped is unconfigured: nothing may be attached until the next
		// configure, which is exactly what present already waits for.
		w.hidden, w.configured, w.remap = true, false, false
	case visShow:
		if !w.hidden {
			return nil
		}
		w.hidden, w.remap = false, true
		// The initial commit again, with no buffer: it asks for a configure.
		return w.surface.Commit()
	}
	return nil
}
