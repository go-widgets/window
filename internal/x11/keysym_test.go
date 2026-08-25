// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package x11

import (
	"encoding/binary"
	"testing"

	xproto "github.com/go-freedesktop/x11"
)

func TestParseKeyboardMappingAndLookup(t *testing.T) {
	order := binary.LittleEndian
	// Two keycodes starting at 8, 2 keysyms each:
	//   kc 8: unshifted 'a' (0x61), shifted 'A' (0x41)
	//   kc 9: unshifted Return (0xff0d), shifted NoSymbol (0)
	first, count, perCode := uint8(8), uint8(2), uint8(2)
	e := xproto.NewEncoder(order)
	for _, ks := range []uint32{0x61, 0x41, ksReturn, 0} {
		e.Put32(ks)
	}
	km := parseKeyboardMapping(order, first, count, perCode, e.Bytes())

	if km.Keysym(8, false) != 0x61 || km.Keysym(8, true) != 0x41 {
		t.Fatalf("kc8 lookup wrong")
	}
	if km.Keysym(9, false) != ksReturn {
		t.Fatalf("kc9 unshifted wrong")
	}
	// Shifted NoSymbol falls back to unshifted.
	if km.Keysym(9, true) != ksReturn {
		t.Fatalf("kc9 shifted fallback wrong")
	}
	// Out-of-range keycodes.
	if km.Keysym(7, false) != 0 || km.Keysym(200, false) != 0 {
		t.Fatalf("out-of-range should be 0")
	}
	// nil / empty keymap.
	var nilKM *Keymap
	if nilKM.Keysym(8, false) != 0 {
		t.Fatalf("nil keymap should be 0")
	}
	empty := &Keymap{Min: 8, PerCode: 0}
	if empty.Keysym(8, false) != 0 {
		t.Fatalf("perCode 0 should be 0")
	}
}

func TestKeyboardMappingHeader(t *testing.T) {
	if b1, b2 := keyboardMappingHeader(8, 248); b1 != 8 || b2 != 248 {
		t.Fatalf("header %d,%d", b1, b2)
	}
}

func TestKeysymName(t *testing.T) {
	cases := map[uint32]string{
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
		ksControlL:  "Control",
		ksAltL:      "Alt",
	}
	for ks, want := range cases {
		if got := KeysymName(ks); got != want {
			t.Errorf("KeysymName(%#x)=%q want %q", ks, got, want)
		}
	}
	if KeysymName(0x61) != "" {
		t.Errorf("printable keysym should have no name")
	}
}

func TestIsModifier(t *testing.T) {
	for _, ks := range []uint32{ksShiftL, ksShiftR, ksControlL, ksControlR, ksAltL, ksAltR} {
		if !IsModifier(ks) {
			t.Errorf("%#x should be modifier", ks)
		}
	}
	if IsModifier(ksReturn) || IsModifier(0x61) {
		t.Errorf("non-modifier flagged")
	}
}

func TestKeysymRune(t *testing.T) {
	cases := []struct {
		ks   uint32
		r    rune
		want bool
	}{
		{0x61, 'a', true},       // ascii lower
		{0x41, 'A', true},       // ascii upper
		{0x7e, '~', true},       // ascii high
		{0x20, 0, false},        // space excluded (named)
		{0xe9, 'é', true},       // latin-1
		{0x01000041, 'A', true}, // unicode-flagged
		{0x02000041, 0, false},  // top byte not 0x01 -> not matched
		{ksReturn, 0, false},    // control keysym, no rune
	}
	for _, c := range cases {
		r, ok := KeysymRune(c.ks)
		if ok != c.want || (ok && r != c.r) {
			t.Errorf("KeysymRune(%#x)=(%q,%v) want (%q,%v)", c.ks, r, ok, c.r, c.want)
		}
	}
}
