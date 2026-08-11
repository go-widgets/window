// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package x11

import (
	"encoding/binary"
	"testing"
	"time"
)

// A selection request is asserted on the WIRE, not on a round trip through our
// own decoder: the server is the reader, and a field in the wrong place is
// invisible to any test that only reads back what it wrote.
func TestSetSelectionOwnerWire(t *testing.T) {
	order := binary.LittleEndian
	c, fc := dialFakeConn(t, order, nil)
	fc.out.Reset()

	if err := c.SetSelectionOwner(0x111, 0x222, 0x333); err != nil {
		t.Fatalf("set owner: %v", err)
	}
	b := fc.out.Bytes()
	if b[0] != opSetSelectionOwner {
		t.Fatalf("opcode = %d, want %d", b[0], opSetSelectionOwner)
	}
	if got := order.Uint32(b[4:8]); got != 0x111 {
		t.Errorf("owner = %#x", got)
	}
	if got := order.Uint32(b[8:12]); got != 0x222 {
		t.Errorf("selection = %#x", got)
	}
	if got := order.Uint32(b[12:16]); got != 0x333 {
		t.Errorf("time = %#x", got)
	}
}

func TestSetSelectionOwnerWriteError(t *testing.T) {
	order := binary.LittleEndian
	c, fc := dialFakeConn(t, order, nil)
	fc.writeErr = errInjected
	if err := c.SetSelectionOwner(1, 2, CurrentTime); err == nil {
		t.Fatal("a write error was swallowed")
	}
}

// Nobody owning the selection is 0, and that is an ordinary answer — a fresh
// session with nothing copied — not a failure to report.
func TestGetSelectionOwner(t *testing.T) {
	order := binary.LittleEndian
	var tail [24]byte
	order.PutUint32(tail[0:4], 0xABCD)
	c, fc := dialFakeConn(t, order, replyPacket(order, 0, 1, tail, nil))
	fc.out.Reset()

	owner, err := c.GetSelectionOwner(0x55)
	if err != nil {
		t.Fatalf("get owner: %v", err)
	}
	if owner != 0xABCD {
		t.Errorf("owner = %#x, want 0xABCD", owner)
	}
	if b := fc.out.Bytes(); b[0] != opGetSelectionOwner || order.Uint32(b[4:8]) != 0x55 {
		t.Errorf("request = % x", b[:8])
	}
}

func TestGetSelectionOwnerError(t *testing.T) {
	order := binary.LittleEndian
	c, _ := dialFakeConn(t, order, errorPacket(order, 1, 1, 0, opGetSelectionOwner, 0))
	if _, err := c.GetSelectionOwner(1); err == nil {
		t.Fatal("a protocol error was swallowed")
	}
}

func TestConvertSelectionWire(t *testing.T) {
	order := binary.LittleEndian
	c, fc := dialFakeConn(t, order, nil)
	fc.out.Reset()

	if err := c.ConvertSelection(1, 2, 3, 4, 5); err != nil {
		t.Fatalf("convert: %v", err)
	}
	b := fc.out.Bytes()
	if b[0] != opConvertSelection {
		t.Fatalf("opcode = %d", b[0])
	}
	for i, want := range []uint32{1, 2, 3, 4, 5} {
		if got := order.Uint32(b[4+i*4 : 8+i*4]); got != want {
			t.Errorf("field %d = %d, want %d", i, got, want)
		}
	}
}

func TestConvertSelectionWriteError(t *testing.T) {
	order := binary.LittleEndian
	c, fc := dialFakeConn(t, order, nil)
	fc.writeErr = errInjected
	if err := c.ConvertSelection(1, 2, 3, 4, 5); err == nil {
		t.Fatal("a write error was swallowed")
	}
}

// GetProperty reports its length in FORMAT units, so 8-, 16- and 32-bit
// properties of the same element count carry different numbers of bytes.
// Reading that wrong truncates a paste or reads past the reply.
func TestGetPropertyFormats(t *testing.T) {
	order := binary.LittleEndian
	for _, tc := range []struct {
		name    string
		format  byte
		n       uint32
		payload []byte
		want    int
	}{
		{"8-bit text", 8, 5, []byte("hello\x00\x00\x00"), 5},
		{"16-bit", 16, 2, []byte{1, 2, 3, 4}, 4},
		{"32-bit atoms", 32, 2, []byte{1, 2, 3, 4, 5, 6, 7, 8}, 8},
		{"absent property", 0, 0, nil, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var tail [24]byte
			order.PutUint32(tail[0:4], 0x777) // type
			order.PutUint32(tail[8:12], tc.n) // value length, in format units
			reply := replyPacket(order, tc.format, 1, tail, tc.payload)
			c, fc := dialFakeConn(t, order, reply)
			fc.out.Reset()

			typ, format, data, err := c.GetProperty(0x10, 0x20, 0, true, 1024)
			if err != nil {
				t.Fatalf("get property: %v", err)
			}
			if typ != 0x777 || format != tc.format {
				t.Errorf("type/format = %#x/%d", typ, format)
			}
			if len(data) != tc.want {
				t.Errorf("read %d bytes, want %d", len(data), tc.want)
			}
			if tc.name == "8-bit text" && string(data) != "hello" {
				t.Errorf("data = %q", data)
			}
			// delete=1 rides in the request's data byte.
			if b := fc.out.Bytes(); b[1] != 1 {
				t.Errorf("delete flag = %d, want 1", b[1])
			}
		})
	}
}

// A reply claiming more data than it carries is a broken server, and must be
// clamped rather than sliced past the end of the packet.
func TestGetPropertyClampsALyingLength(t *testing.T) {
	order := binary.LittleEndian
	var tail [24]byte
	order.PutUint32(tail[8:12], 9999) // claims far more than the payload
	reply := replyPacket(order, 8, 1, tail, []byte("short___"))
	c, _ := dialFakeConn(t, order, reply)

	_, _, data, err := c.GetProperty(1, 2, 0, false, 16)
	if err != nil {
		t.Fatalf("get property: %v", err)
	}
	if len(data) != 8 {
		t.Errorf("read %d bytes from an 8-byte payload", len(data))
	}
}

func TestGetPropertyError(t *testing.T) {
	order := binary.LittleEndian
	c, _ := dialFakeConn(t, order, errorPacket(order, 1, 1, 0, opGetProperty, 0))
	if _, _, _, err := c.GetProperty(1, 2, 0, false, 1); err == nil {
		t.Fatal("a protocol error was swallowed")
	}
}

// The reply to a SelectionRequest is an event the CLIENT sends, which is the
// only place this package writes an event rather than reading one — so the
// 32-byte body is asserted field by field.
func TestSendSelectionNotifyWire(t *testing.T) {
	order := binary.LittleEndian
	c, fc := dialFakeConn(t, order, nil)
	fc.out.Reset()

	if err := c.SendSelectionNotify(0xAA, 0xBB, 0xCC, 0xDD, 0xEE); err != nil {
		t.Fatalf("send notify: %v", err)
	}
	b := fc.out.Bytes()
	if b[0] != opSendEvent {
		t.Fatalf("opcode = %d, want %d", b[0], opSendEvent)
	}
	if b[1] != 0 {
		t.Errorf("propagate = %d, want 0", b[1])
	}
	if got := order.Uint32(b[4:8]); got != 0xAA {
		t.Errorf("destination = %#x, want the requestor", got)
	}
	if got := order.Uint32(b[8:12]); got != 0 {
		t.Errorf("event-mask = %#x, want 0 (deliver to the requestor itself)", got)
	}
	ev := b[12:44]
	if ev[0] != evSelectionNotify {
		t.Errorf("event code = %d, want %d", ev[0], evSelectionNotify)
	}
	for i, want := range []uint32{0xEE, 0xAA, 0xBB, 0xCC, 0xDD} { // time, requestor, selection, target, property
		if got := order.Uint32(ev[4+i*4 : 8+i*4]); got != want {
			t.Errorf("event field %d = %#x, want %#x", i, got, want)
		}
	}
	if len(ev) != 32 {
		t.Errorf("event body is %d bytes, want 32", len(ev))
	}
}

func TestSendSelectionNotifyWriteError(t *testing.T) {
	order := binary.LittleEndian
	c, fc := dialFakeConn(t, order, nil)
	fc.writeErr = errInjected
	if err := c.SendSelectionNotify(1, 2, 3, 4, 5); err == nil {
		t.Fatal("a write error was swallowed")
	}
}

// A refusal carries property 0, which is how an owner says "I cannot produce
// that target" — and is the answer a requestor otherwise waits forever for.
func TestSendSelectionNotifyCanRefuse(t *testing.T) {
	order := binary.LittleEndian
	c, fc := dialFakeConn(t, order, nil)
	fc.out.Reset()
	if err := c.SendSelectionNotify(0xAA, 0xBB, 0xCC, 0, CurrentTime); err != nil {
		t.Fatalf("send refusal: %v", err)
	}
	// The event body starts at byte 12; within it property is the fifth 32-bit
	// field, at offset 20 — reading 16 gets the TARGET, which is how this test
	// first failed.
	if got := order.Uint32(fc.out.Bytes()[12+20 : 12+24]); got != 0 {
		t.Errorf("property = %#x in a refusal, want 0", got)
	}
}

func TestDecodeSelectionEvents(t *testing.T) {
	order := binary.LittleEndian

	req := make([]byte, 32)
	req[0] = evSelectionRequest
	order.PutUint32(req[4:8], 0x11)   // time
	order.PutUint32(req[8:12], 0x22)  // owner
	order.PutUint32(req[12:16], 0x33) // requestor
	order.PutUint32(req[16:20], 0x44) // selection
	order.PutUint32(req[20:24], 0x55) // target
	order.PutUint32(req[24:28], 0x66) // property
	ev := decodeEvent(order, req)
	if ev.Code != evSelectionRequest || ev.Window != 0x22 || ev.Requestor != 0x33 ||
		ev.Selection != 0x44 || ev.Target != 0x55 || ev.Property != 0x66 {
		t.Errorf("SelectionRequest decoded as %+v", ev)
	}

	not := make([]byte, 32)
	not[0] = evSelectionNotify
	order.PutUint32(not[4:8], 0x77)   // time
	order.PutUint32(not[8:12], 0x88)  // requestor
	order.PutUint32(not[12:16], 0x99) // selection
	order.PutUint32(not[16:20], 0xAA) // target
	order.PutUint32(not[20:24], 0)    // property: refused
	ev = decodeEvent(order, not)
	if ev.Requestor != 0x88 || ev.Window != 0x88 || ev.Selection != 0x99 ||
		ev.Target != 0xAA || ev.Property != 0 {
		t.Errorf("SelectionNotify decoded as %+v", ev)
	}

	clr := make([]byte, 32)
	clr[0] = evSelectionClear
	order.PutUint32(clr[4:8], 0xB1)   // time
	order.PutUint32(clr[8:12], 0xB2)  // owner losing it
	order.PutUint32(clr[12:16], 0xB3) // selection
	ev = decodeEvent(order, clr)
	if ev.Window != 0xB2 || ev.Selection != 0xB3 {
		t.Errorf("SelectionClear decoded as %+v", ev)
	}
}

// An exchange that reads events which are not its reply must give them back:
// dropping one loses a click, and the application never sees it happen.
func TestPushEventReturnsItToTheHead(t *testing.T) {
	order := binary.LittleEndian
	c, _ := dialFakeConn(t, order, nil)

	c.PushEvent(Event{Code: 12, Window: 0xB})
	c.PushEvent(Event{Code: 4, Window: 0xA}) // pushed second, delivered first

	first, err := c.NextEvent()
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if first.Code != 4 || first.Window != 0xA {
		t.Errorf("first = %+v, want the most recently pushed", first)
	}
	second, err := c.NextEvent()
	if err != nil {
		t.Fatalf("next: %v", err)
	}
	if second.Code != 12 || second.Window != 0xB {
		t.Errorf("second = %+v", second)
	}
}

// A transport that cannot answer says so, rather than reporting "not ready" and
// making every paste look like a dead owner.
func TestWaitReadableUnsupportedTransport(t *testing.T) {
	order := binary.LittleEndian
	c, _ := dialFakeConn(t, order, nil)
	ready, supported := c.WaitReadable(time.Millisecond)
	if supported {
		t.Error("the in-memory fake claimed it could wait on a socket")
	}
	if ready {
		t.Error("an unsupported wait reported ready")
	}
}
