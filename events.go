// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/window/internal/x11"
)

// outcome is the result of translating one X11 event: the toolkit events to
// deliver to the root widget, plus lifecycle flags for the run loop.
type outcome struct {
	events  []toolkit.Event
	repaint bool
	resize  bool
	rw, rh  int
	quit    bool
}

// mapEvent translates a decoded X11 event into an outcome. It is pure with
// respect to I/O — it reads only the window's keymap, size and WM atoms —
// so the whole input-routing table is unit-testable with no X server.
//
// Modifier state (Ctrl/Shift) and drag-vs-move are derived from the event's
// own state mask rather than tracked across events, which keeps the mapping
// stateless and correct even if an event is missed.
func (w *Window) mapEvent(xe x11.Event) outcome {
	shift := xe.State&x11.ModShift != 0
	ctrl := xe.State&x11.ModControl != 0

	switch xe.Code {
	case xcodeKeyPress:
		return w.mapKey(xe, shift, ctrl, true)
	case xcodeKeyRelease:
		return w.mapKey(xe, shift, ctrl, false)
	case xcodeButtonPress:
		return w.mapButtonPress(xe, shift, ctrl)
	case xcodeButtonRelease:
		return w.mapButtonRelease(xe, shift, ctrl)
	case xcodeMotionNotify:
		return w.mapMotion(xe, shift, ctrl)
	case xcodeConfigureNotify:
		if int(xe.Width) != w.w || int(xe.Height) != w.h {
			return outcome{resize: true, rw: int(xe.Width), rh: int(xe.Height)}
		}
		return outcome{}
	case xcodeExpose:
		// Repaint once the final expose rectangle in a burst arrives.
		return outcome{repaint: true}
	case xcodeClientMessage:
		if xe.Format == 32 && xe.Atom == w.wmProtocols && xe.Data32 == w.wmDeleteWindow {
			return outcome{quit: true}
		}
		return outcome{}
	default:
		return outcome{}
	}
}

// mapKey turns a KeyPress/KeyRelease into key/char toolkit events. A named
// key (Enter, ArrowLeft, ...) yields a single KeyDown/KeyUp carrying that
// name; a printable key yields KeyDown+Char on press (Char being the
// committed rune) and KeyUp on release. Modifier keys deliver nothing.
func (w *Window) mapKey(xe x11.Event, shift, ctrl, press bool) outcome {
	ks := w.keymap.Keysym(xe.Detail, shift)
	if x11.IsModifier(ks) {
		return outcome{}
	}
	down, up := toolkit.EventKeyDown, toolkit.EventKeyUp
	out := outcome{repaint: true}
	if name := x11.KeysymName(ks); name != "" {
		kind := down
		if !press {
			kind = up
		}
		out.events = append(out.events, toolkit.Event{Kind: kind, Code: name, Ctrl: ctrl, Shift: shift})
		return out
	}
	if r, ok := x11.KeysymRune(ks); ok {
		s := string(r)
		if press {
			out.events = append(out.events,
				toolkit.Event{Kind: toolkit.EventKeyDown, Code: s, Ctrl: ctrl, Shift: shift},
				toolkit.Event{Kind: toolkit.EventChar, Code: s, Ctrl: ctrl, Shift: shift})
		} else {
			out.events = append(out.events, toolkit.Event{Kind: toolkit.EventKeyUp, Code: s, Ctrl: ctrl, Shift: shift})
		}
		return out
	}
	// Unmapped keysym: nothing to deliver, but a repaint is harmless.
	out.repaint = false
	return out
}

// mapButtonPress turns a ButtonPress into a click (buttons 1–3) or a scroll
// tick (wheel buttons 4/5).
func (w *Window) mapButtonPress(xe x11.Event, shift, ctrl bool) outcome {
	x, y := int(xe.EventX), int(xe.EventY)
	switch xe.Detail {
	case x11.ButtonWheelUp:
		return outcome{repaint: true, events: []toolkit.Event{{Kind: toolkit.EventScroll, X: x, Y: y, Delta: -1, Ctrl: ctrl, Shift: shift}}}
	case x11.ButtonWheelDown:
		return outcome{repaint: true, events: []toolkit.Event{{Kind: toolkit.EventScroll, X: x, Y: y, Delta: 1, Ctrl: ctrl, Shift: shift}}}
	default:
		return outcome{repaint: true, events: []toolkit.Event{{Kind: toolkit.EventClick, X: x, Y: y, Ctrl: ctrl, Shift: shift}}}
	}
}

// mapButtonRelease turns a ButtonRelease (buttons 1–3) into a mouse-up.
// Wheel-button releases carry no toolkit meaning.
func (w *Window) mapButtonRelease(xe x11.Event, shift, ctrl bool) outcome {
	if xe.Detail == x11.ButtonWheelUp || xe.Detail == x11.ButtonWheelDown {
		return outcome{}
	}
	return outcome{repaint: true, events: []toolkit.Event{{
		Kind: toolkit.EventMouseUp, X: int(xe.EventX), Y: int(xe.EventY), Ctrl: ctrl, Shift: shift,
	}}}
}

// mapMotion turns a MotionNotify into a drag (a button held) or a plain
// hover move (no button), per the event's state mask.
func (w *Window) mapMotion(xe x11.Event, shift, ctrl bool) outcome {
	buttons := x11.ModButton1 | x11.ModButton2 | x11.ModButton3
	kind := toolkit.EventMouseMove
	if xe.State&uint16(buttons) != 0 {
		kind = toolkit.EventMouseDrag
	}
	return outcome{repaint: true, events: []toolkit.Event{{
		Kind: kind, X: int(xe.EventX), Y: int(xe.EventY), Ctrl: ctrl, Shift: shift,
	}}}
}

// X11 event codes re-exported at the window layer so mapEvent reads
// clearly. They mirror the internal/x11 wire codes.
const (
	xcodeKeyPress        = 2
	xcodeKeyRelease      = 3
	xcodeButtonPress     = 4
	xcodeButtonRelease   = 5
	xcodeMotionNotify    = 6
	xcodeExpose          = 12
	xcodeConfigureNotify = 22
	xcodeClientMessage   = 33
)
