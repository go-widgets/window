// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package x11

import (
	"encoding/binary"
	"testing"

	xproto "github.com/go-freedesktop/x11"
)

// The RANDR and XINERAMA wire formats are tested where they now live, in
// github.com/go-freedesktop/x11. What is left to prove here is that THIS
// package's request/reply machine satisfies what that enumeration asks of it,
// over the real packet path, and that _NET_WORKAREA — the one piece that is a
// windowing question rather than a display one — is read the way a window
// manager writes it.

// randrScript builds the server's side of a whole Monitors() exchange: the
// extension gate, the version negotiation, one monitor, its name, and the
// EDID lookup that follows.
func randrScript(order ByteOrder) []byte {
	var script []byte
	add := func(b []byte) { script = append(script, b...) }

	var tail [24]byte
	tail[0] = 1   // present
	tail[1] = 140 // RANDR major opcode
	add(replyPacket(order, 0, 1, tail, nil))

	tail = [24]byte{}
	order.PutUint32(tail[0:4], 1) // version 1.5
	order.PutUint32(tail[4:8], 5)
	add(replyPacket(order, 0, 2, tail, nil))

	mon := xproto.NewEncoder(order)
	mon.Put32(0x40) // name atom
	mon.Put8(1)     // primary
	mon.Put8(0)     // automatic
	mon.Put16(1)    // one output
	mon.Put16(0)    // x
	mon.Put16(0)    // y
	mon.Put16(3840)
	mon.Put16(2160)
	mon.Put32(597)
	mon.Put32(336)
	mon.Put32(0x42)
	tail = [24]byte{}
	order.PutUint32(tail[0:4], 12345) // timestamp
	order.PutUint32(tail[4:8], 1)     // one monitor
	order.PutUint32(tail[8:12], 1)    // one output
	add(replyPacket(order, 0, 3, tail, mon.Bytes()))

	name := xproto.NewEncoder(order)
	name.PutString("DP-2")
	tail = [24]byte{}
	order.PutUint16(tail[0:2], 4)
	add(replyPacket(order, 0, 4, tail, name.Bytes()))

	tail = [24]byte{}
	order.PutUint32(tail[0:4], 0x51) // the EDID atom
	add(replyPacket(order, 0, 5, tail, nil))

	edid := make([]byte, 128)
	copy(edid, []byte{0x00, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x00})
	d := edid[54:72]
	d[3] = 0xfc
	copy(d[5:], "DELL U2720Q\n ")
	tail = [24]byte{}
	order.PutUint32(tail[0:4], 19) // INTEGER
	order.PutUint32(tail[8:12], uint32(len(edid)))
	add(replyPacket(order, 8, 6, tail, edid))

	return script
}

func TestMonitorsGoesOverTheWire(t *testing.T) {
	for _, order := range []ByteOrder{binary.LittleEndian, binary.BigEndian} {
		c, _ := dialFakeConn(t, order, randrScript(order))
		mons, err := c.Monitors(0)
		if err != nil {
			t.Fatalf("Monitors: %v", err)
		}
		if len(mons) != 1 {
			t.Fatalf("got %d monitors, want 1", len(mons))
		}
		m := mons[0]
		if m.Name != "DP-2" || m.Model != "DELL U2720Q" || !m.Primary ||
			m.Width != 3840 || m.Height != 2160 {
			t.Fatalf("got %+v, want DP-2 / DELL U2720Q / primary / 3840x2160", m)
		}
		// The connector is what RANDR states; the model is what the panel
		// calls itself, and it is the one a user recognises.
		if m.DisplayName() != "DELL U2720Q" {
			t.Errorf("DisplayName() = %q, want the model", m.DisplayName())
		}
	}
}

func TestMonitorsRefusesAScreenThatDoesNotExist(t *testing.T) {
	order := binary.LittleEndian
	c, _ := dialFakeConn(t, order, nil)
	if _, err := c.Monitors(4); err == nil {
		t.Fatal("Monitors accepted screen 4 of a one-screen server")
	}
}

func TestRequestNamesTheFailedRequest(t *testing.T) {
	order := binary.LittleEndian
	c, _ := dialFakeConn(t, order, errorPacket(order, 1, 1, 0, 140, 42))
	_, err := c.Request("RRGetMonitors", 140, 42, nil)
	if err == nil {
		t.Fatal("an error reply came back as a reply")
	}
	// A bare opcode says nothing to whoever reads the log; the name does.
	if got := err.Error(); got == "" || !contains(got, "RRGetMonitors") {
		t.Errorf("error %q does not name the request", got)
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// cardinalReply builds a GetProperty reply carrying 32-bit CARDINALs, which is
// how EWMH publishes every one of its geometry properties.
func cardinalReply(order ByteOrder, seq uint16, values ...uint32) []byte {
	data := make([]byte, 4*len(values))
	for i, v := range values {
		order.PutUint32(data[i*4:], v)
	}
	var tail [24]byte
	order.PutUint32(tail[0:4], AtomCardinal)
	order.PutUint32(tail[8:12], uint32(len(values)))
	return replyPacket(order, 32, seq, tail, data)
}

func TestWorkAreaReadsTheCurrentDesktop(t *testing.T) {
	order := binary.LittleEndian
	var script []byte
	script = append(script, internReplyPkt(order, 1, 0x60)...) // _NET_WORKAREA
	// Two desktops: the second one has a dock down the left-hand edge.
	script = append(script, cardinalReply(order, 2,
		0, 27, 1920, 1053,
		96, 27, 1824, 1053)...)
	script = append(script, internReplyPkt(order, 3, 0x61)...) // _NET_CURRENT_DESKTOP
	script = append(script, cardinalReply(order, 4, 1)...)

	c, _ := dialFakeConn(t, order, script)
	x, y, w, h, ok := c.WorkArea(testRootWin)
	if !ok {
		t.Fatal("WorkArea reported nothing for a desktop that publishes it")
	}
	if x != 96 || y != 27 || w != 1824 || h != 1053 {
		t.Errorf("work area = %d,%d %dx%d, want the SECOND desktop's 96,27 1824x1053", x, y, w, h)
	}
}

func TestWorkAreaWithOneDesktopAsksNoFurther(t *testing.T) {
	order := binary.LittleEndian
	var script []byte
	script = append(script, internReplyPkt(order, 1, 0x60)...)
	script = append(script, cardinalReply(order, 2, 0, 27, 1920, 1053)...)
	// Nothing follows: a second lookup would run off the end of the script and
	// fail, which is the assertion — one desktop leaves nothing to choose.
	c, _ := dialFakeConn(t, order, script)
	x, y, w, h, ok := c.WorkArea(testRootWin)
	if !ok || x != 0 || y != 27 || w != 1920 || h != 1053 {
		t.Errorf("work area = %d,%d %dx%d ok=%v, want 0,27 1920x1053 ok=true", x, y, w, h, ok)
	}
}

func TestWorkAreaAcceptsANegativeOrigin(t *testing.T) {
	// A screen laid out to the left of the primary one has a negative x, and
	// the property is CARDINAL, so the sign only survives a two's-complement
	// read. Reading it unsigned would put the work area 4 billion pixels away.
	order := binary.LittleEndian
	var script []byte
	script = append(script, internReplyPkt(order, 1, 0x60)...)
	script = append(script, cardinalReply(order, 2, negative(1920), 0, 3840, 1080)...)
	c, _ := dialFakeConn(t, order, script)
	x, _, _, _, ok := c.WorkArea(testRootWin)
	if !ok || x != -1920 {
		t.Errorf("work area x = %d ok=%v, want -1920", x, ok)
	}
}

func TestWorkAreaWhenNobodyPublishesIt(t *testing.T) {
	order := binary.LittleEndian
	for _, tc := range []struct {
		name   string
		script []byte
	}{
		{"no such atom on the server", internReplyPkt(order, 1, AtomNone)},
		{"the intern fails", nil},
		{"the property read fails", internReplyPkt(order, 1, 0x60)},
		{"a property of the wrong format", append(internReplyPkt(order, 1, 0x60),
			replyPacket(order, 8, 2, [24]byte{}, nil)...)},
		{"a property too short to be a rectangle", append(internReplyPkt(order, 1, 0x60),
			cardinalReply(order, 2, 1, 2)...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := dialFakeConn(t, order, tc.script)
			if _, _, _, _, ok := c.WorkArea(testRootWin); ok {
				t.Error("WorkArea claimed an area a bare server does not publish")
			}
		})
	}
}

func TestWorkAreaFallsBackToTheFirstDesktop(t *testing.T) {
	order := binary.LittleEndian
	for _, tc := range []struct {
		name string
		tail []byte
	}{
		{"no _NET_CURRENT_DESKTOP atom", internReplyPkt(order, 3, AtomNone)},
		{"the intern fails", nil},
		{"the read fails", internReplyPkt(order, 3, 0x61)},
		{"a desktop index of the wrong format", append(internReplyPkt(order, 3, 0x61),
			replyPacket(order, 8, 4, [24]byte{}, nil)...)},
		{"a desktop index past the end of the list", append(internReplyPkt(order, 3, 0x61),
			cardinalReply(order, 4, 99)...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var script []byte
			script = append(script, internReplyPkt(order, 1, 0x60)...)
			script = append(script, cardinalReply(order, 2,
				0, 27, 1920, 1053,
				96, 27, 1824, 1053)...)
			script = append(script, tc.tail...)
			c, _ := dialFakeConn(t, order, script)
			x, _, w, _, ok := c.WorkArea(testRootWin)
			if !ok || x != 0 || w != 1920 {
				t.Errorf("work area = x %d w %d ok=%v, want the first desktop's 0/1920", x, w, ok)
			}
		})
	}
}

// negative encodes -n the way a CARDINAL property carries it: two's
// complement, which is what an X client has to read back to place a screen to
// the left of the origin.
func negative(n int32) uint32 { return uint32(-n) }

// internReplyPkt builds an InternAtom reply.
func internReplyPkt(order ByteOrder, seq uint16, atom uint32) []byte {
	var tail [24]byte
	order.PutUint32(tail[0:4], atom)
	return replyPacket(order, 0, seq, tail, nil)
}
