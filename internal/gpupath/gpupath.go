// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// Package gpupath is the GPU frame path of the X11 and Wayland back-ends
// (Config.GPU): the widgets are recorded through go-widgets/gpupainter into a
// go-gfx/gpu2d list, gpu2d's Vulkan renderer draws it, and the renderer's
// Pixels are copied into the window's own RGBA framebuffer. The back-end then
// presents that framebuffer through the path it already has (wl_shm, MIT-SHM or
// PutImage), unchanged. No dma-buf is involved: docs/NATIVE.md of go-gfx/gpu2d,
// Decision 3, fallback 2.
//
// The path is all or nothing, as the Cocoa one is. A frame the renderer refuses
// (ErrTooLarge, ErrUnsupported, a lost device, a timeout) is recorded in Err,
// the renderer is released, and Render reports false: the caller draws that
// frame, and every later one, on the CPU.
//
// A Path is safe for concurrent use: a window's Close may come from any
// goroutine while the run loop is drawing.
package gpupath

import (
	"errors"
	"sync"

	"github.com/go-gfx/gpu2d"
	"github.com/go-widgets/gpupainter"
	"github.com/go-widgets/toolkit"
)

// Renderer is what the path needs of gpu2d's renderer; *vulkan.Renderer is one.
type Renderer interface {
	// Draw composites the list over the frame and waits for the device.
	Draw(*gpu2d.List) error
	// Pixels is the frame, 4·w·h bytes of RGBA rows top to bottom.
	Pixels() []byte
	// Resize changes the frame's size; its content is not kept.
	Resize(w, h int) error
	// Close releases the device.
	Close()
}

// ErrSize reports a renderer whose frame is not the window's size.
var ErrSize = errors.New("gpupath: the renderer's frame is not the window's size")

// Open is the seam a test replaces to make the GPU path fail to start. By
// default it is gpu2d's Vulkan renderer, on the best device the loader offers
// (open.go); where go-gfx/vulkan has no binding (android included, since
// v0.2.1) it refuses with vulkan.ErrUnsupported.
var Open = open

// Path is one window's GPU state, whether or not the window asked for it. The
// zero Path is inert, so a window need not allocate one; it must not be copied.
type Path struct {
	mu      sync.Mutex
	want    bool
	closed  bool
	dropped bool
	r       Renderer
	list    *gpu2d.List
	paint   *gpupainter.Painter
	err     error
}

// Ask records whether the window asked for the GPU. Without it the path is
// inert: Start does nothing and Render reports false.
func (p *Path) Ask(want bool) {
	p.mu.Lock()
	p.want = want
	p.mu.Unlock()
}

// Start brings the GPU path up for a w × h framebuffer. It is a no-op when the
// window did not ask, when the path is up, or after Close. A failure leaves the
// CPU path in place and is recorded in Err.
func (p *Path) Start(w, h int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if !p.want || p.closed || p.r != nil {
		return
	}
	r, err := Open(w, h)
	if err != nil {
		p.err = err
		return
	}
	list, err := gpu2d.NewList(w, h)
	if err != nil {
		r.Close()
		p.err = err
		return
	}
	// The renderer composites over what its frame already holds, as PixelPainter
	// does over its buffer, and a device's memory is not promised to be zeroed:
	// start from the transparent black a fresh framebuffer is.
	clear(r.Pixels())
	p.r, p.list, p.paint, p.err = r, list, gpupainter.New(list), nil
}

// stop releases the renderer, which must be up. The caller holds the lock.
func (p *Path) stop() {
	p.r.Close()
	p.r, p.list, p.paint = nil, nil, nil
	p.dropped = true
}

// Active reports whether frames are being drawn on the GPU.
func (p *Path) Active() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.r != nil
}

// Err is why a window that asked for the GPU is not using it: nil when it is,
// or when it never asked.
func (p *Path) Err() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

// Device names the device the frames are drawn on when the renderer says (gpu2d's
// does: "llvmpipe (LLVM 19.1.7, 256 bits)"), and is empty otherwise.
func (p *Path) Device() string {
	p.mu.Lock()
	defer p.mu.Unlock()
	if n, ok := p.r.(interface{ Name() string }); ok {
		return n.Name()
	}
	return ""
}

// TakeDropped reports whether the path stopped since the last call, and clears
// that. A window with an incremental root calls it to learn that its
// framebuffer was not kept up to date while the GPU drew, so the next frame must
// be a whole one.
func (p *Path) TakeDropped() bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	d := p.dropped
	p.dropped = false
	return d
}

// Render draws the whole frame (the theme's background, then root laid out to
// w × h) on the GPU and copies it into dst, the window's w × h RGBA
// framebuffer. It reports whether it did. On false the caller draws the frame
// on the CPU: the path was never up, or it just failed (see Err) and is now
// down for good.
func (p *Path) Render(dst []byte, w, h int, root toolkit.Widget, theme *toolkit.Theme) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.r == nil {
		return false
	}
	err := p.record(w, h, root, theme)
	if err == nil {
		err = p.r.Draw(p.list)
	}
	if px := p.r.Pixels(); err == nil && len(px) != len(dst) {
		err = ErrSize
	}
	if err != nil {
		p.err = err
		p.stop()
		return false
	}
	copy(dst, p.r.Pixels())
	return true
}

// record writes the frame into the list.
func (p *Path) record(w, h int, root toolkit.Widget, theme *toolkit.Theme) error {
	p.list.Reset()
	bg := theme.Background
	full := toolkit.Rect{X: 0, Y: 0, W: w, H: h}
	p.list.FillRect(gpu2d.Rect{W: w, H: h}, gpu2d.RGBA{R: bg.R, G: bg.G, B: bg.B, A: bg.A})
	p.paint.Reset(p.list)
	if root != nil {
		root.SetBounds(full)
		root.Draw(p.paint, theme)
	}
	p.paint.Flush()
	return p.list.Err()
}

// Resize follows a new framebuffer size. A refusal (a frame the device cannot
// hold: gpu2d.ErrTooLarge) stops the path, as a refused frame does.
func (p *Path) Resize(w, h int) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.r == nil {
		return
	}
	list, err := gpu2d.NewList(w, h)
	if err == nil {
		err = p.r.Resize(w, h)
	}
	if err != nil {
		p.err = err
		p.stop()
		return
	}
	clear(p.r.Pixels())
	p.list = list // the painter is pointed at it by the next record
}

// Close releases the renderer, waiting for the device. The path never starts
// again; Close twice is harmless.
func (p *Path) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	if p.r != nil {
		p.r.Close()
		p.r, p.list, p.paint = nil, nil, nil
	}
}
