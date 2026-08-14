// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package x11

import (
	"encoding/binary"
	"testing"
)

// The wakeup on the wire. A ClientMessage a server rejects wakes nothing, and
// the failure would look like an application that simply never updates — so the
// layout is asserted byte by byte rather than inferred from the fact that
// nothing errored.
func TestSendClientMessageWire(t *testing.T) {
	order := binary.LittleEndian
	c, fc := dialFakeConn(t, order, nil)
	fc.out.Reset()

	if err := c.SendClientMessage(0xAA, 0xBB, 0xCC); err != nil {
		t.Fatalf("send: %v", err)
	}
	b := fc.out.Bytes()
	if len(b) != 44 {
		t.Fatalf("wrote %d bytes, want 44 (4 header + 4 destination + 4 mask + 32 event)", len(b))
	}
	if b[0] != opSendEvent {
		t.Fatalf("opcode = %d, want %d", b[0], opSendEvent)
	}
	if b[1] != 0 {
		t.Errorf("propagate = %d, want 0", b[1])
	}
	if got := order.Uint16(b[2:4]); got != 11 {
		t.Errorf("request length = %d words, want 11", got)
	}
	if got := order.Uint32(b[4:8]); got != 0xAA {
		t.Errorf("destination = %#x, want the window", got)
	}
	// Zero is what makes this a message to ourselves: the server delivers an
	// unmasked SendEvent to the window's own client. A non-zero mask would send
	// it to whoever selected for that mask instead, which for a wakeup means
	// nobody.
	if got := order.Uint32(b[8:12]); got != 0 {
		t.Errorf("event-mask = %#x, want 0 (deliver to the window's own client)", got)
	}

	ev := b[12:44]
	if ev[0] != evClientMessage {
		t.Errorf("event code = %d, want %d", ev[0], evClientMessage)
	}
	if ev[1] != 32 {
		t.Errorf("format = %d, want 32", ev[1])
	}
	if got := order.Uint32(ev[4:8]); got != 0xAA {
		t.Errorf("event window = %#x, want the window", got)
	}
	if got := order.Uint32(ev[8:12]); got != 0xBB {
		t.Errorf("message type = %#x, want the atom", got)
	}
	if got := order.Uint32(ev[12:16]); got != 0xCC {
		t.Errorf("data word 0 = %#x, want the data", got)
	}
	// The four remaining data words are padding, and must be zero rather than
	// whatever the encoder had lying around.
	for i := 16; i < 32; i++ {
		if ev[i] != 0 {
			t.Errorf("event byte %d = %#x, want 0", i, ev[i])
		}
	}
}

func TestSendClientMessageWriteError(t *testing.T) {
	c, fc := dialFakeConn(t, binary.LittleEndian, nil)
	fc.writeErr = errInjected
	if err := c.SendClientMessage(1, 2, 3); err == nil {
		t.Fatal("a write error was swallowed, so a caller cannot know its wakeup never left")
	}
}
