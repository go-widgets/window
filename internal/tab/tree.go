// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// Package tab runs a widget tree in a plain browser tab, on a <canvas>,
// through go-widgets/webcanvas: no compositor, no SharedArrayBuffer, no
// cross-origin isolation. It is what window.Open returns on js/wasm when the
// page is not a wasmdesk/wasmbox client, so ONE application runs unchanged
// natively, inside wasmdesk, and in an ordinary tab served from anywhere.
//
// Tree, the adapter between a widget tree and webcanvas's App, has no build
// tag: it is plain Go, tested off the browser. Only tab_js.go touches the DOM.
package tab

import (
	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/webcanvas"
	"github.com/go-widgets/window/internal/dnd"
)

// damageRenderer is the opt-in incremental repaint, as the other back-ends
// declare it (it mirrors window.DamageRenderer).
type damageRenderer interface {
	RenderDamaged(p painter.Painter, th *toolkit.Theme) []toolkit.Rect
}

// Tree is a widget tree as a webcanvas scene.
type Tree struct {
	root       toolkit.Widget
	dmg        damageRenderer
	theme      *toolkit.Theme
	w, h       int
	dnd        *dnd.Controller
	buttonHeld bool
	mods       webcanvas.Modifiers
}

var (
	_ webcanvas.App           = (*Tree)(nil)
	_ webcanvas.Resizer       = (*Tree)(nil)
	_ webcanvas.Scroller      = (*Tree)(nil)
	_ webcanvas.ModifierAware = (*Tree)(nil)
)

// NewTree adapts root, laid out on a w×h surface painted with theme.
func NewTree(root toolkit.Widget, w, h int, theme *toolkit.Theme) *Tree {
	if theme == nil {
		theme = toolkit.DefaultDark()
	}
	t := &Tree{root: root, theme: theme, w: w, h: h, dnd: dnd.New()}
	t.dmg, _ = root.(damageRenderer)
	t.dnd.Bind(root)
	return t
}

// Size is the surface in pixels.
func (t *Tree) Size() (int, int) { return t.w, t.h }

// Draw paints the tree into buf, the w×h RGBA framebuffer webcanvas keeps
// between frames -- which is what lets an incremental root repaint only what
// it reports damaged.
func (t *Tree) Draw(buf []byte) {
	p := painter.NewPixelPainter(buf, t.w, t.h)
	full := toolkit.Rect{X: 0, Y: 0, W: t.w, H: t.h}
	t.root.SetBounds(full)
	if t.dmg != nil {
		t.dmg.RenderDamaged(p, t.theme)
		return
	}
	p.FillRect(full, t.theme.Background)
	t.root.Draw(p, t.theme)
}

// Modifiers records the modifier keys of the event about to be delivered.
func (t *Tree) Modifiers(m webcanvas.Modifiers) { t.mods = m }

// event is a toolkit event of kind at (x, y), carrying the current modifiers.
func (t *Tree) event(kind toolkit.EventKind, x, y int) toolkit.Event {
	return toolkit.Event{Kind: kind, X: x, Y: y,
		Ctrl: t.mods.Ctrl, Shift: t.mods.Shift, Alt: t.mods.Alt, Meta: t.mods.Meta}
}

// deliver passes ev through drag-and-drop to the tree. Every event repaints:
// the tree does not say whether it changed, and a hover is a change.
func (t *Tree) deliver(ev toolkit.Event) bool {
	for _, e := range t.dnd.Process(ev) {
		t.root.OnEvent(e)
	}
	return true
}

// Click is a left-button press.
func (t *Tree) Click(x, y int) bool {
	t.buttonHeld = true
	return t.deliver(t.event(toolkit.EventClick, x, y))
}

// Move is a pointer move: a drag while the button is held.
func (t *Tree) Move(x, y int) bool {
	kind := toolkit.EventMouseMove
	if t.buttonHeld {
		kind = toolkit.EventMouseDrag
	}
	return t.deliver(t.event(kind, x, y))
}

// Release ends a press.
func (t *Tree) Release(x, y int) bool {
	t.buttonHeld = false
	return t.deliver(t.event(toolkit.EventMouseUp, x, y))
}

// Context is a right-button press.
func (t *Tree) Context(x, y int) bool {
	return t.deliver(t.event(toolkit.EventSecondaryClick, x, y))
}

// Char is a printable character typed with no Ctrl, Meta or Alt.
func (t *Tree) Char(s string) bool {
	ev := t.event(toolkit.EventChar, 0, 0)
	ev.Code = s
	return t.deliver(ev)
}

// KeyDown is a named key, or a modified one -- whose modifiers Modifiers has
// just recorded, so Ctrl+A arrives as "a" with Ctrl set.
func (t *Tree) KeyDown(s string) bool {
	ev := t.event(toolkit.EventKeyDown, 0, 0)
	ev.Code = s
	return t.deliver(ev)
}

// Scroll is the wheel, in toolkit rows, at the pointer.
func (t *Tree) Scroll(x, y, dx, dy int) bool {
	ev := t.event(toolkit.EventScroll, x, y)
	ev.Delta, ev.DeltaX = dy, dx
	return t.deliver(ev)
}

// Resize follows the canvas to the size the page lays it out at.
func (t *Tree) Resize(w, h int) (int, int) {
	if w > 0 && h > 0 {
		t.w, t.h = w, h
	}
	return t.w, t.h
}
