// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// Package cocoa is the pure-Go (CGO-free, via purego) macOS AppKit windowing
// backend for the go-widgets toolkit. It opens a real NSWindow with a content
// NSView, blits the toolkit's RGBA framebuffer into it through an
// NSBitmapImageRep in -drawRect:, and routes native NSEvent mouse/scroll/key
// input into toolkit.Event, so a go-widgets widget tree runs on a macOS desktop
// exactly as it does on X11, Wayland or in the browser/wasm host.
//
// The Objective-C runtime is reached through the fleet's shared bridge
// github.com/go-macos/objc (Send/RegisterClass/GetClass/NSString/GoString/…),
// itself layered over github.com/ebitengine/purego — no cgo — so the whole
// module builds and links with CGO_ENABLED=0.
//
// This file is the SOVEREIGN, OS-INDEPENDENT half: the NSEvent→toolkit.Event
// mapping (key decode, modifier decode, button/wheel mapping), the flipped-view
// coordinate maths and the damage-rect→dirty-rect conversion, all expressed
// over plain Go values with a single toolkit dependency (the event model). It
// carries NO objc/purego/unsafe dependency, so it builds — and is unit-tested to
// 100% — on every GOOS, mirroring internal/wasmbox's protocol.go. The thin
// AppKit glue that actually creates the NSWindow, presents the bitmap and pumps
// the run loop lives in cocoa_darwin.go (//go:build darwin) and drives
// everything here.
package cocoa

import "github.com/go-widgets/toolkit"

// NSEventModifierFlags bits used by the backend. AppKit reports device-
// independent modifier state in the high bits of the flags mask.
const (
	modShift   = 1 << 17 // NSEventModifierFlagShift
	modControl = 1 << 18 // NSEventModifierFlagControl
	modOption  = 1 << 19 // NSEventModifierFlagOption
	modCommand = 1 << 20 // NSEventModifierFlagCommand
)

// macOS virtual key codes (ANSI layout) for the named editing/navigation keys.
// Every other key is treated as a printable character, decoded from the
// event's -charactersIgnoringModifiers string.
const (
	keyReturn      = 36
	keyTab         = 48
	keyDelete      = 51 // Backspace (the ⌫ key)
	keyEscape      = 53
	keyKeypadEnter = 76
	keyForwardDel  = 117 // ⌦
	keyHome        = 115
	keyPageUp      = 116
	keyEnd         = 119
	keyPageDown    = 121
	keyLeft        = 123
	keyRight       = 124
	keyDownArrow   = 125
	keyUpArrow     = 126
)

// Default window sizing. When a caller opens a window without an explicit size
// the backend picks a readable default from the main screen's visible frame: a
// fraction of the usable area, clamped to a comfortable band on each axis and
// never larger than the screen itself. All values are LOGICAL points (the unit
// the toolkit lays out and the user reads in), never device pixels.
const (
	// defaultFallbackW/H is used when the screen size is unknown (no AppKit
	// screen, e.g. a headless build) — a plainly readable desktop-window size.
	defaultFallbackW = 1280
	defaultFallbackH = 800
	// defaultScreenFraction is the share of the visible frame a defaulted window
	// occupies on each axis.
	defaultScreenFraction = 0.85
	// The clamp band keeps the default comfortably readable on a small display
	// yet never sprawling on a very large one.
	minContentW = 960
	minContentH = 600
	maxContentW = 1600
	maxContentH = 1000
)

// DefaultContentSize picks a readable default window content size, in LOGICAL
// points, from the main screen's visible frame (visW×visH, also in points). It
// takes defaultScreenFraction of the visible area and clamps each axis to the
// [min,max] readability band, then to the visible extent so the window never
// exceeds the usable screen. When the screen size is unknown (visW or visH ≤ 0)
// it returns the fixed fallback. The result is always ≥ 1×1 and ≤ the visible
// frame, so a defaulted window is legible without manual sizing.
func DefaultContentSize(visW, visH float64) (w, h int) {
	if visW <= 0 || visH <= 0 {
		return defaultFallbackW, defaultFallbackH
	}
	return clampContent(visW*defaultScreenFraction, minContentW, maxContentW, visW),
		clampContent(visH*defaultScreenFraction, minContentH, maxContentH, visH)
}

// clampContent rounds want down to whole points, clamps it into [lo,hi] and then
// caps it at the available extent avail so the window never exceeds the screen.
func clampContent(want float64, lo, hi int, avail float64) int {
	v := int(want)
	if v < lo {
		v = lo
	}
	if v > hi {
		v = hi
	}
	if float64(v) > avail {
		v = int(avail)
	}
	return v
}

// Mods is the decoded modifier state carried on every toolkit event the Cocoa
// backend emits: Shift, Ctrl, Alt (⌥ Option) and Meta (⌘ Command).
type Mods struct{ Shift, Ctrl, Alt, Meta bool }

// DecodeMods splits an NSEvent modifierFlags mask into the four toolkit
// modifier flags.
//
// Shift maps from NSEventModifierFlagShift and Alt from NSEventModifierFlagOption
// (⌥). Meta maps from NSEventModifierFlagCommand (⌘). Ctrl maps from EITHER
// Control OR Command, so a plain ⌘-based macOS shortcut still reaches a widget
// with the same Ctrl flag an X11/Wayland Control chord would — preserving the
// toolkit's platform-neutral Ctrl shortcut semantics (Ctrl+C / ⌘C both set
// Ctrl) — while the new Meta flag additionally lets code that cares tell a real
// ⌘ chord apart from Control, and Alt surfaces ⌥ (both previously invisible).
// So ⌘V sets Ctrl+Meta and ⌘⌥V sets Ctrl+Meta+Alt, letting the file manager
// distinguish paste from paste-as-move.
func DecodeMods(flags uint64) Mods {
	return Mods{
		Shift: flags&modShift != 0,
		Ctrl:  flags&(modControl|modCommand) != 0,
		Alt:   flags&modOption != 0,
		Meta:  flags&modCommand != 0,
	}
}

// apply stamps the four modifier flags onto ev.
func (m Mods) apply(ev toolkit.Event) toolkit.Event {
	ev.Shift, ev.Ctrl, ev.Alt, ev.Meta = m.Shift, m.Ctrl, m.Alt, m.Meta
	return ev
}

// DecodeKey maps an NSEvent keyDown/keyUp to either a symbolic key NAME
// (DOM-style: "Enter", "ArrowLeft", …, exactly the names the toolkit widgets
// match and the wasmbox backend emits) or a printable rune. keyCode is checked
// first so Return/Escape/Delete/arrows never leak through as control or
// private-use runes. A result of ("", 0) means the key carries nothing to
// deliver (an unmapped or non-printable key).
//
// chars is the event's -charactersIgnoringModifiers value; only a single
// genuine printable rune (>= 0x20, not DEL, and outside the NSFunctionKey
// private-use range U+F700..U+F8FF, where AppKit reports arrows/F-keys) is
// accepted as text — the identical filter the reader precedent applies.
func DecodeKey(keyCode uint16, chars string) (name string, r rune) {
	switch keyCode {
	case keyReturn, keyKeypadEnter:
		return "Enter", 0
	case keyTab:
		return "Tab", 0
	case keyDelete:
		return "Backspace", 0
	case keyForwardDel:
		return "Delete", 0
	case keyEscape:
		return "Escape", 0
	case keyHome:
		return "Home", 0
	case keyEnd:
		return "End", 0
	case keyPageUp:
		return "PageUp", 0
	case keyPageDown:
		return "PageDown", 0
	case keyLeft:
		return "ArrowLeft", 0
	case keyRight:
		return "ArrowRight", 0
	case keyDownArrow:
		return "ArrowDown", 0
	case keyUpArrow:
		return "ArrowUp", 0
	}
	rs := []rune(chars)
	if len(rs) == 1 && isPrintable(rs[0]) {
		return "", rs[0]
	}
	return "", 0
}

// isPrintable reports whether r is a genuine committed character rather than a
// control code, DEL, or an NSFunctionKey private-use code point (U+F700..U+F8FF,
// which AppKit uses for arrows and function keys).
func isPrintable(r rune) bool {
	return r >= 0x20 && r != 0x7f && (r < 0xF700 || r > 0xF8FF)
}

// MapKey turns a decoded keyDown/keyUp into the toolkit event(s) it produces,
// mirroring the X11 and wasmbox backends EXACTLY:
//
//   - a named key yields a single EventKeyDown (press) / EventKeyUp (release)
//     carrying the name in Code;
//   - a printable key yields EventKeyDown+EventChar on press (Char being the
//     committed rune) and a single EventKeyUp on release — the same press/char
//     split the X11 backend performs;
//   - a key that decodes to nothing (unmapped / pure modifier) delivers nothing.
//
// The result is nil when the key maps to no toolkit event.
func MapKey(keyCode uint16, chars string, m Mods, press bool) []toolkit.Event {
	name, r := DecodeKey(keyCode, chars)
	if name != "" {
		kind := toolkit.EventKeyDown
		if !press {
			kind = toolkit.EventKeyUp
		}
		return []toolkit.Event{m.apply(toolkit.Event{Kind: kind, Code: name})}
	}
	if r == 0 {
		return nil
	}
	s := string(r)
	if press {
		return []toolkit.Event{
			m.apply(toolkit.Event{Kind: toolkit.EventKeyDown, Code: s}),
			m.apply(toolkit.Event{Kind: toolkit.EventChar, Code: s}),
		}
	}
	return []toolkit.Event{m.apply(toolkit.Event{Kind: toolkit.EventKeyUp, Code: s})}
}

// MapMouseDown turns a left/other mouse-button press at the given view-local
// pixel into an EventClick, mirroring the X11 ButtonPress (buttons 1–3 → click)
// mapping. macOS delivers separate selectors per button; the backend routes all
// of them here.
func MapMouseDown(x, y int, m Mods) toolkit.Event {
	return m.apply(toolkit.Event{Kind: toolkit.EventClick, X: x, Y: y})
}

// MapSecondaryClick turns a secondary (right / two-finger / Control-click) press
// at the given view-local pixel into an EventSecondaryClick — the gesture that
// opens a context menu. It is a press with no paired release: a menu opens on
// the down, and there is no secondary-drag to track.
func MapSecondaryClick(x, y int, m Mods) toolkit.Event {
	return m.apply(toolkit.Event{Kind: toolkit.EventSecondaryClick, X: x, Y: y})
}

// MapMouseUp turns a mouse-button release into an EventMouseUp.
func MapMouseUp(x, y int, m Mods) toolkit.Event {
	return m.apply(toolkit.Event{Kind: toolkit.EventMouseUp, X: x, Y: y})
}

// MapMouseMove turns a pointer move into a drag (a button held) or a plain hover
// move, per buttonHeld — the same drag-vs-move split the X11/Wayland backends
// derive from the event's button-state mask (AppKit instead delivers
// -mouseMoved: vs -mouseDragged:, which the glue collapses into buttonHeld).
func MapMouseMove(x, y int, buttonHeld bool, m Mods) toolkit.Event {
	kind := toolkit.EventMouseMove
	if buttonHeld {
		kind = toolkit.EventMouseDrag
	}
	return m.apply(toolkit.Event{Kind: kind, X: x, Y: y})
}

// MapScroll turns an AppKit -scrollingDeltaY into an EventScroll whose Delta is
// normalised to the toolkit's ±1 row step. AppKit's scrollingDeltaY is POSITIVE
// when the content is pushed up (a natural upward swipe); the toolkit's Delta is
// POSITIVE to scroll down/forward, so the sign is inverted — matching the
// browser/wheel convention the X11 and wasmbox backends use. A zero delta yields
// a Delta-0 EventScroll (harmless; scrollable widgets clamp it), so the mapping
// is total.
func MapScroll(x, y int, deltaY float64, m Mods) toolkit.Event {
	return m.apply(toolkit.Event{Kind: toolkit.EventScroll, X: x, Y: y, Delta: -signf(deltaY)})
}

// signf returns the sign of v as -1, 0 or +1.
func signf(v float64) int {
	switch {
	case v < 0:
		return -1
	case v > 0:
		return 1
	default:
		return 0
	}
}

// ViewCoords converts an NSEvent -locationInWindow (window base coordinates,
// which are ALWAYS bottom-left origin in points, even in a flipped view) to
// device-pixel coordinates with a top-left origin — the coordinate space the
// toolkit framebuffer and every toolkit.Event uses.
//
// boundsHPoints is the content view's height in points; scale is the window's
// backing scale factor (1 on a non-Retina display, 2 on Retina). The Y axis is
// flipped (boundsHPoints - py) to move the origin to the top, then both axes are
// multiplied by scale to reach device pixels.
func ViewCoords(px, py, boundsHPoints, scale float64) (int, int) {
	x := px * scale
	y := (boundsHPoints - py) * scale
	return int(x), int(y)
}

// DirtyRect converts a damage rectangle in DEVICE pixels (top-left origin, the
// space RenderDamaged reports and the framebuffer uses) to a rectangle in the
// flipped content view's POINT coordinates, ready for -setNeedsDisplayInRect:.
// Because the content view is flipped (isFlipped → top-left origin, matching the
// buffer), only a scale division is needed — no Y flip. The returned rectangle
// is clamped to be non-negative and is expanded to whole points (floor origin,
// ceil far edge) so a sub-point damage rect never leaves a seam.
func DirtyRect(r toolkit.Rect, scale float64) (x, y, w, h float64) {
	if scale <= 0 {
		scale = 1
	}
	x0 := float64(r.X) / scale
	y0 := float64(r.Y) / scale
	x1 := float64(r.X+r.W) / scale
	y1 := float64(r.Y+r.H) / scale
	x0, y0 = floor(x0), floor(y0)
	x1, y1 = ceil(x1), ceil(y1)
	return x0, y0, x1 - x0, y1 - y0
}

// floor/ceil are the two integer-boundary helpers DirtyRect needs, kept local so
// the codec stays a math.Nan-free leaf (the inputs are always finite, small,
// non-negative pixel counts).
func floor(v float64) float64 {
	i := float64(int(v))
	if v < 0 && i != v {
		i--
	}
	return i
}

func ceil(v float64) float64 {
	i := float64(int(v))
	if v > 0 && i != v {
		i++
	}
	return i
}

// unitToByte maps a 0..1 AppKit colour component to a 0..255 byte, clamped and
// rounded. It lives here, untagged, rather than beside the AppKit calls that
// feed it, because it is arithmetic and not glue: this way the Linux coverage
// gate proves it, on a machine that has no AppKit at all.
//
// Clamping is not defensive padding. AppKit colour components are documented as
// 0..1 but come from a colour-space conversion, which can land a hair outside
// on either side; without the clamp that becomes a wrapped byte -- a component
// of 1.000001 turning into 0, a black accent where the user chose white.
func unitToByte(v float64) uint8 {
	switch {
	case v <= 0:
		return 0
	case v >= 1:
		return 255
	default:
		return uint8(v*255 + 0.5)
	}
}
