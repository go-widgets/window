// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux && !android

package gtk

import (
	"github.com/go-gtk/gtk4"
	"github.com/go-widgets/toolkit"
)

// installInput attaches GTK event controllers to the window and turns each into a
// toolkit.Event delivered to the root, then asks for a repaint. It is what makes
// the DRAWN regions of a self-rendering Surface — everything that is not one of
// the overlaid native controls — clickable, scrollable and typeable; the native
// controls receive their own input from GTK directly and consume it before it
// reaches these window-level controllers.
func (w *Window) installInput() {
	w.win.OnMouseDown(func(button int, state uint, x, y float64) { w.pointer(button, state, x, y, true) })
	w.win.OnMouseUp(func(button int, state uint, x, y float64) { w.pointer(button, state, x, y, false) })
	w.win.OnMotion(func(state uint, x, y float64) {
		ev := toolkit.Event{Kind: toolkit.EventMouseMove, X: w.px(x), Y: w.px(y)}
		if w.btnDown {
			ev.Kind = toolkit.EventMouseDrag
		}
		w.deliver(applyMods(ev, state))
	})
	w.win.OnScroll(func(dx, dy float64, state uint) {
		w.deliver(applyMods(toolkit.Event{Kind: toolkit.EventScroll, Delta: rows(dy), DeltaX: rows(dx)}, state))
	})
	w.win.OnKey(func(keyval, keycode, state uint, press bool) {
		for _, ev := range mapKey(keyval, state, press) {
			w.deliver(ev)
		}
	})
}

// px scales a GTK logical point (the coordinate space of the window and its
// framebuffer picture) to a framebuffer device pixel, the space toolkit events
// and the drawn scene use.
func (w *Window) px(v float64) int { return int(v * w.scale) }

// pointer maps a button press/release: the primary (and any non-secondary) button
// is a click/mouse-up pair the drawn widgets hit-test; the secondary button is a
// context-menu press with no paired release. btnDown drives the drag-vs-move split
// in OnMotion.
func (w *Window) pointer(button int, state uint, x, y float64, down bool) {
	ev := toolkit.Event{X: w.px(x), Y: w.px(y)}
	switch {
	case down && button == 3:
		ev.Kind = toolkit.EventSecondaryClick
	case down:
		ev.Kind = toolkit.EventClick
		w.btnDown = true
	default:
		ev.Kind = toolkit.EventMouseUp
		w.btnDown = false
	}
	w.deliver(applyMods(ev, state))
}

// deliver dispatches one event to the root and marks the window dirty so the frame
// clock presents the frame the event produced. It runs on the GTK main thread, the
// same thread as the frame clock, so it needs no lock.
func (w *Window) deliver(ev toolkit.Event) {
	if w.root != nil {
		w.root.OnEvent(ev)
	}
	w.dirty.Store(true)
}

// applyMods stamps the four modifier flags the toolkit carries, decoded from the
// GDK modifier state.
func applyMods(ev toolkit.Event, state uint) toolkit.Event {
	ev.Shift = state&gtk4.ModShift != 0
	ev.Ctrl = state&gtk4.ModControl != 0
	ev.Alt = state&gtk4.ModAlt != 0
	ev.Meta = state&gtk4.ModSuper != 0
	return ev
}

// rows reduces a scroll delta to the toolkit's ±1-row step. GTK's scroll dy is
// positive scrolling down (toward the end of the content), which is the toolkit's
// positive Delta, so no inversion — unlike the Cocoa backend, which flips AppKit's
// natural-scroll delta.
func rows(d float64) int {
	switch {
	case d > 0:
		return 1
	case d < 0:
		return -1
	}
	return 0
}

// keyNames maps the GDK keyvals of the named keys to the toolkit's DOM-style
// names — the exact strings the Cocoa/X11 backends emit and the widgets match.
var keyNames = map[uint]string{
	0xff0d: "Enter", 0xff8d: "Enter", // Return, KP_Enter
	0xff08: "Backspace",
	0xff09: "Tab",
	0xffff: "Delete",
	0xff1b: "Escape",
	0xff50: "Home", 0xff57: "End",
	0xff55: "PageUp", 0xff56: "PageDown",
	0xff51: "ArrowLeft", 0xff53: "ArrowRight",
	0xff52: "ArrowUp", 0xff54: "ArrowDown",
}

// mapKey turns a GDK key event into the toolkit events it produces, mirroring the
// Cocoa/X11 split: a named key is a single EventKeyDown/EventKeyUp carrying the
// name; a printable key is EventKeyDown+EventChar on press and EventKeyUp on
// release; a non-printable unnamed key (a bare modifier, a function key) delivers
// nothing. gdk_keyval_to_unicode returns a CONTROL rune for named keys like
// Return, so the name table is consulted first and the rune path keeps only
// printable runes.
func mapKey(keyval, state uint, press bool) []toolkit.Event {
	if name, ok := keyNames[keyval]; ok {
		kind := toolkit.EventKeyDown
		if !press {
			kind = toolkit.EventKeyUp
		}
		return []toolkit.Event{applyMods(toolkit.Event{Kind: kind, Code: name}, state)}
	}
	r := gtk4.KeyvalToUnicode(keyval)
	if r < 0x20 || r == 0x7f {
		return nil
	}
	s := string(r)
	if press {
		return []toolkit.Event{
			applyMods(toolkit.Event{Kind: toolkit.EventKeyDown, Code: s}, state),
			applyMods(toolkit.Event{Kind: toolkit.EventChar, Code: s}, state),
		}
	}
	return []toolkit.Event{applyMods(toolkit.Event{Kind: toolkit.EventKeyUp, Code: s}, state)}
}
