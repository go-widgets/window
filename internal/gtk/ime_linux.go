// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux && !android

package gtk

import (
	"github.com/go-gtk/gtk4"
	"github.com/go-widgets/toolkit"
)

// installIME puts a GtkIMContext in front of the window's key controller, so a
// dead key, a compose sequence or an input method (ibus, fcitx, whatever the
// session selects) can compose text in the painted fields. GTK offers every key
// to the context first; what it turns into text arrives through "commit", the
// composition in progress through "preedit-changed", and only the keys it did not
// consume (named keys, shortcuts, releases) reach installInput's key callback.
// It returns false when GTK has no input-method support, and the window then
// takes plain keys as before.
func (w *Window) installIME() bool {
	im := gtk4.IMContextNew()
	if im == 0 {
		return false
	}
	w.im = im
	im.SetClientWidget(w.win)
	im.OnPreeditChanged(func() {
		text, cur := im.Preedit()
		if w.root != nil {
			setPreedit(w.root, preeditFrom(text, cur))
		}
		w.placeIME()
		w.dirty.Store(true)
	})
	im.OnCommit(func(text string) {
		if w.root != nil && !composeInto(w.root, text) {
			for _, ev := range commitFallback(text) {
				w.root.OnEvent(ev)
			}
		}
		w.placeIME()
		w.dirty.Store(true)
	})
	return true
}

// placeIME tells the input method where the caret is, so its candidate window
// opens beside the field being typed in.
func (w *Window) placeIME() {
	if w.im == 0 || w.root == nil {
		return
	}
	if _, r, ok := toolkit.FocusedComposition(w.root); ok {
		w.im.SetCursorLocation(caretRect(r, w.scale))
	}
}
