// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"fmt"

	"github.com/go-widgets/toolkit"
)

// Windows is the optional capability of opening SECONDARY top-level windows: a
// second window, with its own widget tree, on the loop the first one already
// runs.
//
// [Open] gives one window and [Backend.Run] blocks driving it, so an
// application that is already inside Run cannot open another by calling Open
// again -- that would be a second loop on a thread that has one. OpenChild
// instead creates the window ON the parent's loop: the parent presents it and
// feeds it input, and no second Run exists. It is what a "+" button that opens
// an add-an-item form in its own window needs.
//
// OpenChild binds root and presents the first frame before it returns, and
// must be called from the parent's UI thread -- from a widget's event handler,
// a menu action, or anything else the parent's loop calls -- because the
// platform toolkit is touched directly. onClose, when non-nil, is called once on
// that thread when the child goes away, whether the user closed it or [Child.Close]
// did. Closing a child never ends the parent's Run; closing the parent closes
// its children.
//
// Implemented by the macOS (Cocoa), GTK4 and Windows (Win32) back-ends. X11,
// Wayland, wasmbox and Android do NOT implement it, and [OpenChild] answers [ErrNotSupported] for
// them -- there is no stand-in that pretends.
type Windows interface {
	OpenChild(cfg Config, root toolkit.Widget, onClose func()) (Child, error)
}

// Child is a secondary window opened by [OpenChild]. It has no Run: the
// parent's loop drives it. All methods are safe from any goroutine and return
// once the request is on its way to the window's own thread.
type Child interface {
	// Close closes the window. It is safe to call more than once, and after the
	// user has closed it.
	Close() error
	// Raise brings the window to the front and asks for the keyboard focus.
	Raise() error
	// Size is the current client size, in the same unit as [Backend.Size].
	Size() (int, int)
	// String identifies the window for debugging.
	String() string
}

// OpenChild opens a secondary window on parent's run loop; see [Windows]. A
// back-end without the capability answers [ErrNotSupported], naming itself.
func OpenChild(parent Backend, cfg Config, root toolkit.Widget, onClose func()) (Child, error) {
	if w, ok := parent.(Windows); ok {
		return w.OpenChild(cfg, root, onClose)
	}
	return nil, fmt.Errorf("%w: %v cannot open a second window", ErrNotSupported, parent)
}
