// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// Package nativeimg decodes the PNG an application hands a native control
// (toolkit.NativeControl.Image) and refuses a picture that is too large to be
// an icon. Scaling, fitting and compositing are the painter's (IconSize,
// IconBGRAOver): this package keeps no pixel kernel of its own.
//
// The bytes come from the application, but an application embeds whatever it
// is given: a header that claims 60000x60000 pixels must be refused from its
// header alone, before a decoder allocates 14 GB for it.
package nativeimg

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"
	"image/png"
)

const (
	// MaxBytes bounds the encoded PNG.
	MaxBytes = 1 << 20
	// MaxSide bounds each dimension of the decoded picture, in pixels.
	MaxSide = 1024

	// Pad is the margin, in pixels, kept between a button's picture and its
	// edge: the picture is fitted inside the control less this on every side.
	Pad = 4
)

// ErrTooLarge is returned for a picture beyond MaxBytes or MaxSide.
var ErrTooLarge = errors.New("nativeimg: picture too large")

// Image is a decoded picture: straight (non-premultiplied) RGBA, row after row.
type Image struct {
	W, H int
	RGBA []byte // len == W*H*4
}

// Decode decodes a PNG under the bounds. The header is read first and a
// picture beyond the limits is refused before any pixel buffer is allocated.
func Decode(data []byte) (*Image, error) {
	if len(data) > MaxBytes {
		return nil, fmt.Errorf("%w: %d bytes (limit %d)", ErrTooLarge, len(data), MaxBytes)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("nativeimg: %w", err)
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > MaxSide || cfg.Height > MaxSide {
		return nil, fmt.Errorf("%w: %dx%d (limit %d per side)", ErrTooLarge, cfg.Width, cfg.Height, MaxSide)
	}
	src, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("nativeimg: %w", err)
	}
	b := src.Bounds()
	nrgba := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(nrgba, nrgba.Bounds(), src, b.Min, draw.Src)
	return &Image{W: b.Dx(), H: b.Dy(), RGBA: nrgba.Pix}, nil
}
