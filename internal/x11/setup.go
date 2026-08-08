// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package x11

import "fmt"

// Format is one entry of the server's pixmap-format list: for a given
// colour depth it fixes the bits-per-pixel and scanline padding a ZPixmap
// image of that depth must use on the wire.
type Format struct {
	Depth       uint8
	BitsPerPix  uint8
	ScanlinePad uint8
}

// VisualType describes a visual: its class and the RGB channel masks a
// TrueColor/DirectColor visual packs a pixel with. The masks drive the
// RGBA→wire byte conversion in PutImage.
type VisualType struct {
	ID          uint32
	Class       uint8
	BitsPerRGB  uint8
	ColormapEnt uint16
	RedMask     uint32
	GreenMask   uint32
	BlueMask    uint32
}

// TrueColor and DirectColor are the visual classes whose pixels are
// directly RGB-decomposable via the masks (no palette lookup).
const (
	VisualStaticGray  = 0
	VisualGrayScale   = 1
	VisualStaticColor = 2
	VisualPseudoColor = 3
	VisualTrueColor   = 4
	VisualDirectColor = 5
)

// Depth groups the visuals available at a given colour depth.
type Depth struct {
	Depth   uint8
	Visuals []VisualType
}

// Screen is one root screen: its root window, default colormap, root
// visual and the allowed depths (each carrying its visuals).
type Screen struct {
	Root          uint32
	DefaultColmap uint32
	WhitePixel    uint32
	BlackPixel    uint32
	Width         uint16
	Height        uint16
	RootVisual    uint32
	RootDepth     uint8
	Depths        []Depth
}

// Setup is the parsed server connection-setup reply: everything the client
// needs to allocate resource IDs, pick a visual, size images correctly and
// map keycodes.
type Setup struct {
	Release        uint32
	ResourceIDBase uint32
	ResourceIDMask uint32
	Vendor         string
	MaxRequestLen  uint16 // in 4-byte units
	ImageByteOrder uint8  // 0 = LSBFirst, 1 = MSBFirst
	BitmapBitOrder uint8
	BitmapUnit     uint8
	BitmapPad      uint8
	MinKeycode     uint8
	MaxKeycode     uint8
	Formats        []Format
	Screens        []Screen
}

// Image byte-order values.
const (
	imageOrderLSB = 0
	imageOrderMSB = 1
)

// buildSetupRequest encodes the client connection-setup request: the
// byte-order sentinel, protocol 11.0, and the authorization name+data
// (MIT-MAGIC-COOKIE-1 and the 16-byte cookie, both empty for an
// unauthenticated local connection).
func buildSetupRequest(order ByteOrder, sentinel byte, authName string, authData []byte) []byte {
	e := newEncoder(order)
	e.put8(sentinel)
	e.put8(0) // unused
	e.put16(11)
	e.put16(0)
	e.put16(uint16(len(authName)))
	e.put16(uint16(len(authData)))
	e.skip(2) // unused
	e.putString(authName)
	e.putBytes(authData)
	e.pad(len(authData))
	return e.buf
}

// parseSetupReply decodes a Success setup reply body. The 8-byte reply
// header (status, pad, major, minor, additional-data length) has already
// been consumed by the caller; body is the additional-data block.
func parseSetupReply(order ByteOrder, body []byte) (*Setup, error) {
	d := newDecoder(order, body)
	s := &Setup{}
	s.Release = d.get32()
	s.ResourceIDBase = d.get32()
	s.ResourceIDMask = d.get32()
	_ = d.get32() // motion-buffer-size
	vendorLen := int(d.get16())
	s.MaxRequestLen = d.get16()
	numScreens := int(d.get8())
	numFormats := int(d.get8())
	s.ImageByteOrder = d.get8()
	s.BitmapBitOrder = d.get8()
	s.BitmapUnit = d.get8()
	s.BitmapPad = d.get8()
	s.MinKeycode = d.get8()
	s.MaxKeycode = d.get8()
	d.skip(4) // unused
	s.Vendor = d.getString(vendorLen)

	s.Formats = make([]Format, 0, numFormats)
	for i := 0; i < numFormats; i++ {
		f := Format{
			Depth:       d.get8(),
			BitsPerPix:  d.get8(),
			ScanlinePad: d.get8(),
		}
		d.skip(5) // unused
		s.Formats = append(s.Formats, f)
	}

	s.Screens = make([]Screen, 0, numScreens)
	for i := 0; i < numScreens; i++ {
		sc := Screen{}
		sc.Root = d.get32()
		sc.DefaultColmap = d.get32()
		sc.WhitePixel = d.get32()
		sc.BlackPixel = d.get32()
		_ = d.get32() // current-input-masks
		sc.Width = d.get16()
		sc.Height = d.get16()
		_ = d.get16() // width-mm
		_ = d.get16() // height-mm
		_ = d.get16() // min-installed-maps
		_ = d.get16() // max-installed-maps
		sc.RootVisual = d.get32()
		_ = d.get8() // backing-stores
		_ = d.get8() // save-unders
		sc.RootDepth = d.get8()
		numDepths := int(d.get8())
		sc.Depths = make([]Depth, 0, numDepths)
		for j := 0; j < numDepths; j++ {
			dp := Depth{Depth: d.get8()}
			d.skip(1) // unused
			numVis := int(d.get16())
			d.skip(4) // unused
			dp.Visuals = make([]VisualType, 0, numVis)
			for k := 0; k < numVis; k++ {
				v := VisualType{}
				v.ID = d.get32()
				v.Class = d.get8()
				v.BitsPerRGB = d.get8()
				v.ColormapEnt = d.get16()
				v.RedMask = d.get32()
				v.GreenMask = d.get32()
				v.BlueMask = d.get32()
				d.skip(4) // unused
				dp.Visuals = append(dp.Visuals, v)
			}
			sc.Depths = append(sc.Depths, dp)
		}
		s.Screens = append(s.Screens, sc)
	}

	if !d.ok {
		return nil, fmt.Errorf("x11: truncated setup reply")
	}
	if len(s.Screens) == 0 {
		return nil, fmt.Errorf("x11: setup reply has no screens")
	}
	return s, nil
}

// FindVisual returns the VisualType with the given id on screen sc, and
// whether it was found.
func (sc *Screen) FindVisual(id uint32) (VisualType, bool) {
	for _, dp := range sc.Depths {
		for _, v := range dp.Visuals {
			if v.ID == id {
				return v, true
			}
		}
	}
	return VisualType{}, false
}

// RootVisualType returns the screen's root visual descriptor, falling back
// to a synthesized 24-bit TrueColor BGRX visual if the root visual id is
// somehow absent from the depth list (defensive; real servers always list
// it).
func (sc *Screen) RootVisualType() VisualType {
	if v, ok := sc.FindVisual(sc.RootVisual); ok {
		return v
	}
	return VisualType{
		ID:        sc.RootVisual,
		Class:     VisualTrueColor,
		RedMask:   0x00ff0000,
		GreenMask: 0x0000ff00,
		BlueMask:  0x000000ff,
	}
}

// FormatFor returns the pixmap Format matching depth, and whether one
// exists. PutImage needs it to size each pixel and pad each scanline.
func (s *Setup) FormatFor(depth uint8) (Format, bool) {
	for _, f := range s.Formats {
		if f.Depth == depth {
			return f, true
		}
	}
	return Format{}, false
}
