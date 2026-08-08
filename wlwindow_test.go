// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"testing"

	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/window/internal/wayland"
)

// testKeymap is a tiny xkb keymap covering the keys the translation tests
// exercise: a/A, Return, Shift_L (modifier), space.
const testKeymap = `
xkb_keymap {
  xkb_keycodes "k" {
    <AC01> = 38;
    <RTRN> = 36;
    <LFSH> = 50;
    <SPCE> = 65;
  };
  xkb_symbols "s" {
    key <AC01> { [ a, A ] };
    key <RTRN> { [ Return ] };
    key <LFSH> { [ Shift_L ] };
    key <SPCE> { [ space ] };
  };
};
`

// evdev codes are the xkb keycodes minus the constant offset of 8.
const (
	evA     = 38 - 8
	evRtrn  = 36 - 8
	evShift = 50 - 8
	evSpace = 65 - 8
)

func TestTranslateKey(t *testing.T) {
	km := wayland.ParseKeymap(testKeymap)

	// Printable press -> KeyDown + Char; release -> KeyUp.
	evs := translateKey(km, evA, true, false, false)
	if len(evs) != 2 || evs[0].Kind != toolkit.EventKeyDown || evs[0].Code != "a" ||
		evs[1].Kind != toolkit.EventChar || evs[1].Code != "a" {
		t.Fatalf("press a = %+v", evs)
	}
	up := translateKey(km, evA, false, false, false)
	if len(up) != 1 || up[0].Kind != toolkit.EventKeyUp || up[0].Code != "a" {
		t.Fatalf("release a = %+v", up)
	}
	// Shift selects level 1 ('A') and marks the event Shift.
	sh := translateKey(km, evA, true, true, false)
	if sh[1].Code != "A" || !sh[1].Shift {
		t.Fatalf("shifted a = %+v", sh)
	}
	// Ctrl passes through.
	if c := translateKey(km, evA, true, false, true); !c[0].Ctrl {
		t.Fatalf("ctrl a = %+v", c)
	}
	// Named key: single KeyDown/KeyUp carrying the toolkit name.
	nd := translateKey(km, evRtrn, true, false, false)
	if len(nd) != 1 || nd[0].Kind != toolkit.EventKeyDown || nd[0].Code != "Enter" {
		t.Fatalf("Return down = %+v", nd)
	}
	nu := translateKey(km, evRtrn, false, false, false)
	if nu[0].Kind != toolkit.EventKeyUp || nu[0].Code != "Enter" {
		t.Fatalf("Return up = %+v", nu)
	}
	// space -> ' ' rune.
	sp := translateKey(km, evSpace, true, false, false)
	if sp[1].Kind != toolkit.EventChar || sp[1].Code != " " {
		t.Fatalf("space = %+v", sp)
	}
	// Modifier key delivers nothing.
	if m := translateKey(km, evShift, true, false, false); m != nil {
		t.Fatalf("Shift_L should deliver nothing: %+v", m)
	}
	// Unmapped keycode delivers nothing.
	if u := translateKey(km, 250, true, false, false); u != nil {
		t.Fatalf("unmapped key should deliver nothing: %+v", u)
	}
}

func TestTranslateButton(t *testing.T) {
	w := &wlWindow{ptrX: 33, ptrY: 44}
	// Left press -> EventClick + sets the drag bit.
	press := w.translateButton(wayland.BtnLeft, true, false, false)
	if len(press) != 1 || press[0].Kind != toolkit.EventClick || press[0].X != 33 || press[0].Y != 44 {
		t.Fatalf("left press = %+v", press)
	}
	if w.buttons == 0 {
		t.Fatal("press should set the drag bit")
	}
	// Release -> EventMouseUp + clears the bit; modifiers pass through.
	rel := w.translateButton(wayland.BtnLeft, false, true, true)
	if rel[0].Kind != toolkit.EventMouseUp || !rel[0].Ctrl || !rel[0].Shift {
		t.Fatalf("left release = %+v", rel)
	}
	if w.buttons != 0 {
		t.Fatal("release should clear the drag bit")
	}
	// Right/middle also map; an unknown button is dropped.
	if r := w.translateButton(wayland.BtnRight, true, false, false); r[0].Kind != toolkit.EventClick {
		t.Fatalf("right press = %+v", r)
	}
	if m := w.translateButton(wayland.BtnMiddle, true, false, false); m[0].Kind != toolkit.EventClick {
		t.Fatalf("middle press = %+v", m)
	}
	if u := w.translateButton(0x999, true, false, false); u != nil {
		t.Fatalf("unknown button should be dropped: %+v", u)
	}
}

func TestTranslateMotion(t *testing.T) {
	w := &wlWindow{ptrX: 5, ptrY: 6}
	// No button held -> hover move.
	if mv := w.translateMotion(false, false); mv[0].Kind != toolkit.EventMouseMove || mv[0].X != 5 {
		t.Fatalf("move = %+v", mv)
	}
	// Button held -> drag; modifiers pass through.
	w.buttons = 1
	if dr := w.translateMotion(true, false); dr[0].Kind != toolkit.EventMouseDrag || !dr[0].Shift {
		t.Fatalf("drag = %+v", dr)
	}
}

func TestTranslateAxis(t *testing.T) {
	w := &wlWindow{ptrX: 1, ptrY: 2}
	down := w.translateAxis(wayland.AxisVerticalScroll, wayland.FixedFromInt(10), false, false)
	if len(down) != 1 || down[0].Kind != toolkit.EventScroll || down[0].Delta != 1 {
		t.Fatalf("scroll down = %+v", down)
	}
	up := w.translateAxis(wayland.AxisVerticalScroll, wayland.FixedFromInt(-5), false, false)
	if up[0].Delta != -1 {
		t.Fatalf("scroll up = %+v", up)
	}
	// Zero value and horizontal axis produce nothing.
	if z := w.translateAxis(wayland.AxisVerticalScroll, 0, false, false); z != nil {
		t.Fatalf("zero axis = %+v", z)
	}
	if h := w.translateAxis(wayland.AxisHorizontalScroll, wayland.FixedFromInt(3), false, false); h != nil {
		t.Fatalf("horizontal axis = %+v", h)
	}
}

func TestButtonBit(t *testing.T) {
	if buttonBit(wayland.BtnLeft) != 1 || buttonBit(wayland.BtnMiddle) != 2 ||
		buttonBit(wayland.BtnRight) != 4 || buttonBit(0x999) != 0 {
		t.Fatal("buttonBit mapping wrong")
	}
}

func TestWlWindowDrawAndHelpers(t *testing.T) {
	root := &recWidget{}
	w := &wlWindow{w: 20, h: 10, buf: make([]byte, 4*20*10), theme: toolkit.DefaultDark(), root: root}
	w.draw()
	if root.drawn == 0 {
		t.Fatal("root should be drawn")
	}
	if gw, gh := w.Size(); gw != 20 || gh != 10 {
		t.Fatalf("Size = %dx%d", gw, gh)
	}
	if w.String() == "" {
		t.Fatal("String empty")
	}
	// mods with no keyboard is false/false.
	if s, c := w.mods(); s || c {
		t.Fatal("mods with no keyboard should be false")
	}
	// queue of nothing is a no-op; queue of something marks repaint.
	w.queue(nil)
	if w.repaint {
		t.Fatal("empty queue should not repaint")
	}
	w.queue([]toolkit.Event{{Kind: toolkit.EventClick}})
	if !w.repaint || len(w.pending) != 1 {
		t.Fatal("queue should append and mark repaint")
	}
	// draw with no root is fine.
	(&wlWindow{w: 4, h: 4, buf: make([]byte, 4*4*4), theme: toolkit.DefaultDark()}).draw()
}

func TestWlPresentBeforeConfigure(t *testing.T) {
	// present is a no-op until the first xdg configure arrives.
	w := &wlWindow{configured: false}
	if err := w.present(); err != nil {
		t.Fatalf("present before configure should be a no-op: %v", err)
	}
}

func TestWlWindowApplyResize(t *testing.T) {
	w := &wlWindow{w: 10, h: 10, buf: make([]byte, 4*10*10)}
	w.pendingW, w.pendingH, w.needResize = 30, 20, true
	w.applyResize()
	if w.w != 30 || w.h != 20 || len(w.buf) != 4*30*20 || !w.repaint || w.needResize {
		t.Fatalf("resize state w=%d h=%d buf=%d", w.w, w.h, len(w.buf))
	}
	// A zero pending size is rejected and clears the flag.
	w.pendingW, w.pendingH, w.needResize = 0, 0, true
	w.applyResize()
	if w.needResize {
		t.Fatal("zero resize should clear the flag")
	}
}

