// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package nativeimg

import (
	"bytes"
	"encoding/binary"
	"errors"
	"hash/crc32"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func encode(t testing.TB, w, h int, c color.NRGBA) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestDecodeKeepsStraightRGBA(t *testing.T) {
	m, err := Decode(encode(t, 3, 2, color.NRGBA{R: 10, G: 20, B: 30, A: 128}))
	if err != nil {
		t.Fatal(err)
	}
	if m.W != 3 || m.H != 2 || len(m.RGBA) != 3*2*4 {
		t.Fatalf("got %dx%d len %d", m.W, m.H, len(m.RGBA))
	}
	if got := m.RGBA[:4]; !bytes.Equal(got, []byte{10, 20, 30, 128}) {
		t.Fatalf("pixel = %v, want straight 10 20 30 128", got)
	}
}

func TestDecodeRefusesLargeBytes(t *testing.T) {
	if _, err := Decode(make([]byte, MaxBytes+1)); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func TestDecodeRefusesLargeSideFromTheHeaderAlone(t *testing.T) {
	// A header claiming 60000x60000 with no pixel data: it must be refused
	// before anything is allocated, so it never reaches the truncated body.
	for _, wh := range [][2]uint32{{60000, 60000}, {MaxSide + 1, 1}, {1, MaxSide + 1}} {
		if _, err := Decode(header(wh[0], wh[1])); !errors.Is(err, ErrTooLarge) {
			t.Errorf("%dx%d: err = %v, want ErrTooLarge", wh[0], wh[1], err)
		}
	}
	if _, err := Decode(encode(t, MaxSide, 1, color.NRGBA{A: 255})); err != nil {
		t.Errorf("MaxSide wide: %v", err)
	}
}

func TestDecodeRefusesGarbage(t *testing.T) {
	for _, in := range [][]byte{nil, {}, []byte("not a png"), header(0, 0)} {
		if _, err := Decode(in); err == nil {
			t.Errorf("Decode(%q) accepted", in)
		}
	}
	// Not a PNG at all is a decode error, not "too large"; and a header within
	// the bounds with no pixels behind it fails in the pixel decode.
	for _, in := range [][]byte{nil, []byte("not a png"), header(16, 16)} {
		img, err := Decode(in)
		if err == nil || img != nil || errors.Is(err, ErrTooLarge) {
			t.Errorf("Decode(%q) = %v, %v; want a plain decode error", in, img, err)
		}
	}
}

// header builds a PNG signature plus an IHDR claiming w x h, and nothing else.
func header(w, h uint32) []byte {
	var b bytes.Buffer
	b.Write([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	ihdr := make([]byte, 13)
	binary.BigEndian.PutUint32(ihdr[0:], w)
	binary.BigEndian.PutUint32(ihdr[4:], h)
	ihdr[8], ihdr[9] = 8, 6 // 8-bit RGBA
	var l [4]byte
	binary.BigEndian.PutUint32(l[:], 13)
	b.Write(l[:])
	b.WriteString("IHDR")
	b.Write(ihdr)
	crc := crc32.NewIEEE()
	crc.Write([]byte("IHDR"))
	crc.Write(ihdr)
	binary.BigEndian.PutUint32(l[:], crc.Sum32())
	b.Write(l[:])
	return b.Bytes()
}

func FuzzDecode(f *testing.F) {
	f.Add(encode(f, 2, 2, color.NRGBA{R: 1, G: 2, B: 3, A: 4}))
	f.Add(header(60000, 60000))
	f.Add([]byte("junk"))
	f.Fuzz(func(t *testing.T, data []byte) {
		m, err := Decode(data)
		if err != nil {
			return
		}
		if m.W <= 0 || m.H <= 0 || m.W > MaxSide || m.H > MaxSide || len(m.RGBA) != m.W*m.H*4 {
			t.Fatalf("inconsistent image %dx%d len %d", m.W, m.H, len(m.RGBA))
		}
	})
}
