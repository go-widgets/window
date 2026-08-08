// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package x11

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func trueColorSetup(maxReq uint16, imgOrder uint8) (*Setup, VisualType) {
	s := &Setup{
		MaxRequestLen:  maxReq,
		ImageByteOrder: imgOrder,
		Formats:        []Format{{Depth: 24, BitsPerPix: 32, ScanlinePad: 32}},
	}
	v := VisualType{ID: 1, Class: VisualTrueColor, RedMask: 0x00ff0000, GreenMask: 0x0000ff00, BlueMask: 0x000000ff}
	return s, v
}

func TestNewPresenterErrors(t *testing.T) {
	s, v := trueColorSetup(0xffff, imageOrderLSB)
	// No format for the requested depth.
	if _, err := NewPresenter(s, v, 16); err == nil {
		t.Fatalf("missing format should error")
	}
	// Non-decomposable visual class.
	badClass := v
	badClass.Class = VisualPseudoColor
	if _, err := NewPresenter(s, badClass, 24); err == nil {
		t.Fatalf("bad class should error")
	}
	// Zero channel mask.
	badMask := v
	badMask.GreenMask = 0
	if _, err := NewPresenter(s, badMask, 24); err == nil {
		t.Fatalf("zero mask should error")
	}
}

func TestNewPresenterMaxReqFallback(t *testing.T) {
	s, v := trueColorSetup(0, imageOrderLSB)
	p, err := NewPresenter(s, v, 24)
	if err != nil {
		t.Fatalf("presenter: %v", err)
	}
	if p.maxReqUnits != 1<<16 {
		t.Fatalf("maxReq fallback = %d", p.maxReqUnits)
	}
	if p.BytesPerPixel() != 4 {
		t.Fatalf("bpp bytes = %d", p.BytesPerPixel())
	}
}

func TestEncodeRegionLSB(t *testing.T) {
	s, v := trueColorSetup(0xffff, imageOrderLSB)
	p, _ := NewPresenter(s, v, 24)
	// 2x1 RGBA source.
	src := []byte{0x11, 0x22, 0x33, 0xff, 0x44, 0x55, 0x66, 0xff}
	out := p.encodeRegion(src, 8, 0, 0, 2, 1)
	want := []byte{0x33, 0x22, 0x11, 0x00, 0x66, 0x55, 0x44, 0x00}
	if !bytes.Equal(out, want) {
		t.Fatalf("LSB encode = % x want % x", out, want)
	}
}

func TestEncodeRegionMSB(t *testing.T) {
	s, v := trueColorSetup(0xffff, imageOrderMSB)
	p, _ := NewPresenter(s, v, 24)
	src := []byte{0x11, 0x22, 0x33, 0xff}
	out := p.encodeRegion(src, 4, 0, 0, 1, 1)
	want := []byte{0x00, 0x11, 0x22, 0x33}
	if !bytes.Equal(out, want) {
		t.Fatalf("MSB encode = % x want % x", out, want)
	}
}

func TestEncodeRegionOffset(t *testing.T) {
	s, v := trueColorSetup(0xffff, imageOrderLSB)
	p, _ := NewPresenter(s, v, 24)
	// 2x2 image; encode only the bottom-right 1x1 pixel at (1,1).
	src := make([]byte, 2*2*4)
	// pixel (1,1) = R=0x0A G=0x0B B=0x0C
	off := (1*2 + 1) * 4
	src[off], src[off+1], src[off+2], src[off+3] = 0x0A, 0x0B, 0x0C, 0xff
	out := p.encodeRegion(src, 8, 1, 1, 1, 1)
	want := []byte{0x0C, 0x0B, 0x0A, 0x00}
	if !bytes.Equal(out, want) {
		t.Fatalf("offset encode = % x want % x", out, want)
	}
}

func TestScaleAndPixel16bpp(t *testing.T) {
	// RGB565 presenter built directly to exercise the width<8 scale path
	// and 16-bpp putValue.
	p := &Presenter{
		imageOrder:  imageOrderLSB,
		bpp:         16,
		scanlinePad: 16,
		rShift:      11, rWidth: 5,
		gShift: 5, gWidth: 6,
		bShift: 0, bWidth: 5,
	}
	// White should saturate all channels: 0xF800|0x07E0|0x001F = 0xFFFF.
	if got := p.pixel(0xff, 0xff, 0xff); got != 0xFFFF {
		t.Fatalf("565 white = %#x", got)
	}
	dst := make([]byte, 2)
	p.putValue(dst, 0xABCD)
	if dst[0] != 0xCD || dst[1] != 0xAB {
		t.Fatalf("16bpp LSB putValue = % x", dst)
	}
	p.imageOrder = imageOrderMSB
	p.putValue(dst, 0xABCD)
	if dst[0] != 0xAB || dst[1] != 0xCD {
		t.Fatalf("16bpp MSB putValue = % x", dst)
	}
}

func TestScanlineBytesPadFallback(t *testing.T) {
	p := &Presenter{bpp: 32, scanlinePad: 0}
	// padBytes<=0 falls back to a 4-byte boundary.
	if got := p.scanlineBytes(3); got != 12 {
		t.Fatalf("scanlineBytes = %d want 12", got)
	}
}

func TestRowsPerChunk(t *testing.T) {
	// Budget smaller than one line -> clamps to a single row.
	p := &Presenter{maxReqUnits: 7} // 7*4 - 24 = 4
	if got := p.rowsPerChunk(8); got != 1 {
		t.Fatalf("clamped rows = %d want 1", got)
	}
	// Ample budget -> budget/line rows.
	p2 := &Presenter{maxReqUnits: 100} // 400 - 24 = 376
	if got := p2.rowsPerChunk(40); got != 9 {
		t.Fatalf("rows = %d want 9", got)
	}
}

func TestPutImageSingle(t *testing.T) {
	order := binary.LittleEndian
	c, fc := dialFakeConn(t, order, nil)
	s, v := trueColorSetup(0xffff, imageOrderLSB)
	p, _ := NewPresenter(s, v, 24)
	src := make([]byte, 4*4*4) // 4x4
	fc.out.Reset()
	if err := c.PutImage(p, testRootWin, c.NewID(), src, 16, 0, 0, 4, 4, 10, 20); err != nil {
		t.Fatalf("putimage: %v", err)
	}
	b := fc.out.Bytes()
	if b[0] != opPutImage {
		t.Fatalf("opcode = %d", b[0])
	}
	if b[1] != imageFormatZPixmap {
		t.Fatalf("format byte = %d", b[1])
	}
	total := int(order.Uint16(b[2:4])) * 4
	if total != len(b) {
		t.Fatalf("length field %d != actual %d", total, len(b))
	}
}

func TestPutImageTiled(t *testing.T) {
	order := binary.LittleEndian
	c, fc := dialFakeConn(t, order, nil)
	s, v := trueColorSetup(0xffff, imageOrderLSB)
	p, _ := NewPresenter(s, v, 24)
	// Force one row per request: budget just under two scanlines.
	line := p.scanlineBytes(4) // 16 bytes
	p.maxReqUnits = (putImageFixedBytes + line) / 4
	src := make([]byte, 4*3*4) // 4x3
	fc.out.Reset()
	if err := c.PutImage(p, testRootWin, c.NewID(), src, 16, 0, 0, 4, 3, 0, 0); err != nil {
		t.Fatalf("putimage tiled: %v", err)
	}
	// Count PutImage requests emitted.
	b := fc.out.Bytes()
	count := 0
	for i := 0; i+4 <= len(b); {
		op := b[i]
		total := int(order.Uint16(b[i+2:i+4])) * 4
		if total == 0 {
			break
		}
		if op == opPutImage {
			count++
		}
		i += total
	}
	if count != 3 {
		t.Fatalf("tiled PutImage count = %d want 3", count)
	}
}

func TestPutImageEmpty(t *testing.T) {
	c, fc := dialFakeConn(t, binary.LittleEndian, nil)
	s, v := trueColorSetup(0xffff, imageOrderLSB)
	p, _ := NewPresenter(s, v, 24)
	fc.out.Reset()
	if err := c.PutImage(p, testRootWin, 1, nil, 0, 0, 0, 0, 0, 0, 0); err != nil {
		t.Fatalf("empty putimage: %v", err)
	}
	if fc.out.Len() != 0 {
		t.Fatalf("empty putimage wrote %d bytes", fc.out.Len())
	}
}

func TestPutImageWriteError(t *testing.T) {
	c, fc := dialFakeConn(t, binary.LittleEndian, nil)
	s, v := trueColorSetup(0xffff, imageOrderLSB)
	p, _ := NewPresenter(s, v, 24)
	fc.writeErr = errInjected
	src := make([]byte, 4*4)
	if err := c.PutImage(p, testRootWin, 1, src, 4, 0, 0, 1, 1, 0, 0); err != errInjected {
		t.Fatalf("write error not propagated: %v", err)
	}
}
