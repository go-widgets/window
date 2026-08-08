// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package x11

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestBuildSetupRequest(t *testing.T) {
	for _, tc := range []struct {
		order    ByteOrder
		sentinel byte
	}{
		{binary.LittleEndian, orderLSB},
		{binary.BigEndian, orderMSB},
	} {
		req := buildSetupRequest(tc.order, tc.sentinel, authMITCookie, bytes.Repeat([]byte{0xAB}, 16))
		if req[0] != tc.sentinel {
			t.Fatalf("sentinel = %c", req[0])
		}
		if tc.order.Uint16(req[2:4]) != 11 {
			t.Fatalf("major != 11")
		}
		if tc.order.Uint16(req[6:8]) != uint16(len(authMITCookie)) {
			t.Fatalf("auth name length wrong")
		}
		if tc.order.Uint16(req[8:10]) != 16 {
			t.Fatalf("auth data length wrong")
		}
		if !bytes.Contains(req, []byte(authMITCookie)) {
			t.Fatalf("auth name not embedded")
		}
		// Whole request must be 4-byte aligned.
		if len(req)%4 != 0 {
			t.Fatalf("request length %d not padded", len(req))
		}
	}
}

func TestParseSetupReply(t *testing.T) {
	for _, order := range []ByteOrder{binary.LittleEndian, binary.BigEndian} {
		body := setupReplyBody(order, imageOrderLSB)
		s, err := parseSetupReply(order, body)
		if err != nil {
			t.Fatalf("parse: %v", err)
		}
		if s.Vendor != "Test" {
			t.Fatalf("vendor = %q", s.Vendor)
		}
		if s.ResourceIDBase != 0x04000000 || s.ResourceIDMask != 0x001fffff {
			t.Fatalf("resource id base/mask wrong")
		}
		if s.MinKeycode != 8 || s.MaxKeycode != 9 {
			t.Fatalf("keycodes %d..%d", s.MinKeycode, s.MaxKeycode)
		}
		if len(s.Screens) != 1 || s.Screens[0].Root != testRootWin {
			t.Fatalf("screen wrong")
		}
		if len(s.Formats) != 1 || s.Formats[0].BitsPerPix != 32 {
			t.Fatalf("format wrong")
		}
		sc := s.Screens[0]
		if sc.RootVisual != testVisualID || sc.RootDepth != 24 {
			t.Fatalf("root visual/depth wrong")
		}
		v, ok := sc.FindVisual(testVisualID)
		if !ok || v.Class != VisualTrueColor || v.RedMask != 0x00ff0000 {
			t.Fatalf("FindVisual wrong: %+v ok=%v", v, ok)
		}
		if _, ok := sc.FindVisual(0x999); ok {
			t.Fatalf("FindVisual should miss")
		}
		if got := sc.RootVisualType(); got.ID != testVisualID {
			t.Fatalf("RootVisualType id = %#x", got.ID)
		}
		if f, ok := s.FormatFor(24); !ok || f.BitsPerPix != 32 {
			t.Fatalf("FormatFor(24) wrong")
		}
		if _, ok := s.FormatFor(16); ok {
			t.Fatalf("FormatFor(16) should miss")
		}
	}
}

func TestRootVisualTypeFallback(t *testing.T) {
	// A screen whose root-visual id is absent from the depth list falls back
	// to a synthesized 24-bit TrueColor visual.
	sc := &Screen{RootVisual: 0xabc}
	v := sc.RootVisualType()
	if v.ID != 0xabc || v.Class != VisualTrueColor || v.RedMask != 0x00ff0000 {
		t.Fatalf("fallback wrong: %+v", v)
	}
}

func TestParseSetupReplyErrors(t *testing.T) {
	order := binary.LittleEndian
	// No screens: numScreens = 0, numFormats = 0.
	e := newEncoder(order)
	e.put32(0)  // release
	e.put32(0)  // base
	e.put32(0)  // mask
	e.put32(0)  // motion
	e.put16(0)  // vendor len
	e.put16(0)  // max req
	e.put8(0)   // screens
	e.put8(0)   // formats
	e.skip(6)   // image order..max keycode (6 bytes: image,bit,unit,pad,min,max)
	e.skip(4)   // unused
	if _, err := parseSetupReply(order, e.buf); err == nil {
		t.Fatal("no-screens should error")
	}

	// Truncated: claims a screen but body ends early.
	e2 := newEncoder(order)
	e2.put32(0)
	e2.put32(0)
	e2.put32(0)
	e2.put32(0)
	e2.put16(0) // vendor len
	e2.put16(0)
	e2.put8(1) // one screen promised...
	e2.put8(0)
	e2.skip(6)
	e2.skip(4)
	// ...but no screen bytes follow.
	if _, err := parseSetupReply(order, e2.buf); err == nil {
		t.Fatal("truncated should error")
	}
}
