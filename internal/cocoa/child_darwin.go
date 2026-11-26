// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package cocoa

import (
	"github.com/go-macos/objc"
	"github.com/go-widgets/toolkit"
)

// Secondary windows.
//
// An application has ONE -[NSApplication run]: the main window's Run is what
// drives it, and a second top-level window is simply another NSWindow on that
// same loop. Everything that makes one window special in this package is the
// `active` pointer the class callbacks used to read, so a second window needs
// exactly one thing: a way for a callback to find ITS window. AppKit hands every
// callback its receiver -- the view, the window or the delegate -- and the
// registry below maps each of the three back to the *Window, on the main thread
// where every one of them runs.

// registry maps a window's NSWindow, content view and delegate to the Go window.
// It is read and written on the main thread only, like every AppKit callback.
var registry = map[objc.ID]*Window{}

// makeWindow is how OpenChild creates the NSWindow; a variable so a test can make
// the creation fail, which a live AppKit does not do on demand.
var makeWindow = newWindow

func register(w *Window) {
	for _, id := range [...]objc.ID{w.win, w.view, w.deleg} {
		if id != 0 {
			registry[id] = w
		}
	}
}

func unregister(w *Window) {
	for _, id := range [...]objc.ID{w.win, w.view, w.deleg} {
		if id != 0 && registry[id] == w {
			delete(registry, id)
		}
	}
}

// lookup is the window an AppKit receiver belongs to, nil when it is none of ours.
func lookup(id objc.ID) *Window { return registry[id] }

// route is lookup for a callback that is about to act: it also makes the window
// the one the shared helpers (the input-method code, the key mapping) read as
// `active`, and falls back to whatever was active for a receiver it does not
// know -- which is every receiver of a one-window program, so nothing changes
// there.
func route(self objc.ID) *Window {
	if w := lookup(self); w != nil {
		active = w
		return w
	}
	return active
}

// OpenChild opens a secondary top-level window on this window's run loop. root
// is bound and presented before it returns; onClose, when non-nil, is called
// once, on the main thread, when the child goes away -- by the user's close
// control or by Close.
//
// It must be called on the main thread, from inside the loop (a handler, a
// menu action, a main-thread callback): AppKit is touched directly. Closing the
// child never ends the loop; closing this window closes its children first.
func (w *Window) OpenChild(o Options, root toolkit.Widget, onClose func()) (*Window, error) {
	if w == nil || w.closed {
		return nil, ErrClosed
	}
	c, err := makeWindow(o, true)
	if err != nil {
		return nil, err
	}
	c.parent = w
	c.onClose = onClose
	w.children = append(w.children, c)
	c.bindAndSeed(root)
	return c, nil
}

// childClosed is the child's goodbye: it leaves the routing table and its
// parent's list, and tells its owner. Idempotent, because the user's close and
// Close can both arrive.
func (w *Window) childClosed() {
	if w.closed {
		return
	}
	w.closed = true
	w.gone.Store(true)
	w.stopGPU()
	unregister(w)
	if p := w.parent; p != nil {
		for i, c := range p.children {
			if c == w {
				p.children = append(p.children[:i], p.children[i+1:]...)
				break
			}
		}
		if active == w {
			active = p
		}
	}
	if f := w.onClose; f != nil {
		w.onClose = nil
		f()
	}
}

// closeChildren closes every secondary window this one owns.
func (w *Window) closeChildren() {
	for len(w.children) > 0 {
		c := w.children[0]
		c.childClosed() // removes c from w.children
		if c.win != 0 {
			c.win.Send(selClose)
		}
	}
}
