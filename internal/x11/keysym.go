// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package x11

import (
	xproto "github.com/go-freedesktop/x11"
)

// Keymap holds a decoded GetKeyboardMapping reply: for each keycode in
// [Min, Min+len/PerCode) a run of PerCode keysyms, level 0 being the
// unshifted symbol and level 1 the shifted one.
type Keymap struct {
	Min     uint8
	PerCode int
	Syms    []uint32
}

// buildGetKeyboardMappingReq encodes a GetKeyboardMapping request body
// (the bytes after the 4-byte request header): the request has no body,
// so first-keycode and count travel in the header. This helper returns
// the header fields the Conn needs; the request itself is assembled by
// Conn.getKeyboardMapping.
//
// It is kept as a pure function purely for symmetry with the other
// request builders and to keep the header layout documented in one place.
func keyboardMappingHeader(first, count uint8) (b1, b2 byte) { return first, count }

// parseKeyboardMapping turns the additional-data block of a
// GetKeyboardMapping reply into a Keymap. perCode is the reply header's
// keysyms-per-keycode byte; first is the first-keycode the request asked
// for; count is how many keycodes were requested.
func parseKeyboardMapping(order ByteOrder, first, count, perCode uint8, body []byte) *Keymap {
	km := &Keymap{Min: first, PerCode: int(perCode)}
	n := int(count) * int(perCode)
	d := xproto.NewDecoder(order, body)
	km.Syms = make([]uint32, 0, n)
	for i := 0; i < n; i++ {
		km.Syms = append(km.Syms, d.Get32())
	}
	return km
}

// Keysym returns the keysym bound to keycode at the given shift level
// (false = level 0, true = level 1). A level-1 lookup that resolves to
// NoSymbol (0) falls back to level 0, matching the core-protocol rule
// that an absent shifted symbol repeats the unshifted one. Out-of-range
// keycodes yield 0.
func (k *Keymap) Keysym(keycode uint8, shift bool) uint32 {
	if k == nil || k.PerCode == 0 {
		return 0
	}
	if keycode < k.Min {
		return 0
	}
	idx := (int(keycode) - int(k.Min)) * k.PerCode
	if idx < 0 || idx+k.PerCode > len(k.Syms) {
		return 0
	}
	level := 0
	if shift {
		level = 1
	}
	ks := k.Syms[idx+level]
	if ks == 0 && level == 1 {
		ks = k.Syms[idx]
	}
	return ks
}

// X11 keysym constants (subset of keysymdef.h) that map to named,
// non-printable keys the toolkit understands.
const (
	ksBackSpace = 0xff08
	ksTab       = 0xff09
	ksReturn    = 0xff0d
	ksEscape    = 0xff1b
	ksHome      = 0xff50
	ksLeft      = 0xff51
	ksUp        = 0xff52
	ksRight     = 0xff53
	ksDown      = 0xff54
	ksPageUp    = 0xff55
	ksPageDown  = 0xff56
	ksEnd       = 0xff57
	ksDelete    = 0xffff
	ksKPEnter   = 0xff8d
	ksSpace     = 0x0020

	ksShiftL   = 0xffe1
	ksShiftR   = 0xffe2
	ksControlL = 0xffe3
	ksControlR = 0xffe4
	ksAltL     = 0xffe9
	ksAltR     = 0xffea
)

// keysymNames maps named keysyms to the DOM-style key names the toolkit's
// widgets switch on (see toolkit.Event.Code).
var keysymNames = map[uint32]string{
	ksBackSpace: "Backspace",
	ksTab:       "Tab",
	ksReturn:    "Enter",
	ksKPEnter:   "Enter",
	ksEscape:    "Escape",
	ksHome:      "Home",
	ksLeft:      "ArrowLeft",
	ksUp:        "ArrowUp",
	ksRight:     "ArrowRight",
	ksDown:      "ArrowDown",
	ksPageUp:    "PageUp",
	ksPageDown:  "PageDown",
	ksEnd:       "End",
	ksDelete:    "Delete",
	ksSpace:     "Space",
	ksShiftL:    "Shift",
	ksShiftR:    "Shift",
	ksControlL:  "Control",
	ksControlR:  "Control",
	ksAltL:      "Alt",
	ksAltR:      "Alt",
}

// KeysymName returns the toolkit key name for a keysym, or "" when the
// keysym has no named binding (it is either printable — see KeysymRune —
// or unhandled).
func KeysymName(ks uint32) string { return keysymNames[ks] }

// IsModifier reports whether ks is a Shift/Control/Alt modifier keysym,
// which the host tracks for Event.Ctrl/Event.Shift but does not deliver
// as a character.
func IsModifier(ks uint32) bool {
	switch ks {
	case ksShiftL, ksShiftR, ksControlL, ksControlR, ksAltL, ksAltR:
		return true
	}
	return false
}

// KeysymRune returns the printable rune a keysym produces and whether it
// is printable. Latin-1 keysyms (0x20–0xff) are their own codepoint; the
// 0x01000000-flagged range carries a direct Unicode codepoint. The space
// key is treated as a named key (KeysymName == "Space"), not a rune, so
// it is excluded here.
func KeysymRune(ks uint32) (rune, bool) {
	switch {
	case ks == ksSpace:
		return 0, false
	case ks >= 0x20 && ks <= 0x7e:
		return rune(ks), true
	case ks >= 0xa0 && ks <= 0xff:
		return rune(ks), true
	case ks&0xff000000 == 0x01000000:
		return rune(ks & 0x00ffffff), true
	}
	return 0, false
}
