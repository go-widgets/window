// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package x11

import (
	"fmt"
	"math/bits"
)

// Presenter converts a toolkit RGBA framebuffer (R,G,B,A byte order) into
// the exact ZPixmap wire bytes a given visual + pixmap-format expect, and
// tiles PutImage requests so none exceeds the server's maximum request
// length.
//
// Pixel bytes are laid out per the server's image-byte-order (independent
// of the protocol byte order): each pixel value is assembled from the RGB
// channels via the visual's masks, then serialised LSB- or MSB-first in
// bpp/8 bytes.
type Presenter struct {
	imageOrder  uint8
	bpp         int // bits per pixel on the wire (typically 32 for depth 24)
	scanlinePad int // scanline pad in bits
	depth       uint8
	maxReqUnits int // maximum-request-length, in 4-byte units

	rShift, rWidth uint
	gShift, gWidth uint
	bShift, bWidth uint
}

// putImageFixedBytes is the fixed portion of a PutImage request preceding
// the image data: the 4-byte request header plus drawable, gc, the
// width/height and dst-x/dst-y words, and the left-pad/depth/pad word.
const putImageFixedBytes = 24

// NewPresenter derives the pixel-packing parameters for depth from the
// screen's visual and the server setup.
func NewPresenter(setup *Setup, vis VisualType, depth uint8) (*Presenter, error) {
	fmtEntry, ok := setup.FormatFor(depth)
	if !ok {
		return nil, fmt.Errorf("x11: no pixmap format for depth %d", depth)
	}
	if vis.Class != VisualTrueColor && vis.Class != VisualDirectColor {
		return nil, fmt.Errorf("x11: visual class %d is not directly RGB-decomposable", vis.Class)
	}
	if vis.RedMask == 0 || vis.GreenMask == 0 || vis.BlueMask == 0 {
		return nil, fmt.Errorf("x11: visual has a zero channel mask")
	}
	maxReq := int(setup.MaxRequestLen)
	if maxReq <= 0 {
		maxReq = 1 << 16 // core-protocol default when the server reports none
	}
	p := &Presenter{
		imageOrder:  setup.ImageByteOrder,
		bpp:         int(fmtEntry.BitsPerPix),
		scanlinePad: int(fmtEntry.ScanlinePad),
		depth:       depth,
		maxReqUnits: maxReq,
		rShift:      uint(bits.TrailingZeros32(vis.RedMask)),
		rWidth:      uint(bits.OnesCount32(vis.RedMask)),
		gShift:      uint(bits.TrailingZeros32(vis.GreenMask)),
		gWidth:      uint(bits.OnesCount32(vis.GreenMask)),
		bShift:      uint(bits.TrailingZeros32(vis.BlueMask)),
		bWidth:      uint(bits.OnesCount32(vis.BlueMask)),
	}
	return p, nil
}

// BytesPerPixel is the on-the-wire size of one pixel.
func (p *Presenter) BytesPerPixel() int { return p.bpp / 8 }

// scanlineBytes is the padded byte length of one image row of w pixels.
func (p *Presenter) scanlineBytes(w int) int {
	raw := (w*p.bpp + 7) / 8
	padBytes := p.scanlinePad / 8
	if padBytes <= 0 {
		padBytes = 4
	}
	return ((raw + padBytes - 1) / padBytes) * padBytes
}

// scale converts an 8-bit channel value to the mask's bit width.
func scale(v uint8, width uint) uint32 {
	if width >= 8 {
		return uint32(v) << (width - 8)
	}
	return uint32(v) >> (8 - width)
}

// pixel packs one RGBA source pixel (bytes r,g,b,_) into its wire value.
func (p *Presenter) pixel(r, g, b uint8) uint32 {
	return scale(r, p.rWidth)<<p.rShift |
		scale(g, p.gWidth)<<p.gShift |
		scale(b, p.bWidth)<<p.bShift
}

// putValue appends v as bpp/8 bytes in the server's image byte order.
func (p *Presenter) putValue(dst []byte, v uint32) {
	n := p.bpp / 8
	if p.imageOrder == imageOrderMSB {
		for i := n - 1; i >= 0; i-- {
			dst[i] = byte(v)
			v >>= 8
		}
		return
	}
	for i := 0; i < n; i++ {
		dst[i] = byte(v)
		v >>= 8
	}
}

// encodeRegion converts the w×h rectangle at (sx, sy) of an RGBA source
// buffer (srcStride bytes per row) into padded ZPixmap wire bytes.
func (p *Presenter) encodeRegion(src []byte, srcStride, sx, sy, w, h int) []byte {
	line := p.scanlineBytes(w)
	bpx := p.bpp / 8
	out := make([]byte, line*h)
	for row := 0; row < h; row++ {
		srcRow := (sy+row)*srcStride + sx*4
		dstRow := row * line
		for col := 0; col < w; col++ {
			so := srcRow + col*4
			p.putValue(out[dstRow+col*bpx:dstRow+col*bpx+bpx],
				p.pixel(src[so], src[so+1], src[so+2]))
		}
	}
	return out
}

// rowsPerChunk is the most image rows that fit under the server's maximum
// request length for a row of scanline byte-length line.
func (p *Presenter) rowsPerChunk(line int) int {
	budget := p.maxReqUnits*4 - putImageFixedBytes
	if budget < line {
		return 1
	}
	return budget / line
}

// PutImage blits the w×h rectangle at (sx, sy) of the RGBA source buffer
// onto drawable at (dstX, dstY) via one or more ZPixmap PutImage requests,
// each kept under the server's maximum request length by horizontal
// banding.
func (c *Conn) PutImage(p *Presenter, drawable, gc uint32, src []byte, srcStride, sx, sy, w, h, dstX, dstY int) error {
	if w <= 0 || h <= 0 {
		return nil
	}
	line := p.scanlineBytes(w)
	band := p.rowsPerChunk(line)
	for y0 := 0; y0 < h; y0 += band {
		rows := band
		if y0+rows > h {
			rows = h - y0
		}
		data := p.encodeRegion(src, srcStride, sx, sy+y0, w, rows)
		if err := c.putImageChunk(p, drawable, gc, w, rows, dstX, dstY+y0, data); err != nil {
			return err
		}
	}
	return nil
}

// putImageChunk emits a single PutImage request for an already-encoded
// band of image data.
func (c *Conn) putImageChunk(p *Presenter, drawable, gc uint32, w, h, dstX, dstY int, data []byte) error {
	e := newEncoder(c.order)
	e.put32(drawable)
	e.put32(gc)
	e.put16(uint16(w))
	e.put16(uint16(h))
	e.put16(uint16(int16(dstX)))
	e.put16(uint16(int16(dstY)))
	e.put8(0)       // left-pad (0 for ZPixmap)
	e.put8(p.depth) // depth
	e.skip(2)       // unused
	e.putBytes(data)
	e.pad(len(data))
	return c.sendRequest(opPutImage, imageFormatZPixmap, e.buf)
}
