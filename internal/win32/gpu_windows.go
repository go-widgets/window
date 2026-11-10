// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package win32

import (
	"github.com/go-gfx/gpu2d"
	"github.com/go-gfx/gpu2d/d3d11"
	"github.com/go-widgets/gpupainter"
	"github.com/go-widgets/toolkit"
)

// gpuPath draws frames on the GPU (Config.GPU), as the Cocoa back-end's does: the
// widgets record into a gpu2d list through gpupainter, gpu2d's Direct3D 11
// renderer draws it, and a second kernel copies the result into the back buffer
// of a flip-model swap chain on the window's own HWND (go-gfx/gpu2d Present,
// go-mswin/d3d11 v0.3.0). The frame never comes back to the CPU and WM_PAINT has
// nothing to blit.
//
// A flip-model swap chain owns what its window shows ("only Direct3D content in
// flip model swap chains ... are visible"), so while it exists onPaint only
// validates the update region; when the path stops, the swap chain is released
// before GDI draws again.
//
// Opt-in, as on macOS: the Direct3D 11 back-end is proven on WARP in CI and on no
// GPU (go-gfx/gpu2d docs/NATIVE.md, Decisions taken).
type gpuPath struct {
	r     *d3d11.Renderer
	sc    *d3d11.SwapChain
	list  *gpu2d.List
	paint *gpupainter.Painter
}

// gpuOpener is the seam a test replaces to make the GPU path fail to start, or
// to ask for the software device (WARP) on a machine whose adapter is not the
// one being proven. By default it is the default hardware adapter.
var gpuOpener = d3d11.New

// gpuCapture, when set by a test, receives a copy of each frame's back buffer as
// the device holds it just before the swap chain presents it (the frame the
// window is about to show), with the framebuffer size. It is the proof that the
// presented frame is the CPU path's, with no desktop to take a screenshot from.
var gpuCapture func(frame []byte, w, h int)

// gpuFault, when set by a test, is asked before each present: an error stops the
// path as a lost device would.
var gpuFault func() error

// startGPU switches the window to the GPU path, if it was asked for. Any failure
// (no adapter, a device that cannot make a flip swap chain with UAV back
// buffers, a frame too large) leaves the CPU path in place and is recorded in
// GPUError. It runs once the window exists, from Run.
func (w *Window) startGPU() {
	if !w.wantGPU || w.gpu != nil || w.hwnd == 0 {
		return
	}
	r, err := gpuOpener(w.w, w.h)
	if err != nil {
		w.gpuErr = err
		return
	}
	sc, err := r.NewSwapChain(w.hwnd)
	if err != nil {
		r.Close()
		w.gpuErr = err
		return
	}
	list, err := gpu2d.NewList(w.w, w.h)
	if err != nil {
		sc.Close()
		r.Close()
		w.gpuErr = err
		return
	}
	w.gpu = &gpuPath{r: r, sc: sc, list: list, paint: gpupainter.New(list)}
	w.gpuErr = nil
}

// stopGPU releases the swap chain and the device, and gives the window back to
// GDI. The window draws on the CPU afterwards.
func (w *Window) stopGPU() {
	g := w.gpu
	if g == nil {
		return
	}
	w.gpu = nil
	g.sc.Close()
	g.r.Close()
	// Direct3D defers the destruction of what it was using; a flip swap chain
	// released but not flushed would still own the window.
}

// drawGPU records the whole frame: the background, then the root.
func (w *Window) drawGPU() {
	g := w.gpu
	g.list.Reset()
	bg := w.theme.Background
	g.list.FillRect(gpu2d.Rect{W: w.w, H: w.h}, gpu2d.RGBA{R: bg.R, G: bg.G, B: bg.B, A: bg.A})
	g.paint.Reset(g.list)
	if w.root != nil {
		w.root.SetBounds(toolkit.Rect{X: 0, Y: 0, W: w.w, H: w.h})
		w.root.Draw(g.paint, w.theme)
	}
	g.paint.Flush()
}

// presentGPU sends the recorded frame to the swap chain. A frame the device
// refuses drops the window to the CPU path for good, and that frame is drawn
// there instead.
func (w *Window) presentGPU() {
	g := w.gpu
	err := g.list.Err()
	if err == nil && gpuFault != nil {
		err = gpuFault()
	}
	if err == nil {
		if gpuCapture != nil {
			frame := make([]byte, 4*w.w*w.h)
			if err = g.r.PresentAndRead(g.list, g.sc, frame); err == nil {
				gpuCapture(frame, w.w, w.h)
			}
		} else {
			err = g.r.Present(g.list, g.sc)
		}
	}
	if err != nil {
		w.gpuErr = err
		w.stopGPU()
		w.draw()
		w.invalidateAll()
	}
}

// resizeGPU follows a new framebuffer size.
func (w *Window) resizeGPU() {
	g := w.gpu
	if g == nil {
		return
	}
	list, err := gpu2d.NewList(w.w, w.h)
	if err == nil {
		err = g.r.Resize(w.w, w.h)
	}
	if err == nil {
		err = g.sc.Resize(w.w, w.h)
	}
	if err != nil {
		w.gpuErr = err
		w.stopGPU()
		w.draw()
		w.invalidateAll()
		return
	}
	g.list = list
}

// GPU reports whether frames are being drawn on the GPU.
func (w *Window) GPU() bool { return w.gpu != nil }

// GPUError is why a window that asked for the GPU is not using it: nil when it
// is, or when it never asked.
func (w *Window) GPUError() error { return w.gpuErr }
