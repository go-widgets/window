// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package x11

import "testing"

// benchPresenter builds a standard depth-24 TrueColor presenter (8:8:8 masks,
// 32 bpp, LSB image order) — the common desktop visual — for the pack
// benchmarks.
func benchPresenter(b *testing.B) *Presenter {
	b.Helper()
	s, v := trueColorSetup(0xffff, imageOrderLSB)
	p, err := NewPresenter(s, v, 24)
	if err != nil {
		b.Fatalf("presenter: %v", err)
	}
	return p
}

// BenchmarkEncodeRectIntoFull measures packing a full 1920x1080 RGBA
// framebuffer into a reused (persistent) shared-segment buffer — the live
// X11 MIT-SHM present hot path.
func BenchmarkEncodeRectIntoFull(b *testing.B) {
	const w, h = 1920, 1080
	p := benchPresenter(b)
	src := make([]byte, w*h*4)
	for i := range src {
		src[i] = byte(i)
	}
	seg := make([]byte, p.SegmentSize(w, h))
	b.SetBytes(int64(w * h * 4))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := p.EncodeRectInto(seg, w, src, w*4, 0, 0, w, h); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkEncodeRectIntoDamage measures packing a small 64x64 damage
// sub-region into the shared segment at an interior offset.
func BenchmarkEncodeRectIntoDamage(b *testing.B) {
	const w, h = 1920, 1080
	const dw, dh = 64, 64
	p := benchPresenter(b)
	src := make([]byte, w*h*4)
	for i := range src {
		src[i] = byte(i)
	}
	seg := make([]byte, p.SegmentSize(w, h))
	b.SetBytes(int64(dw * dh * 4))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := p.EncodeRectInto(seg, w, src, w*4, 900, 500, dw, dh); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkEncodeRegionFull measures the plain-PutImage (non-SHM fallback)
// full-frame pack, which allocates its wire buffer.
func BenchmarkEncodeRegionFull(b *testing.B) {
	const w, h = 1920, 1080
	p := benchPresenter(b)
	src := make([]byte, w*h*4)
	for i := range src {
		src[i] = byte(i)
	}
	b.SetBytes(int64(w * h * 4))
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = p.encodeRegion(src, w*4, 0, 0, w, h)
	}
}
