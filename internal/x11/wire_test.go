// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package x11

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestPad4(t *testing.T) {
	cases := []struct{ in, want int }{{0, 0}, {1, 4}, {3, 4}, {4, 4}, {5, 8}, {7, 8}, {8, 8}}
	for _, c := range cases {
		if got := pad4(c.in); got != c.want {
			t.Errorf("pad4(%d)=%d want %d", c.in, got, c.want)
		}
		if got := padding(c.in); got != c.want-c.in {
			t.Errorf("padding(%d)=%d want %d", c.in, got, c.want-c.in)
		}
	}
}

func TestEncoderRoundTripBothOrders(t *testing.T) {
	for _, order := range []ByteOrder{binary.LittleEndian, binary.BigEndian} {
		e := newEncoder(order)
		e.put8(0xAB)
		e.put16(0x1234)
		e.put32(0xDEADBEEF)
		e.putBytes([]byte{1, 2})
		e.putString("hi")   // 2 bytes + 2 pad
		e.skip(1)           // one zero
		e.pad(1)            // 3 pad after 1 written byte -> here uses len arg
		d := newDecoder(order, e.buf)
		if d.get8() != 0xAB {
			t.Fatal("get8")
		}
		if d.get16() != 0x1234 {
			t.Fatal("get16")
		}
		if d.get32() != 0xDEADBEEF {
			t.Fatal("get32")
		}
		if !bytes.Equal(d.getBytes(2), []byte{1, 2}) {
			t.Fatal("getBytes")
		}
		if d.getString(2) != "hi" {
			t.Fatal("getString")
		}
		d.skip(1) // the skip(1) zero
		// the pad(1) wrote padding(1)=3 zero bytes
		if remaining := len(d.buf) - d.off; remaining != 3 {
			t.Fatalf("remaining=%d want 3", remaining)
		}
		if !d.ok {
			t.Fatal("decoder should still be ok")
		}
	}
}

func TestDecoderTruncation(t *testing.T) {
	d := newDecoder(binary.LittleEndian, []byte{1})
	if d.get16() != 0 || d.ok {
		t.Fatal("get16 past end should zero + clear ok")
	}
	// Once not ok, every read is a no-op returning zero.
	if d.get8() != 0 || d.get32() != 0 {
		t.Fatal("reads after !ok must be zero")
	}
	if b := d.getBytes(1); b != nil {
		t.Fatal("getBytes past end should be nil")
	}
	// getString past end.
	d2 := newDecoder(binary.LittleEndian, []byte{})
	if d2.getString(4) != "" {
		t.Fatal("getString past end should be empty")
	}
	// skip past end clamps.
	d3 := newDecoder(binary.LittleEndian, []byte{1, 2})
	d3.skip(9)
	if d3.ok {
		t.Fatal("skip past end should clear ok")
	}
	// need on already-not-ok decoder short-circuits.
	if d3.need(1) {
		t.Fatal("need on !ok should be false")
	}
}

func TestOrderFor(t *testing.T) {
	if o, ok := orderFor('l'); !ok || o != binary.LittleEndian {
		t.Fatal("l -> little")
	}
	if o, ok := orderFor('B'); !ok || o != binary.BigEndian {
		t.Fatal("B -> big")
	}
	if _, ok := orderFor('x'); ok {
		t.Fatal("bad sentinel should be !ok")
	}
}

func TestReadFull(t *testing.T) {
	buf := make([]byte, 3)
	if err := readFull(bytes.NewReader([]byte{1, 2, 3}), buf); err != nil {
		t.Fatalf("readFull: %v", err)
	}
	if err := readFull(bytes.NewReader([]byte{1}), buf); err == nil {
		t.Fatal("readFull short should error")
	}
}
