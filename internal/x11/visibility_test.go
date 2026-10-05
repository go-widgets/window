// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package x11

import (
	"encoding/binary"
	"testing"
)

// Every request here is read by a window manager that will not say it
// disagreed: a wrong mask, a wrong destination or a wrong data word is a window
// that simply does not move. So the bytes are asserted, not inferred from the
// absence of an error.

func TestUnmapWindowWire(t *testing.T) {
	order := binary.LittleEndian
	c, fc := dialFakeConn(t, order, nil)
	fc.out.Reset()
	if err := c.UnmapWindow(0xAA); err != nil {
		t.Fatal(err)
	}
	b := fc.out.Bytes()
	if op, _, total := parseReq(order, b); op != opUnmapWindow || total != 8 || len(b) != 8 {
		t.Fatalf("UnmapWindow = op %d, %d bytes (field %d), want op %d in 8", op, len(b), total, opUnmapWindow)
	}
	if got := order.Uint32(b[4:8]); got != 0xAA {
		t.Errorf("window = %#x, want 0xAA", got)
	}
}

func TestRaiseWindowWire(t *testing.T) {
	order := binary.LittleEndian
	c, fc := dialFakeConn(t, order, nil)
	fc.out.Reset()
	if err := c.RaiseWindow(0xAA); err != nil {
		t.Fatal(err)
	}
	b := fc.out.Bytes()
	if op, _, total := parseReq(order, b); op != opConfigureWindow || total != 16 || len(b) != 16 {
		t.Fatalf("RaiseWindow = op %d, %d bytes (field %d), want ConfigureWindow in 16", op, len(b), total)
	}
	if got := order.Uint32(b[4:8]); got != 0xAA {
		t.Errorf("window = %#x", got)
	}
	if got := order.Uint16(b[8:10]); got != configStackMode {
		t.Errorf("value-mask = %#x, want only stack-mode (%#x)", got, configStackMode)
	}
	if got := order.Uint32(b[12:16]); got != stackAbove {
		t.Errorf("stack-mode = %d, want Above", got)
	}
}

// rootEvent splits a SendEvent request into its destination, mask and event.
func rootEvent(t *testing.T, order binary.ByteOrder, b []byte) (dest, mask uint32, ev []byte) {
	t.Helper()
	if len(b) != 44 || b[0] != opSendEvent {
		t.Fatalf("not a SendEvent of 44 bytes: op %d, %d bytes", b[0], len(b))
	}
	if b[1] != 0 {
		t.Errorf("propagate = %d, want 0", b[1])
	}
	return order.Uint32(b[4:8]), order.Uint32(b[8:12]), b[12:44]
}

func TestWithdrawUnmapsThenTellsTheRoot(t *testing.T) {
	order := binary.LittleEndian
	c, fc := dialFakeConn(t, order, nil)
	fc.out.Reset()
	if err := c.Withdraw(0x123, 0xAA); err != nil {
		t.Fatal(err)
	}
	b := fc.out.Bytes()
	if len(b) != 8+44 || b[0] != opUnmapWindow {
		t.Fatalf("Withdraw wrote %d bytes starting with op %d, want an UnmapWindow then a SendEvent", len(b), b[0])
	}
	dest, mask, ev := rootEvent(t, order, b[8:])
	if dest != 0x123 {
		t.Errorf("destination = %#x, want the root", dest)
	}
	if mask != rootMessageMask {
		t.Errorf("mask = %#x, want SubstructureRedirect|SubstructureNotify", mask)
	}
	if ev[0] != evUnmapNotify {
		t.Errorf("event = %d, want UnmapNotify", ev[0])
	}
	if got := order.Uint32(ev[4:8]); got != 0x123 {
		t.Errorf("event window = %#x, want the root", got)
	}
	if got := order.Uint32(ev[8:12]); got != 0xAA {
		t.Errorf("unmapped window = %#x, want ours", got)
	}
	if ev[12] != 0 {
		t.Errorf("from-configure = %d, want false", ev[12])
	}
}

func TestRequestActivateWire(t *testing.T) {
	order := binary.LittleEndian
	c, fc := dialFakeConn(t, order, nil)
	fc.out.Reset()
	if err := c.RequestActivate(0x123, 0xAA, 0x77); err != nil {
		t.Fatal(err)
	}
	dest, mask, ev := rootEvent(t, order, fc.out.Bytes())
	if dest != 0x123 || mask != rootMessageMask {
		t.Errorf("sent to %#x with mask %#x, want the root with SubstructureRedirect|SubstructureNotify", dest, mask)
	}
	if ev[0] != evClientMessage || ev[1] != 32 {
		t.Fatalf("event = %d format %d, want a 32-bit ClientMessage", ev[0], ev[1])
	}
	if got := order.Uint32(ev[4:8]); got != 0xAA {
		t.Errorf("window = %#x, want the one to activate", got)
	}
	if got := order.Uint32(ev[8:12]); got != 0x77 {
		t.Errorf("type = %#x, want _NET_ACTIVE_WINDOW", got)
	}
	if got := order.Uint32(ev[12:16]); got != netSourceApplication {
		t.Errorf("source = %d, want 1 (an application)", got)
	}
	for i := 16; i < 32; i++ {
		if ev[i] != 0 {
			t.Errorf("event byte %d = %#x, want 0", i, ev[i])
		}
	}
}

func TestVisibilityRequestsReportAWriteError(t *testing.T) {
	c, fc := dialFakeConn(t, binary.LittleEndian, nil)
	fc.writeErr = errInjected
	for name, call := range map[string]func() error{
		"UnmapWindow":     func() error { return c.UnmapWindow(1) },
		"Withdraw":        func() error { return c.Withdraw(2, 1) },
		"RaiseWindow":     func() error { return c.RaiseWindow(1) },
		"RequestActivate": func() error { return c.RequestActivate(2, 1, 3) },
	} {
		if err := call(); err != errInjected {
			t.Errorf("%s = %v, want the write error", name, err)
		}
	}
}
