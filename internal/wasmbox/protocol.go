// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// Package wasmbox implements the client half of the wasmdesk/wasmbox
// external-client wire protocol, so a go-widgets application can run as a
// client of the browser compositor exactly as it runs on X11 or Wayland.
//
// This file is the SOVEREIGN, transport-agnostic codec: the wire messages
// (hello/welcome/commit/input/set_title/request_close/closed), the SAB
// damage-rectangle maths and the input→toolkit.Event mapping, all expressed
// over plain Go values (JSON-cloneable map[string]any and structs). It carries
// NO syscall/js dependency, so it builds — and is unit-tested to 100% — on
// every GOOS. The thin syscall/js glue that actually allocates the
// SharedArrayBuffer and posts/receives messages over the compositor MessagePort
// lives in client_js.go (//go:build js && wasm) and drives everything here.
//
// The wire contract mirrored here is wasmdesk/wasmbox's docs/protocol.md; the
// wasmbox repository itself is unmodified — this is a pure client-side backend.
package wasmbox

import (
	"fmt"

	"github.com/go-widgets/toolkit"
)

// Message type discriminants (the JSON `type` field), matching docs/protocol.md.
const (
	KindHello        = "hello"
	KindWelcome      = "welcome"
	KindCommit       = "commit"
	KindInput        = "input"
	KindSetTitle     = "set_title"
	KindRequestClose = "request_close"
	KindClosed       = "closed"
)

// RoleWindow is the default surface role (a normal, decorated, focusable
// window). The backend only ever opens window-role surfaces.
const RoleWindow = "window"

// Rect is a surface-local rectangle in pixels, matching toolkit.Rect's shape
// but kept local so the codec stays a leaf with a single toolkit dependency
// (the event model). Conversions are trivial.
type Rect struct{ X, Y, W, H int }

// Hello is the client's opening handshake message. The SharedArrayBuffer and
// the (optional) seqlock control word are attached by the syscall/js layer as
// live JS handles; every scalar field of the wire message is produced here.
type Hello struct {
	Title  string
	W, H   int
	Stride int // bytes per row; canonical = 4*W
}

// Welcome is the compositor's reply granting a window id and (possibly clamped)
// surface size.
type Welcome struct {
	WindowID int
	GrantedW int
	GrantedH int
}

// InputEvent is one decoded `input.event` payload. Fields not carried by a
// given kind are zero (e.g. Key/Code are empty for mouse events; X/Y are zero
// for keyboard events).
type InputEvent struct {
	Kind   string // mousedown | mouseup | mousemove | wheel | keydown | keyup
	X, Y   int
	Button int
	Key    string
	Code   string
	DX, DY int
}

// Mouse/keyboard input kinds carried by input.event.kind.
const (
	InMouseDown = "mousedown"
	InMouseUp   = "mouseup"
	InMouseMove = "mousemove"
	InWheel     = "wheel"
	InKeyDown   = "keydown"
	InKeyUp     = "keyup"
)

// EncodeHello builds the JSON-cloneable `hello` message body (minus the live
// SAB/ctl JS handles, which the transport attaches). role is always "window".
func EncodeHello(h Hello) map[string]any {
	return map[string]any{
		"type":   KindHello,
		"title":  h.Title,
		"role":   RoleWindow,
		"w":      h.W,
		"h":      h.H,
		"stride": h.Stride,
	}
}

// DecodeWelcome parses a `welcome` message.
func DecodeWelcome(m map[string]any) (Welcome, error) {
	if k := MessageKind(m); k != KindWelcome {
		return Welcome{}, fmt.Errorf("wasmbox: want %q message, got %q", KindWelcome, k)
	}
	return Welcome{
		WindowID: toInt(m["window_id"]),
		GrantedW: toInt(m["granted_w"]),
		GrantedH: toInt(m["granted_h"]),
	}, nil
}

// EncodeCommit builds a `commit` message naming the damaged sub-rectangle.
func EncodeCommit(windowID int, damage Rect) map[string]any {
	return map[string]any{
		"type":      KindCommit,
		"window_id": windowID,
		"damage":    rectToMap(damage),
	}
}

// EncodeSetTitle builds a `set_title` message.
func EncodeSetTitle(windowID int, title string) map[string]any {
	return map[string]any{
		"type":      KindSetTitle,
		"window_id": windowID,
		"title":     title,
	}
}

// EncodeRequestClose builds a `request_close` message.
func EncodeRequestClose(windowID int) map[string]any {
	return map[string]any{
		"type":      KindRequestClose,
		"window_id": windowID,
	}
}

// DecodeInput parses an `input` message into the target window id and the event
// payload.
func DecodeInput(m map[string]any) (windowID int, ev InputEvent, err error) {
	if k := MessageKind(m); k != KindInput {
		return 0, InputEvent{}, fmt.Errorf("wasmbox: want %q message, got %q", KindInput, k)
	}
	windowID = toInt(m["window_id"])
	e, _ := m["event"].(map[string]any)
	ev = InputEvent{
		Kind:   toStr(e["kind"]),
		X:      toInt(e["x"]),
		Y:      toInt(e["y"]),
		Button: toInt(e["button"]),
		Key:    toStr(e["key"]),
		Code:   toStr(e["code"]),
		DX:     toInt(e["dx"]),
		DY:     toInt(e["dy"]),
	}
	return windowID, ev, nil
}

// DecodeClosed parses a `closed` message.
func DecodeClosed(m map[string]any) (windowID int, reason string, err error) {
	if k := MessageKind(m); k != KindClosed {
		return 0, "", fmt.Errorf("wasmbox: want %q message, got %q", KindClosed, k)
	}
	return toInt(m["window_id"]), toStr(m["reason"]), nil
}

// MessageKind returns the message's `type` discriminant, or "" when absent.
func MessageKind(m map[string]any) string { return toStr(m["type"]) }

// ---- SAB damage-rectangle maths -------------------------------------------

// FullRect is the whole w×h surface as a damage rectangle.
func FullRect(w, h int) Rect { return Rect{X: 0, Y: 0, W: w, H: h} }

// ClampRect intersects r with the w×h surface, returning a possibly-empty
// rectangle (W or H <= 0 when r lies fully off-surface). It mirrors the
// backend's own clamp so commit damage is always in-bounds for the compositor.
func ClampRect(r Rect, w, h int) Rect {
	x, y, rw, rh := r.X, r.Y, r.W, r.H
	if x < 0 {
		rw += x
		x = 0
	}
	if y < 0 {
		rh += y
		y = 0
	}
	if x+rw > w {
		rw = w - x
	}
	if y+rh > h {
		rh = h - y
	}
	return Rect{X: x, Y: y, W: rw, H: rh}
}

// RowSpan returns, for a clamped rectangle over a surface of the given
// bytes-per-row stride, the byte offset of its first pixel and the number of
// bytes in one of its rows — the coordinates a transport uses to copy just the
// damaged rows into the SAB (4 bytes/pixel, row-major, top-left origin).
func RowSpan(r Rect, stride int) (startByte, rowBytes int) {
	return r.Y*stride + r.X*4, r.W * 4
}

// ---- input → toolkit.Event mapping ----------------------------------------

// MapInput translates one decoded wasmbox input event into the toolkit event(s)
// it produces, mapping kind/x/y/button/key/code/dx/dy to the toolkit event
// model exactly as the X11 and Wayland backends do:
//
//   - mousedown → EventClick (all buttons, mirroring X11 ButtonPress 1–3)
//   - mouseup   → EventMouseUp
//   - mousemove → EventMouseDrag when a button is held, else EventMouseMove
//   - wheel     → EventScroll with Delta normalised to −1 (up/back) / +1
//     (down/forward), matching the X11 wheel-button mapping; the vertical
//     delta wins, falling back to the horizontal delta when dy is zero
//   - keydown   → a named key (len>1, e.g. "Enter"/"ArrowLeft") yields one
//     EventKeyDown carrying that name in Code; a printable key (a single rune)
//     yields EventKeyDown+EventChar, Char being the committed rune — the same
//     press/char split the X11 backend performs
//   - keyup     → the symmetric single EventKeyUp
//   - modifier keys (Shift/Control/Alt/Meta/…) deliver nothing, mirroring
//     X11's IsModifier suppression
//
// buttonHeld carries the transport's pointer-button state so mousemove can pick
// drag vs move statelessly (the X11/Wayland backends read it from the event's
// own state mask; wasmbox input carries none, so the transport tracks it). The
// result is nil when the event maps to no toolkit event.
func MapInput(ev InputEvent, buttonHeld bool) []toolkit.Event {
	switch ev.Kind {
	case InMouseDown:
		return []toolkit.Event{{Kind: toolkit.EventClick, X: ev.X, Y: ev.Y}}
	case InMouseUp:
		return []toolkit.Event{{Kind: toolkit.EventMouseUp, X: ev.X, Y: ev.Y}}
	case InMouseMove:
		kind := toolkit.EventMouseMove
		if buttonHeld {
			kind = toolkit.EventMouseDrag
		}
		return []toolkit.Event{{Kind: kind, X: ev.X, Y: ev.Y}}
	case InWheel:
		return []toolkit.Event{{Kind: toolkit.EventScroll, X: ev.X, Y: ev.Y, Delta: wheelDelta(ev.DX, ev.DY)}}
	case InKeyDown:
		return mapKey(ev.Key, true)
	case InKeyUp:
		return mapKey(ev.Key, false)
	default:
		return nil
	}
}

// wheelDelta normalises a DOM wheel delta to the toolkit's ±1 row step: the
// vertical delta decides the sign, falling back to the horizontal delta when it
// is zero (a zero result means "no scroll", which callers pass through as a
// Delta-0 EventScroll — harmless, clamped by scrollable widgets).
func wheelDelta(dx, dy int) int {
	if dy != 0 {
		return sign(dy)
	}
	return sign(dx)
}

func sign(v int) int {
	switch {
	case v < 0:
		return -1
	case v > 0:
		return 1
	default:
		return 0
	}
}

// mapKey mirrors the X11 backend's key handling: modifier keys are dropped; a
// named key yields a single KeyDown/KeyUp carrying its name; a printable key
// yields KeyDown+Char on press and KeyUp on release, the character sitting in
// Code (KeyDown/KeyUp) and Code (Char), as the toolkit expects.
func mapKey(key string, press bool) []toolkit.Event {
	if key == "" || isModifierKey(key) {
		return nil
	}
	if isPrintableKey(key) {
		if press {
			return []toolkit.Event{
				{Kind: toolkit.EventKeyDown, Code: key},
				{Kind: toolkit.EventChar, Code: key},
			}
		}
		return []toolkit.Event{{Kind: toolkit.EventKeyUp, Code: key}}
	}
	kind := toolkit.EventKeyDown
	if !press {
		kind = toolkit.EventKeyUp
	}
	return []toolkit.Event{{Kind: kind, Code: key}}
}

// isPrintableKey reports whether a DOM KeyboardEvent.key value denotes a single
// committed character (as opposed to a named key like "Enter" or "ArrowUp").
// DOM reports printable keys as the character itself — exactly one rune.
func isPrintableKey(key string) bool {
	n := 0
	for range key {
		n++
		if n > 1 {
			return false
		}
	}
	return n == 1
}

// modifierKeys is the set of DOM KeyboardEvent.key names that are pure
// modifiers and therefore deliver no toolkit event (matching X11 IsModifier).
var modifierKeys = map[string]bool{
	"Shift": true, "Control": true, "Alt": true, "AltGraph": true,
	"Meta": true, "CapsLock": true, "NumLock": true, "ScrollLock": true,
	"Fn": true, "FnLock": true, "Super": true, "Hyper": true, "OS": true,
}

func isModifierKey(key string) bool { return modifierKeys[key] }

// ---- value helpers --------------------------------------------------------

// rectToMap renders a Rect as the JSON `{x,y,w,h}` object the wire uses.
func rectToMap(r Rect) map[string]any {
	return map[string]any{"x": r.X, "y": r.Y, "w": r.W, "h": r.H}
}

// toInt coerces a JSON-cloneable numeric value (int from Go encoders, float64
// from JS/JSON, or a nil for an absent field) to an int.
func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case float32:
		return int(n)
	default:
		return 0
	}
}

// toStr coerces a JSON-cloneable value to a string, yielding "" for a non-string
// or absent field.
func toStr(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
