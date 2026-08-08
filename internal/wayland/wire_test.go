// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package wayland

import (
	"encoding/binary"
	"testing"
)

func TestPad(t *testing.T) {
	cases := []struct{ n, wantPad4, wantPadding int }{
		{0, 0, 0}, {1, 4, 3}, {2, 4, 2}, {3, 4, 1}, {4, 4, 0}, {5, 8, 3}, {8, 8, 0},
	}
	for _, c := range cases {
		if got := pad4(c.n); got != c.wantPad4 {
			t.Errorf("pad4(%d) = %d, want %d", c.n, got, c.wantPad4)
		}
		if got := padding(c.n); got != c.wantPadding {
			t.Errorf("padding(%d) = %d, want %d", c.n, got, c.wantPadding)
		}
	}
}

func TestFixed(t *testing.T) {
	if got := FixedFromInt(3); got != 768 {
		t.Fatalf("FixedFromInt(3) = %d, want 768", got)
	}
	if got := FixedFromInt(3).Int(); got != 3 {
		t.Fatalf("FixedFromInt(3).Int() = %d, want 3", got)
	}
	if got := FixedFromFloat(2.5); got != 640 {
		t.Fatalf("FixedFromFloat(2.5) = %d, want 640", got)
	}
	if got := FixedFromFloat(2.5).Float(); got != 2.5 {
		t.Fatalf("FixedFromFloat(2.5).Float() = %v, want 2.5", got)
	}
	// Negative values round-trip (arithmetic shift keeps the sign).
	if got := FixedFromInt(-2).Int(); got != -2 {
		t.Fatalf("FixedFromInt(-2).Int() = %d, want -2", got)
	}
	if got := FixedFromFloat(-1.25).Float(); got != -1.25 {
		t.Fatalf("FixedFromFloat(-1.25).Float() = %v, want -1.25", got)
	}
}

func TestEncodeDecodeScalars(t *testing.T) {
	bothOrders(t, func(t *testing.T, order ByteOrder) {
		e := newEncoder(order)
		e.putU32(0xdeadbeef)
		e.putI32(-12345)
		e.putFixed(FixedFromInt(7))
		d := newDecoder(order, e.buf)
		if got := d.getU32(); got != 0xdeadbeef {
			t.Errorf("getU32 = %#x", got)
		}
		if got := d.getI32(); got != -12345 {
			t.Errorf("getI32 = %d", got)
		}
		if got := d.getFixed(); got.Int() != 7 {
			t.Errorf("getFixed = %d", got.Int())
		}
		if !d.ok {
			t.Error("decoder not ok after exact reads")
		}
	})
}

func TestEncodeDecodeString(t *testing.T) {
	bothOrders(t, func(t *testing.T, order ByteOrder) {
		for _, s := range []string{"", "a", "wl_compositor", "xdg_wm_base"} {
			e := newEncoder(order)
			e.putString(s)
			// The encoded length is len+1 (NUL), padded to 4.
			if len(e.buf)%4 != 0 {
				t.Errorf("string %q not 4-padded: %d bytes", s, len(e.buf))
			}
			d := newDecoder(order, e.buf)
			if got := d.getString(); got != s {
				t.Errorf("string round-trip %q -> %q", s, got)
			}
			if !d.ok {
				t.Errorf("decoder not ok after string %q", s)
			}
		}
	})
}

func TestDecodeStringZeroLength(t *testing.T) {
	// A wire length of 0 denotes a null string, decoded as "".
	order := binary.LittleEndian
	e := newEncoder(order)
	e.putU32(0)
	d := newDecoder(order, e.buf)
	if got := d.getString(); got != "" {
		t.Errorf("zero-length string = %q, want empty", got)
	}
}

func TestDecodeStringTruncated(t *testing.T) {
	order := binary.LittleEndian
	e := newEncoder(order)
	e.putU32(16) // claims 16 bytes but supplies none
	d := newDecoder(order, e.buf)
	if got := d.getString(); got != "" {
		t.Errorf("truncated string = %q, want empty", got)
	}
	if d.ok {
		t.Error("decoder should be not-ok after truncated string")
	}
}

func TestEncodeDecodeArray(t *testing.T) {
	bothOrders(t, func(t *testing.T, order ByteOrder) {
		for _, a := range [][]byte{{}, {1}, {1, 2, 3}, {1, 2, 3, 4, 5}} {
			e := newEncoder(order)
			e.putArray(a)
			if len(e.buf)%4 != 0 {
				t.Errorf("array len %d not 4-padded: %d bytes", len(a), len(e.buf))
			}
			d := newDecoder(order, e.buf)
			got := d.getArray()
			if len(got) != len(a) {
				t.Fatalf("array round-trip len %d -> %d", len(a), len(got))
			}
			for i := range a {
				if got[i] != a[i] {
					t.Errorf("array[%d] = %d, want %d", i, got[i], a[i])
				}
			}
		}
	})
}

func TestDecodeArrayTruncated(t *testing.T) {
	order := binary.LittleEndian
	e := newEncoder(order)
	e.putU32(12) // claims 12 bytes, supplies none
	d := newDecoder(order, e.buf)
	if got := d.getArray(); got != nil {
		t.Errorf("truncated array = %v, want nil", got)
	}
	if d.ok {
		t.Error("decoder should be not-ok after truncated array")
	}
}

func TestDecodeScalarTruncated(t *testing.T) {
	order := binary.LittleEndian
	d := newDecoder(order, []byte{1, 2}) // fewer than 4 bytes
	if got := d.getU32(); got != 0 {
		t.Errorf("truncated getU32 = %#x, want 0", got)
	}
	if d.ok {
		t.Error("decoder should be not-ok after truncated u32")
	}
	// Once not-ok, further reads stay 0 and not-ok.
	if got := d.getU32(); got != 0 || d.ok {
		t.Error("decoder should stay not-ok")
	}
}

func TestNeedNegative(t *testing.T) {
	d := newDecoder(binary.LittleEndian, []byte{1, 2, 3, 4})
	if d.need(-1) {
		t.Error("need(-1) should be false")
	}
	if d.ok {
		t.Error("need(-1) should clear ok")
	}
}

func TestPutBytesAndPad(t *testing.T) {
	e := newEncoder(binary.LittleEndian)
	e.putBytes([]byte{9, 9, 9})
	e.pad(3)
	if len(e.buf) != 4 || e.buf[3] != 0 {
		t.Fatalf("putBytes+pad = %v", e.buf)
	}
}

func TestOrderName(t *testing.T) {
	if orderName(binary.BigEndian) != "big" {
		t.Error("big endian name")
	}
	if orderName(binary.LittleEndian) != "little" {
		t.Error("little endian name")
	}
}

func TestNativeOrderResolves(t *testing.T) {
	if NativeOrder == nil {
		t.Fatal("NativeOrder must be set")
	}
	// It round-trips a 32-bit value like any ByteOrder (it is
	// binary.NativeEndian, a distinct type from Little/BigEndian).
	e := newEncoder(NativeOrder)
	e.putU32(0x01020304)
	if got := newDecoder(NativeOrder, e.buf).getU32(); got != 0x01020304 {
		t.Fatalf("NativeOrder round-trip = %#x", got)
	}
}
