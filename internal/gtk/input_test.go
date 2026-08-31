// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux && !android

package gtk

import (
	"testing"

	"github.com/go-gtk/gtk4"
	"github.com/go-widgets/toolkit"
)

// TestMapKeyNamed checks the named-key path: a GDK keyval for Return/arrow/etc.
// becomes a single EventKeyDown (press) or EventKeyUp (release) carrying the
// toolkit's DOM-style name, with modifiers stamped. The printable-rune path calls
// gdk_keyval_to_unicode, which needs GTK loaded, so it is exercised by the live
// end-to-end run rather than here.
func TestMapKeyNamed(t *testing.T) {
	// Return, pressed, with Ctrl held.
	evs := mapKey(0xff0d, gtk4.ModControl, true)
	if len(evs) != 1 || evs[0].Kind != toolkit.EventKeyDown || evs[0].Code != "Enter" || !evs[0].Ctrl {
		t.Fatalf("Return press = %+v, want one EventKeyDown Code=Enter Ctrl", evs)
	}
	// Left arrow, released.
	evs = mapKey(0xff51, 0, false)
	if len(evs) != 1 || evs[0].Kind != toolkit.EventKeyUp || evs[0].Code != "ArrowLeft" {
		t.Fatalf("Left release = %+v, want one EventKeyUp Code=ArrowLeft", evs)
	}
	// A bare modifier (Shift_L, 0xffe1) is neither named nor printable → nothing.
	// (Its keyval is not in the table and maps to no Unicode, but calling
	// KeyvalToUnicode needs GTK; instead assert the table lookup for a key we do
	// name, and trust the live run for the unnamed-non-printable case.)
	if _, ok := keyNames[0xff09]; !ok {
		t.Fatal("Tab (0xff09) should be a named key")
	}
}

// TestRows reduces a scroll delta to the toolkit's ±1-row step, with no inversion.
func TestRows(t *testing.T) {
	if rows(2.5) != 1 || rows(-0.1) != -1 || rows(0) != 0 {
		t.Fatalf("rows: got %d,%d,%d want 1,-1,0", rows(2.5), rows(-0.1), rows(0))
	}
}

// TestApplyMods decodes the four GDK modifier bits the toolkit carries.
func TestApplyMods(t *testing.T) {
	ev := applyMods(toolkit.Event{}, gtk4.ModShift|gtk4.ModControl|gtk4.ModAlt|gtk4.ModSuper)
	if !ev.Shift || !ev.Ctrl || !ev.Alt || !ev.Meta {
		t.Fatalf("all mods set: %+v", ev)
	}
	if ev = applyMods(toolkit.Event{}, 0); ev.Shift || ev.Ctrl || ev.Alt || ev.Meta {
		t.Fatalf("no mods: %+v", ev)
	}
}

// TestPointerClassifies checks the button/press mapping and the drag latch,
// without a display: pointer only reads scale and writes the root (nil here) and
// the dirty/btnDown flags.
func TestPointerClassifies(t *testing.T) {
	w := &Window{scale: 2}
	w.pointer(1, 0, 5, 10, true) // primary press at (5,10) points → (10,20) px
	if !w.btnDown || !w.dirty.Load() {
		t.Fatalf("primary press: btnDown=%v dirty=%v, want both true", w.btnDown, w.dirty.Load())
	}
	w.pointer(1, 0, 5, 10, false) // release clears the latch
	if w.btnDown {
		t.Fatal("release did not clear btnDown")
	}
	w.pointer(3, 0, 0, 0, true) // secondary press is not a drag latch
	if w.btnDown {
		t.Fatal("secondary press should not latch a drag")
	}
}
