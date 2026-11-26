// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux && !android

package gtk

import (
	"github.com/go-gtk/gtk4"
	"github.com/go-widgets/toolkit"
)

// OpenChild opens a secondary top-level window on this window's GLib main loop:
// GTK already runs every window of the process on one loop, so the child needs
// no loop of its own, only its widget tree bound and its close handled
// separately from the parent's. root is bound and the window presented before it
// returns; onClose, when non-nil, is called once, on the GTK thread, when the
// child goes away.
//
// Call it from the GTK thread (a handler the loop runs). Closing the child does
// not end the parent's Run; closing the parent closes its children.
func (w *Window) OpenChild(title string, width, height int, theme *toolkit.Theme, scale float64, root toolkit.Widget, onClose func()) (*Window, error) {
	if w.closing.Load() {
		return nil, ErrClosed
	}
	if theme == nil {
		theme = w.theme
	}
	c, err := Open(title, width, height, theme, scale)
	if err != nil {
		return nil, err
	}
	c.child, c.parent, c.onClose = true, w, onClose
	w.children = append(w.children, c)
	c.present(root)
	return c, nil
}

// closeChild asks the child's window to close, from any goroutine. The user's
// close and this one meet in onCloseRequest, which is where the child is
// retired, so the two cannot disagree about it.
func (w *Window) closeChild() error {
	if w.closing.Load() {
		return nil
	}
	gtk4.IdleAdd(func() {
		if !w.closing.Load() {
			w.win.Close()
		}
	})
	return nil
}

// childGone retires a child whose window is closing: it leaves its parent's list
// and its owner is told, once.
func (w *Window) childGone() {
	if w.closing.Swap(true) {
		return
	}
	if p := w.parent; p != nil {
		for i, c := range p.children {
			if c == w {
				p.children = append(p.children[:i], p.children[i+1:]...)
				break
			}
		}
	}
	if f := w.onClose; f != nil {
		w.onClose = nil
		f()
	}
}

// closeChildren closes every secondary window this one owns, on the GTK thread.
func (w *Window) closeChildren() {
	for len(w.children) > 0 {
		c := w.children[0]
		c.childGone() // removes c from w.children
		c.win.Close()
	}
}
