// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package wayland

import "testing"

func TestPackARGB8888(t *testing.T) {
	// A 2x2 RGBA image; verify each packed pixel decodes back to ARGB.
	const w, h = 2, 2
	src := []byte{
		10, 20, 30, 255, /* px(0,0) */ 40, 50, 60, 128, // px(1,0)
		70, 80, 90, 200, /* px(0,1) */ 100, 110, 120, 255, // px(1,1)
	}
	dst := make([]byte, w*h*4)
	PackARGB8888(dst, w*4, src, w*4, w, h)

	check := func(px, r, g, b, a uint32) {
		v := NativeOrder.Uint32(dst[px*4 : px*4+4])
		if gr := (v >> 16) & 0xff; gr != r {
			t.Errorf("px%d R = %d, want %d", px, gr, r)
		}
		if gg := (v >> 8) & 0xff; gg != g {
			t.Errorf("px%d G = %d, want %d", px, gg, g)
		}
		if gb := v & 0xff; gb != b {
			t.Errorf("px%d B = %d, want %d", px, gb, b)
		}
		if ga := (v >> 24) & 0xff; ga != a {
			t.Errorf("px%d A = %d, want %d", px, ga, a)
		}
	}
	check(0, 10, 20, 30, 255)
	check(1, 40, 50, 60, 128)
	check(2, 70, 80, 90, 200)
	check(3, 100, 110, 120, 255)
}

func TestPackARGB8888Strided(t *testing.T) {
	// A source with padding between rows must be read at the given stride
	// and written tightly to the destination.
	const w, h = 1, 2
	srcStride := 8 // 1 pixel + 4 pad bytes per row
	src := make([]byte, srcStride*h)
	src[0], src[1], src[2], src[3] = 1, 2, 3, 4
	src[srcStride+0], src[srcStride+1], src[srcStride+2], src[srcStride+3] = 5, 6, 7, 8
	dst := make([]byte, w*4*h)
	PackARGB8888(dst, w*4, src, srcStride, w, h)
	if v := NativeOrder.Uint32(dst[0:4]); (v>>16)&0xff != 1 {
		t.Errorf("row0 R = %d", (v>>16)&0xff)
	}
	if v := NativeOrder.Uint32(dst[4:8]); (v>>16)&0xff != 5 {
		t.Errorf("row1 R = %d", (v>>16)&0xff)
	}
}
