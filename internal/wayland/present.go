// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package wayland

import (
	"encoding/binary"
	"fmt"
)

// PackARGB8888 converts a w×h RGBA source (4 bytes per pixel, R,G,B,A byte
// order, srcStride bytes per row) into WL_SHM_FORMAT_ARGB8888 pixels in dst
// (dstStride bytes per row). Each destination pixel is the 32-bit value
// 0xAARRGGBB written in the machine's native byte order — exactly what a
// compositor on the same machine reads back — so the packing is correct on
// little- and big-endian hosts alike.
// The 32-bit word is assembled and stored via the concrete
// binary.NativeEndian (not the ByteOrder interface) so the compiler inlines
// it to a single word store instead of a per-pixel interface method call;
// rows are resliced so the inner loop's indices are provably in range.
func PackARGB8888(dst []byte, dstStride int, src []byte, srcStride, w, h int) {
	n := w * 4
	for y := 0; y < h; y++ {
		srow := src[y*srcStride : y*srcStride+n]
		drow := dst[y*dstStride : y*dstStride+n]
		for x := 0; x < n; x += 4 {
			s := srow[x : x+4 : x+4]
			v := uint32(s[3])<<24 | uint32(s[0])<<16 | uint32(s[1])<<8 | uint32(s[2])
			binary.NativeEndian.PutUint32(drow[x:x+4:x+4], v)
		}
	}
}

// Compositor finds and binds the wl_compositor global.
func (r *Registry) Compositor() (*Compositor, error) {
	g, ok := r.Find("wl_compositor")
	if !ok {
		return nil, fmt.Errorf("wayland: compositor advertises no wl_compositor")
	}
	return bindCompositor(r, g)
}

// Shm finds and binds the wl_shm global.
func (r *Registry) Shm() (*Shm, error) {
	g, ok := r.Find("wl_shm")
	if !ok {
		return nil, fmt.Errorf("wayland: compositor advertises no wl_shm")
	}
	return bindShm(r, g)
}

// XdgWmBase finds and binds the xdg_wm_base global (stable xdg-shell).
func (r *Registry) XdgWmBase() (*XdgWmBase, error) {
	g, ok := r.Find("xdg_wm_base")
	if !ok {
		return nil, fmt.Errorf("wayland: compositor advertises no xdg_wm_base")
	}
	return bindXdgWmBase(r, g)
}
