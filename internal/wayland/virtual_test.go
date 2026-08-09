// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package wayland

import (
	"errors"
	"testing"
)

// vkMgrReg builds a registry whose conn records writes on a stubTransport,
// advertising the virtual-keyboard manager global.
func vkMgrReg(order ByteOrder) (*Registry, *stubTransport) {
	st := &stubTransport{}
	c := NewConn(st, order)
	reg := &Registry{conn: c, globals: []Global{
		{Name: 37, Interface: ifaceVirtualKeyboardManager, Version: 1},
		{Name: 38, Interface: ifaceVirtualPointerManager, Version: 2},
	}}
	return reg, st
}

func TestVirtualKeyboardManagerBind(t *testing.T) {
	bothOrders(t, func(t *testing.T, order ByteOrder) {
		reg, st := vkMgrReg(order)
		m, err := reg.VirtualKeyboardManager()
		if err != nil {
			t.Fatalf("VirtualKeyboardManager: %v", err)
		}
		_, op, d := lastWrite(t, st, order)
		if op != registryReqBind {
			t.Errorf("bind opcode = %d", op)
		}
		name := d.getU32()
		iface := d.getString()
		ver := d.getU32()
		if name != 37 || iface != ifaceVirtualKeyboardManager || ver != 1 {
			t.Errorf("bind args name=%d iface=%q ver=%d", name, iface, ver)
		}
		if m.id == 0 {
			t.Error("manager id unset")
		}
	})
}

func TestVirtualKeyboardManagerNotAdvertised(t *testing.T) {
	st := &stubTransport{}
	reg := &Registry{conn: NewConn(st, NativeOrder)}
	if _, err := reg.VirtualKeyboardManager(); err == nil {
		t.Error("missing manager global should error")
	}
}

func TestVirtualKeyboardManagerBindWriteError(t *testing.T) {
	c := NewConn(&stubTransport{writeErr: errors.New("nope")}, NativeOrder)
	reg := &Registry{conn: c, globals: []Global{{Name: 37, Interface: ifaceVirtualKeyboardManager, Version: 1}}}
	if _, err := reg.VirtualKeyboardManager(); err == nil {
		t.Error("bind write error should propagate")
	}
}

func TestVirtualKeyboardCreateAndRequests(t *testing.T) {
	bothOrders(t, func(t *testing.T, order ByteOrder) {
		// CreateKeyboard over a real socket pair so the keymap fd is actually
		// passed via SCM_RIGHTS and observed by the fake compositor.
		c, fs := newTestConn(t, order)
		m := &VirtualKeyboardManager{conn: c, id: 5}
		seat := &Seat{conn: c, id: 9}
		k, err := m.CreateKeyboard(seat)
		if err != nil {
			t.Fatalf("CreateKeyboard: %v", err)
		}
		obj, op, d, err := fs.readReq()
		if err != nil {
			t.Fatalf("read create: %v", err)
		}
		if obj != 5 || op != vkbdMgrReqCreate {
			t.Errorf("create obj=%d op=%d", obj, op)
		}
		if s := d.getU32(); s != 9 {
			t.Errorf("create seat arg = %d, want 9", s)
		}
		if id := d.getU32(); id != k.id {
			t.Errorf("create new_id = %d, want %d", id, k.id)
		}

		// keymap: passes an fd + (format,size) body.
		fd, size := makeKeymapFD(t, kmText)
		if err := k.Keymap(KeymapFormatXkbV1, fd, uint32(size)); err != nil {
			t.Fatalf("Keymap: %v", err)
		}
		obj, op, d, err = fs.readReq()
		if err != nil {
			t.Fatalf("read keymap: %v", err)
		}
		if obj != k.id || op != vkbdReqKeymap {
			t.Errorf("keymap obj=%d op=%d", obj, op)
		}
		if f := d.getU32(); f != KeymapFormatXkbV1 {
			t.Errorf("keymap format = %d", f)
		}
		if sz := d.getU32(); sz != uint32(size) {
			t.Errorf("keymap size = %d, want %d", sz, size)
		}
		if rfd, ok := fs.popFD(); !ok {
			t.Error("keymap fd not received by compositor")
		} else {
			_ = rfd
		}

		// key
		if err := k.Key(7, 30, StatePressed); err != nil {
			t.Fatalf("Key: %v", err)
		}
		obj, op, d, _ = fs.readReq()
		if obj != k.id || op != vkbdReqKey {
			t.Errorf("key obj=%d op=%d", obj, op)
		}
		if tm, key, state := d.getU32(), d.getU32(), d.getU32(); tm != 7 || key != 30 || state != StatePressed {
			t.Errorf("key args = %d,%d,%d", tm, key, state)
		}

		// modifiers
		if err := k.Modifiers(1, 2, 3, 4); err != nil {
			t.Fatalf("Modifiers: %v", err)
		}
		obj, op, d, _ = fs.readReq()
		if obj != k.id || op != vkbdReqModifiers {
			t.Errorf("modifiers obj=%d op=%d", obj, op)
		}
		if a, b, cc, g := d.getU32(), d.getU32(), d.getU32(), d.getU32(); a != 1 || b != 2 || cc != 3 || g != 4 {
			t.Errorf("modifiers args = %d,%d,%d,%d", a, b, cc, g)
		}

		// destroy
		c.register(k.id, func(uint16, *decoder) error { return nil })
		if err := k.Destroy(); err != nil {
			t.Fatalf("Destroy: %v", err)
		}
		obj, op, _, _ = fs.readReq()
		if obj != k.id || op != vkbdReqDestroy {
			t.Errorf("destroy obj=%d op=%d", obj, op)
		}
		if _, ok := c.handlers[k.id]; ok {
			t.Error("Destroy should unregister the keyboard")
		}
	})
}

func TestVirtualKeyboardWriteErrors(t *testing.T) {
	c := NewConn(&stubTransport{writeErr: errors.New("nope")}, NativeOrder)
	m := &VirtualKeyboardManager{conn: c, id: 5}
	if _, err := m.CreateKeyboard(&Seat{conn: c, id: 9}); err == nil {
		t.Error("CreateKeyboard write error")
	}
	k := &VirtualKeyboard{conn: c, id: 6}
	if err := k.Keymap(KeymapFormatXkbV1, 0, 4); err == nil {
		t.Error("Keymap write error")
	}
	if err := k.Key(1, 30, StatePressed); err == nil {
		t.Error("Key write error")
	}
	if err := k.Modifiers(0, 0, 0, 0); err == nil {
		t.Error("Modifiers write error")
	}
	if err := k.Destroy(); err == nil {
		t.Error("Destroy write error")
	}
}

func TestVirtualPointerManagerBind(t *testing.T) {
	bothOrders(t, func(t *testing.T, order ByteOrder) {
		reg, st := vkMgrReg(order)
		m, err := reg.VirtualPointerManager()
		if err != nil {
			t.Fatalf("VirtualPointerManager: %v", err)
		}
		_, op, d := lastWrite(t, st, order)
		if op != registryReqBind {
			t.Errorf("bind opcode = %d", op)
		}
		name := d.getU32()
		iface := d.getString()
		ver := d.getU32()
		if name != 38 || iface != ifaceVirtualPointerManager || ver != 2 {
			t.Errorf("bind args name=%d iface=%q ver=%d", name, iface, ver)
		}
		if m.id == 0 {
			t.Error("manager id unset")
		}
	})
}

func TestVirtualPointerManagerNotAdvertised(t *testing.T) {
	reg := &Registry{conn: NewConn(&stubTransport{}, NativeOrder)}
	if _, err := reg.VirtualPointerManager(); err == nil {
		t.Error("missing manager global should error")
	}
}

func TestVirtualPointerManagerBindWriteError(t *testing.T) {
	c := NewConn(&stubTransport{writeErr: errors.New("nope")}, NativeOrder)
	reg := &Registry{conn: c, globals: []Global{{Name: 38, Interface: ifaceVirtualPointerManager, Version: 2}}}
	if _, err := reg.VirtualPointerManager(); err == nil {
		t.Error("bind write error should propagate")
	}
}

func TestVirtualPointerCreateAndRequests(t *testing.T) {
	bothOrders(t, func(t *testing.T, order ByteOrder) {
		st := &stubTransport{}
		c := NewConn(st, order)
		m := &VirtualPointerManager{conn: c, id: 5}
		seat := &Seat{conn: c, id: 9}
		p, err := m.CreatePointer(seat)
		if err != nil {
			t.Fatalf("CreatePointer: %v", err)
		}
		obj, op, d := lastWrite(t, st, order)
		if obj != 5 || op != vptrMgrReqCreate {
			t.Errorf("create obj=%d op=%d", obj, op)
		}
		if s, id := d.getU32(), d.getU32(); s != 9 || id != p.id {
			t.Errorf("create args seat=%d id=%d (want id %d)", s, id, p.id)
		}

		if err := p.MotionAbsolute(1, 200, 150, 800, 600); err != nil {
			t.Fatalf("MotionAbsolute: %v", err)
		}
		obj, op, d = lastWrite(t, st, order)
		if obj != p.id || op != vptrReqMotionAbsolute {
			t.Errorf("motion obj=%d op=%d", obj, op)
		}
		if tm, x, y, xe, ye := d.getU32(), d.getU32(), d.getU32(), d.getU32(), d.getU32(); tm != 1 || x != 200 || y != 150 || xe != 800 || ye != 600 {
			t.Errorf("motion args = %d,%d,%d,%d,%d", tm, x, y, xe, ye)
		}

		if err := p.Button(2, BtnLeft, StatePressed); err != nil {
			t.Fatalf("Button: %v", err)
		}
		obj, op, d = lastWrite(t, st, order)
		if obj != p.id || op != vptrReqButton {
			t.Errorf("button obj=%d op=%d", obj, op)
		}
		if tm, b, s := d.getU32(), d.getU32(), d.getU32(); tm != 2 || b != BtnLeft || s != StatePressed {
			t.Errorf("button args = %d,%d,%d", tm, b, s)
		}

		if err := p.Frame(); err != nil {
			t.Fatalf("Frame: %v", err)
		}
		obj, op, _ = lastWrite(t, st, order)
		if obj != p.id || op != vptrReqFrame {
			t.Errorf("frame obj=%d op=%d", obj, op)
		}

		c.register(p.id, func(uint16, *decoder) error { return nil })
		if err := p.Destroy(); err != nil {
			t.Fatalf("Destroy: %v", err)
		}
		obj, op, _ = lastWrite(t, st, order)
		if obj != p.id || op != vptrReqDestroy {
			t.Errorf("destroy obj=%d op=%d", obj, op)
		}
		if _, ok := c.handlers[p.id]; ok {
			t.Error("Destroy should unregister the pointer")
		}
	})
}

func TestVirtualPointerWriteErrors(t *testing.T) {
	c := NewConn(&stubTransport{writeErr: errors.New("nope")}, NativeOrder)
	m := &VirtualPointerManager{conn: c, id: 5}
	if _, err := m.CreatePointer(&Seat{conn: c, id: 9}); err == nil {
		t.Error("CreatePointer write error")
	}
	p := &VirtualPointer{conn: c, id: 6}
	if err := p.MotionAbsolute(0, 0, 0, 1, 1); err == nil {
		t.Error("MotionAbsolute write error")
	}
	if err := p.Button(0, BtnLeft, StatePressed); err == nil {
		t.Error("Button write error")
	}
	if err := p.Frame(); err == nil {
		t.Error("Frame write error")
	}
	if err := p.Destroy(); err == nil {
		t.Error("Destroy write error")
	}
}

// TestSeatOnCapabilities covers the capability callback added for dynamic
// input hot-plug: it fires with the new mask whenever capabilities change.
func TestSeatOnCapabilities(t *testing.T) {
	order := NativeOrder
	s := &Seat{}
	var got uint32
	var calls int
	s.OnCapabilities = func(caps uint32) { got, calls = caps, calls+1 }
	body := bodyOf(order, func(e *encoder) { e.putU32(SeatCapabilityKeyboard | SeatCapabilityPointer) })
	if err := s.handle(seatEvtCapabilities, newDecoder(order, body)); err != nil {
		t.Fatalf("handle: %v", err)
	}
	if calls != 1 || got != (SeatCapabilityKeyboard|SeatCapabilityPointer) {
		t.Errorf("OnCapabilities calls=%d caps=%#x", calls, got)
	}
}
