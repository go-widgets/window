// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package wayland

import "testing"

// BenchmarkPackARGB8888Full measures packing a full 1920x1080 RGBA
// framebuffer into a reused (persistent) wl_shm pool mapping as ARGB8888 —
// the live Wayland present hot path.
func BenchmarkPackARGB8888Full(b *testing.B) {
	const w, h = 1920, 1080
	src := make([]byte, w*h*4)
	for i := range src {
		src[i] = byte(i)
	}
	dst := make([]byte, w*h*4)
	b.SetBytes(int64(w * h * 4))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PackARGB8888(dst, w*4, src, w*4, w, h)
	}
}

// BenchmarkPackARGB8888Damage measures packing a small 64x64 damage
// sub-region into the pool mapping at an interior offset.
func BenchmarkPackARGB8888Damage(b *testing.B) {
	const w, h = 1920, 1080
	const dw, dh = 64, 64
	src := make([]byte, w*h*4)
	for i := range src {
		src[i] = byte(i)
	}
	dst := make([]byte, w*h*4)
	off := (500*w + 900) * 4
	b.SetBytes(int64(dw * dh * 4))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		PackARGB8888(dst[off:], w*4, src[off:], w*4, dw, dh)
	}
}
