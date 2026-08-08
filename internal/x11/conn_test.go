// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package x11

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestHandshakeSuccessBothOrders(t *testing.T) {
	for _, order := range []ByteOrder{binary.LittleEndian, binary.BigEndian} {
		c, fc := dialFakeConn(t, order, nil)
		if c.Setup().Vendor != "Test" {
			t.Fatalf("vendor wrong")
		}
		if c.Order() != order {
			t.Fatalf("order not retained")
		}
		// The first byte written is the byte-order sentinel.
		sent := fc.out.Bytes()
		wantSentinel := byte(orderLSB)
		if order == binary.BigEndian {
			wantSentinel = orderMSB
		}
		if sent[0] != wantSentinel {
			t.Fatalf("sentinel = %c want %c", sent[0], wantSentinel)
		}
		if err := c.Close(); err != nil || !fc.closed {
			t.Fatalf("close failed")
		}
	}
}

func TestHandshakeRefused(t *testing.T) {
	order := binary.LittleEndian
	reason := "no way"
	body := make([]byte, pad4(len(reason)))
	copy(body, reason)
	hdr := make([]byte, 8)
	hdr[0] = 0 // Failed
	hdr[1] = byte(len(reason))
	order.PutUint16(hdr[6:8], uint16(len(body)/4))
	_, err := Handshake(newFakeConn(append(hdr, body...)), order, "", nil)
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte(reason)) {
		t.Fatalf("refused error missing reason: %v", err)
	}
}

func TestHandshakeAuthenticate(t *testing.T) {
	order := binary.LittleEndian
	reason := "auth\x00\x00\x00\x00"
	body := make([]byte, pad4(len(reason)))
	copy(body, reason)
	hdr := make([]byte, 8)
	hdr[0] = 2 // Authenticate
	order.PutUint16(hdr[6:8], uint16(len(body)/4))
	_, err := Handshake(newFakeConn(append(hdr, body...)), order, "", nil)
	if err == nil {
		t.Fatalf("authenticate should error")
	}
}

func TestHandshakeUnknownStatus(t *testing.T) {
	order := binary.LittleEndian
	hdr := make([]byte, 8)
	hdr[0] = 9
	if _, err := Handshake(newFakeConn(hdr), order, "", nil); err == nil {
		t.Fatalf("unknown status should error")
	}
}

func TestHandshakeWriteError(t *testing.T) {
	fc := newFakeConn(nil)
	fc.writeErr = errInjected
	if _, err := Handshake(fc, binary.LittleEndian, "", nil); err != errInjected {
		t.Fatalf("write error not propagated: %v", err)
	}
}

func TestHandshakeReadErrors(t *testing.T) {
	order := binary.LittleEndian
	// Header read fails (empty stream).
	if _, err := Handshake(newFakeConn(nil), order, "", nil); err == nil {
		t.Fatalf("header read should fail")
	}
	// Body read fails: header promises 5 units but no body follows.
	hdr := make([]byte, 8)
	hdr[0] = 1
	order.PutUint16(hdr[6:8], 5)
	if _, err := Handshake(newFakeConn(hdr), order, "", nil); err == nil {
		t.Fatalf("body read should fail")
	}
}

func TestHandshakeParseError(t *testing.T) {
	order := binary.LittleEndian
	// Success header, but the body claims one screen and provides none.
	e := newEncoder(order)
	e.put32(0)
	e.put32(0)
	e.put32(0)
	e.put32(0)
	e.put16(0)
	e.put16(0)
	e.put8(1) // one screen promised
	e.put8(0)
	e.skip(6)
	e.skip(4)
	body := e.buf
	// Pad body to 4-byte alignment.
	for len(body)%4 != 0 {
		body = append(body, 0)
	}
	hdr := make([]byte, 8)
	hdr[0] = 1
	order.PutUint16(hdr[6:8], uint16(len(body)/4))
	if _, err := Handshake(newFakeConn(append(hdr, body...)), order, "", nil); err == nil {
		t.Fatalf("parse error should propagate")
	}
}

func TestNewID(t *testing.T) {
	c, _ := dialFakeConn(t, binary.LittleEndian, nil)
	id0 := c.NewID()
	id1 := c.NewID()
	if id0 == id1 {
		t.Fatalf("ids should differ")
	}
	if id0&^c.setup.ResourceIDMask != c.setup.ResourceIDBase {
		t.Fatalf("id not within base|mask")
	}
}

// parseReq extracts opcode/data/length from a captured request.
func parseReq(order ByteOrder, b []byte) (opcode, data byte, total int) {
	return b[0], b[1], int(order.Uint16(b[2:4])) * 4
}

func TestRequestBuilders(t *testing.T) {
	order := binary.LittleEndian
	c, fc := dialFakeConn(t, order, nil)
	wid := c.NewID()

	type reqCase struct {
		name       string
		call       func() error
		wantOpcode byte
	}
	cases := []reqCase{
		{"CreateWindow", func() error {
			return c.CreateWindow(wid, testRootWin, 0, 0, 320, 240, 0, 0, DefaultEventMask)
		}, opCreateWindow},
		{"MapWindow", func() error { return c.MapWindow(wid) }, opMapWindow},
		{"CreateGC", func() error { return c.CreateGC(c.NewID(), wid) }, opCreateGC},
		{"SetWMName", func() error { return c.SetWMName(wid, "Title") }, opChangeProperty},
		{"SetWMClass", func() error { return c.SetWMClass(wid, "inst", "Class") }, opChangeProperty},
		{"SetWMProtocols", func() error { return c.SetWMProtocols(wid, 0x99, 0x9a) }, opChangeProperty},
	}
	for _, tc := range cases {
		fc.out.Reset()
		if err := tc.call(); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		b := fc.out.Bytes()
		op, _, total := parseReq(order, b)
		if op != tc.wantOpcode {
			t.Fatalf("%s opcode = %d want %d", tc.name, op, tc.wantOpcode)
		}
		if total != len(b) || len(b)%4 != 0 {
			t.Fatalf("%s length mismatch: field=%d actual=%d", tc.name, total, len(b))
		}
	}
}

func TestSendRequestWriteError(t *testing.T) {
	c, fc := dialFakeConn(t, binary.LittleEndian, nil)
	fc.writeErr = errInjected
	if err := c.MapWindow(c.NewID()); err != errInjected {
		t.Fatalf("write error not propagated: %v", err)
	}
}

func TestInternAtom(t *testing.T) {
	order := binary.LittleEndian
	var tail [24]byte
	order.PutUint32(tail[0:4], 0xBEEF) // atom in reply bytes 8..12
	reply := replyPacket(order, 0, 1, tail, nil)
	c, fc := dialFakeConn(t, order, reply)
	fc.out.Reset()
	atom, err := c.InternAtom("WM_PROTOCOLS", false)
	if err != nil {
		t.Fatalf("intern: %v", err)
	}
	if atom != 0xBEEF {
		t.Fatalf("atom = %#x", atom)
	}
	// The request carried only-if-exists=0 and the name.
	b := fc.out.Bytes()
	if b[0] != opInternAtom {
		t.Fatalf("opcode")
	}
	if !bytes.Contains(b, []byte("WM_PROTOCOLS")) {
		t.Fatalf("name not embedded")
	}
	// only-if-exists true path.
	c2, fc2 := dialFakeConn(t, order, reply)
	fc2.out.Reset()
	if _, err := c2.InternAtom("X", true); err != nil {
		t.Fatalf("intern true: %v", err)
	}
	if fc2.out.Bytes()[1] != 1 {
		t.Fatalf("only-if-exists byte not set")
	}
}

func TestInternAtomError(t *testing.T) {
	order := binary.LittleEndian
	errPkt := errorPacket(order, 5, 1, 0x1234, opInternAtom, 0)
	c, _ := dialFakeConn(t, order, errPkt)
	_, err := c.InternAtom("X", false)
	xe, ok := err.(*XError)
	if !ok {
		t.Fatalf("want *XError, got %T", err)
	}
	if xe.Code != 5 || xe.BadValue != 0x1234 || xe.Major != opInternAtom {
		t.Fatalf("decoded error wrong: %+v", xe)
	}
	if xe.Error() == "" {
		t.Fatalf("empty error string")
	}
}

func TestRoundTripWriteError(t *testing.T) {
	c, fc := dialFakeConn(t, binary.LittleEndian, nil)
	fc.writeErr = errInjected
	if _, err := c.InternAtom("X", false); err != errInjected {
		t.Fatalf("roundTrip write error not propagated: %v", err)
	}
}

func TestHandshakeAuthenticateNoNul(t *testing.T) {
	order := binary.LittleEndian
	reason := "auth" // 4 bytes, 4-aligned, contains no NUL
	hdr := make([]byte, 8)
	hdr[0] = 2 // Authenticate
	order.PutUint16(hdr[6:8], uint16(len(reason)/4))
	if _, err := Handshake(newFakeConn(append(hdr, []byte(reason)...)), order, "", nil); err == nil {
		t.Fatalf("authenticate should error")
	}
}

func TestRoundTripReadError(t *testing.T) {
	// No reply preloaded: readPacket hits EOF during the round trip.
	c, _ := dialFakeConn(t, binary.LittleEndian, nil)
	if _, err := c.InternAtom("X", false); err == nil {
		t.Fatalf("roundTrip read error should surface")
	}
}

func TestRoundTripBuffersEvents(t *testing.T) {
	order := binary.LittleEndian
	ev := pointerEvent(order, evButtonPress, Button1, testRootWin, 7, 8, 0)
	var tail [24]byte
	order.PutUint32(tail[0:4], 0xAB)
	reply := replyPacket(order, 0, 1, tail, nil)
	// Event arrives before the reply during the round trip.
	c, _ := dialFakeConn(t, order, append(ev, reply...))
	atom, err := c.InternAtom("A", false)
	if err != nil || atom != 0xAB {
		t.Fatalf("intern with interleaved event: %v atom=%#x", err, atom)
	}
	// The buffered event is delivered next.
	got, err := c.NextEvent()
	if err != nil || got.Code != evButtonPress || got.EventX != 7 {
		t.Fatalf("buffered event wrong: %+v err=%v", got, err)
	}
}

func TestGetKeyboardMapping(t *testing.T) {
	order := binary.LittleEndian
	// Reply: perCode=2, keysyms for keycodes 8..9.
	e := newEncoder(order)
	for _, ks := range []uint32{0x61, 0x41, ksReturn, 0} {
		e.put32(ks)
	}
	var tail [24]byte
	reply := replyPacket(order, 2, 1, tail, e.buf) // b1 = perCode
	c, _ := dialFakeConn(t, order, reply)
	km, err := c.FetchKeymap()
	if err != nil {
		t.Fatalf("fetch keymap: %v", err)
	}
	if km.Keysym(8, false) != 0x61 || km.Keysym(9, false) != ksReturn {
		t.Fatalf("keymap lookup wrong")
	}
}

func TestGetKeyboardMappingError(t *testing.T) {
	order := binary.LittleEndian
	c, _ := dialFakeConn(t, order, errorPacket(order, 1, 1, 0, opGetKeyboardMapping, 0))
	if _, err := c.FetchKeymap(); err == nil {
		t.Fatalf("keymap error should propagate")
	}
}

func TestNextEventStream(t *testing.T) {
	order := binary.LittleEndian
	ev1 := pointerEvent(order, evKeyPress, 38, testRootWin, 1, 2, 0)
	// A stray reply is skipped before the event.
	var tail [24]byte
	stray := replyPacket(order, 0, 3, tail, nil)
	c, _ := dialFakeConn(t, order, append(stray, ev1...))
	got, err := c.NextEvent()
	if err != nil || got.Code != evKeyPress {
		t.Fatalf("event after stray reply wrong: %+v err=%v", got, err)
	}
}

func TestNextEventError(t *testing.T) {
	order := binary.LittleEndian
	c, _ := dialFakeConn(t, order, errorPacket(order, 2, 1, 0, 0, 0))
	if _, err := c.NextEvent(); err == nil {
		t.Fatalf("error packet should surface")
	}
}

func TestNextEventReadError(t *testing.T) {
	c, fc := dialFakeConn(t, binary.LittleEndian, nil)
	fc.readErr = errInjected
	if _, err := c.NextEvent(); err == nil {
		t.Fatalf("read error should surface")
	}
}

func TestReadPacketReadErrorMidExtra(t *testing.T) {
	order := binary.LittleEndian
	// A reply header claiming 4 extra units, but no extra bytes follow.
	hdr := make([]byte, 32)
	hdr[0] = pktReply
	order.PutUint32(hdr[4:8], 1) // 1 unit = 4 bytes extra promised
	c, _ := dialFakeConn(t, order, hdr)
	if _, err := c.NextEvent(); err == nil {
		t.Fatalf("truncated reply extra should error")
	}
}

func TestSeq(t *testing.T) {
	c, _ := dialFakeConn(t, binary.LittleEndian, nil)
	if c.Seq() != 0 {
		t.Fatalf("initial seq")
	}
	_ = c.MapWindow(c.NewID())
	if c.Seq() != 1 {
		t.Fatalf("seq after one request = %d", c.Seq())
	}
}
