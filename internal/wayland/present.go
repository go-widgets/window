// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package wayland

import "fmt"

// PackARGB8888 converts a w×h RGBA source (4 bytes per pixel, R,G,B,A byte
// order, srcStride bytes per row) into WL_SHM_FORMAT_ARGB8888 pixels in dst
// (dstStride bytes per row). Each destination pixel is the 32-bit value
// 0xAARRGGBB written in the machine's native byte order — exactly what a
// compositor on the same machine reads back — so the packing is correct on
// little- and big-endian hosts alike.
func PackARGB8888(dst []byte, dstStride int, src []byte, srcStride, w, h int) {
	for y := 0; y < h; y++ {
		so := y * srcStride
		do := y * dstStride
		for x := 0; x < w; x++ {
			r := uint32(src[so])
			g := uint32(src[so+1])
			b := uint32(src[so+2])
			a := uint32(src[so+3])
			NativeOrder.PutUint32(dst[do:do+4], a<<24|r<<16|g<<8|b)
			so += 4
			do += 4
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
