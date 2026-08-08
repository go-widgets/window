// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package x11

import (
	"encoding/binary"
	"testing"
)

// pointerEvent frames a shared key/button/motion event.
func pointerEvent(order ByteOrder, code, detail byte, win uint32, ex, ey int16, state uint16) []byte {
	pkt := make([]byte, 32)
	pkt[0] = code
	pkt[1] = detail
	order.PutUint16(pkt[2:4], 1)     // seq
	order.PutUint32(pkt[4:8], 42)    // time
	order.PutUint32(pkt[8:12], 0)    // root
	order.PutUint32(pkt[12:16], win) // event window
	order.PutUint32(pkt[16:20], 0)   // child
	order.PutUint16(pkt[20:22], 100) // root-x
	order.PutUint16(pkt[22:24], 200) // root-y
	order.PutUint16(pkt[24:26], uint16(ex))
	order.PutUint16(pkt[26:28], uint16(ey))
	order.PutUint16(pkt[28:30], state) // state
	return pkt
}

func TestDecodeEventPointer(t *testing.T) {
	order := binary.LittleEndian
	ev := decodeEvent(order, pointerEvent(order, evButtonPress, Button1, testRootWin, 33, 44, ModControl))
	if ev.Code != evButtonPress || ev.Detail != Button1 {
		t.Fatalf("code/detail wrong: %+v", ev)
	}
	if ev.EventX != 33 || ev.EventY != 44 {
		t.Fatalf("coords wrong: %+v", ev)
	}
	if ev.State != ModControl || ev.Window != testRootWin || ev.Time != 42 {
		t.Fatalf("state/window/time wrong: %+v", ev)
	}
	if ev.Synth {
		t.Fatalf("should not be synthetic")
	}
	// Negative coordinates decode as signed.
	ev = decodeEvent(order, pointerEvent(order, evMotionNotify, 0, testRootWin, -5, -9, 0))
	if ev.EventX != -5 || ev.EventY != -9 {
		t.Fatalf("signed coords wrong: %+v", ev)
	}
}

func TestDecodeEventSynthBit(t *testing.T) {
	order := binary.LittleEndian
	pkt := pointerEvent(order, evKeyPress|sendEventBit, 38, testRootWin, 1, 1, 0)
	ev := decodeEvent(order, pkt)
	if !ev.Synth || ev.Code != evKeyPress {
		t.Fatalf("synth bit not handled: %+v", ev)
	}
}

func TestDecodeEventExpose(t *testing.T) {
	order := binary.BigEndian
	pkt := make([]byte, 32)
	pkt[0] = evExpose
	order.PutUint16(pkt[2:4], 7)
	order.PutUint32(pkt[4:8], testRootWin)
	order.PutUint16(pkt[8:10], 10)  // x
	order.PutUint16(pkt[10:12], 20) // y
	order.PutUint16(pkt[12:14], 30) // width
	order.PutUint16(pkt[14:16], 40) // height
	order.PutUint16(pkt[16:18], 2)  // count
	ev := decodeEvent(order, pkt)
	if ev.Code != evExpose || ev.X != 10 || ev.Y != 20 || ev.Width != 30 || ev.Height != 40 || ev.Count != 2 {
		t.Fatalf("expose decode wrong: %+v", ev)
	}
}

func TestDecodeEventConfigureNotify(t *testing.T) {
	order := binary.LittleEndian
	pkt := make([]byte, 32)
	pkt[0] = evConfigureNotify
	order.PutUint32(pkt[4:8], 0)            // event
	order.PutUint32(pkt[8:12], testRootWin) // window
	order.PutUint32(pkt[12:16], 0)          // above-sibling
	order.PutUint16(pkt[16:18], uint16(int16(5)))
	order.PutUint16(pkt[18:20], uint16(int16(6)))
	order.PutUint16(pkt[20:22], 640) // width
	order.PutUint16(pkt[22:24], 480) // height
	ev := decodeEvent(order, pkt)
	if ev.Code != evConfigureNotify || ev.Width != 640 || ev.Height != 480 || ev.X != 5 || ev.Y != 6 || ev.Window != testRootWin {
		t.Fatalf("configure decode wrong: %+v", ev)
	}
}

func TestDecodeEventClientMessage(t *testing.T) {
	order := binary.LittleEndian
	pkt := make([]byte, 32)
	pkt[0] = evClientMessage
	pkt[1] = 32                            // format
	order.PutUint32(pkt[4:8], testRootWin) // window
	order.PutUint32(pkt[8:12], 0x77)       // message type atom
	order.PutUint32(pkt[12:16], 0x88)      // data[0]
	ev := decodeEvent(order, pkt)
	if ev.Code != evClientMessage || ev.Format != 32 || ev.Atom != 0x77 || ev.Data32 != 0x88 {
		t.Fatalf("client message decode wrong: %+v", ev)
	}
}

func TestDecodeEventOther(t *testing.T) {
	// An event code we don't special-case decodes header only.
	order := binary.LittleEndian
	pkt := make([]byte, 32)
	pkt[0] = evMapNotify
	order.PutUint16(pkt[2:4], 3)
	ev := decodeEvent(order, pkt)
	if ev.Code != evMapNotify || ev.Seq != 3 {
		t.Fatalf("other event header wrong: %+v", ev)
	}
}
