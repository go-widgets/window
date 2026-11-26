// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package win32

import (
	"github.com/go-mswin/win32"
	"github.com/go-widgets/toolkit"
)

// byHWND routes the WNDPROC to the window a message is for. UI thread only: it
// is written by window creation and WM_DESTROY, and read by the WNDPROC.
var byHWND = map[uintptr]*Window{}

// OpenChild opens a secondary top-level window on this window's thread and
// message loop. root is bound and the first frame painted before it returns;
// onClose, when non-nil, is called once, on the UI thread, when the child goes
// away. Call it on the UI thread (from a handler the loop runs). Closing the
// child does not end the parent's Run; closing the parent closes its children.
func (w *Window) OpenChild(title string, width, height int, theme *toolkit.Theme, native, gpu bool, root toolkit.Widget, onClose func()) (*Window, error) {
	if w.closed || w.hwnd == 0 {
		return nil, ErrClosed
	}
	if theme == nil {
		theme = w.theme
	}
	c, err := newWindow(title, width, height, theme, native, gpu, true)
	if err != nil {
		return nil, err
	}
	c.child, c.parent, c.onClose = true, w, onClose
	c.onScale = w.onScale // the metric scale is the process's, so a child reports to the same owner
	w.children = append(w.children, c)
	c.present(root)
	return c, nil
}

// childGone retires a child whose window was destroyed: it leaves its parent's
// list and its owner is told, once.
func (w *Window) childGone() {
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

// closeChildren destroys every secondary window this one owns, on the UI thread.
func (w *Window) closeChildren() {
	for len(w.children) > 0 {
		c := w.children[0]
		if c.hwnd == 0 {
			c.childGone() // already gone: just drop it
			continue
		}
		win32.DestroyWindow(win32.HWND(c.hwnd)) // WM_DESTROY calls childGone
		if len(w.children) > 0 && w.children[0] == c {
			c.childGone() // DestroyWindow refused; do not loop for ever
		}
	}
}
