// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package cocoa

import (
	"github.com/go-gfx/gpu2d"
	gpumetal "github.com/go-gfx/gpu2d/metal"
	mtl "github.com/go-macos/metal"
	objc "github.com/go-macos/objc"
	"github.com/go-widgets/gpupainter"
	"github.com/go-widgets/toolkit"
)

// gpuPath draws frames on the GPU (Options.GPU). The widgets record into a
// gpu2d list through gpupainter, gpu2d's Metal renderer draws it, and a
// second kernel copies the result into a CAMetalLayer's drawable: the frame
// never comes back to the CPU and -drawRect: has nothing to blit.
//
// The layer is a SUBLAYER of the view's own layer, inserted at index 0, not
// the layer of a layer-hosting view. AppKit does not support subviews in a
// layer-hosting view, and the native controls this back-end embeds are
// subviews; at index 0 their layers stay above the frame.
type gpuPath struct {
	r     *gpumetal.Renderer
	layer *mtl.Layer
	list  *gpu2d.List
	paint *gpupainter.Painter
}

// kCALayerWidthSizable | kCALayerHeightSizable (CALayer.h): the frame follows
// the host layer's bounds when the window is resized.
const caLayerFollowsBounds = 1<<1 | 1<<4

// gpuOpener is the seam a test replaces to make the GPU path fail to start.
var gpuOpener = gpumetal.New

// startGPU switches the window to the GPU path, if it was asked for and the
// window can take it. A window with translucent materials stays on the CPU:
// it punches holes in the CPU framebuffer for the effect views behind it.
// Any failure leaves the CPU path in place and records why (GPUError).
func (w *Window) startGPU() {
	if !w.wantGPU || w.gpu != nil || w.translucent || w.view == 0 {
		return
	}
	r, err := gpuOpener(w.w, w.h)
	if err != nil {
		w.gpuErr = err
		return
	}
	layer, err := r.NewLayer()
	if err != nil {
		r.Close()
		w.gpuErr = err
		return
	}
	list, err := gpu2d.NewList(w.w, w.h)
	if err != nil {
		layer.Close()
		r.Close()
		w.gpuErr = err
		return
	}
	w.view.Send(objc.Sel("setWantsLayer:"), true)
	host := w.view.Send(objc.Sel("layer"))
	ml := objc.ID(layer.ID())
	ml.Send(objc.Sel("setContentsScale:"), w.scale)
	ml.Send(objc.Sel("setFrame:"), objc.Send[nsRect](host, objc.Sel("bounds")))
	ml.Send(objc.Sel("setAutoresizingMask:"), uint32(caLayerFollowsBounds))
	host.Send(objc.Sel("insertSublayer:atIndex:"), ml, uint32(0))
	w.gpu = &gpuPath{r: r, layer: layer, list: list, paint: gpupainter.New(list)}
	w.gpuErr = nil
}

// stopGPU takes the layer out of the view and releases the GPU objects. The
// window draws on the CPU afterwards.
func (w *Window) stopGPU() {
	g := w.gpu
	if g == nil {
		return
	}
	w.gpu = nil
	objc.ID(g.layer.ID()).Send(objc.Sel("removeFromSuperlayer"))
	g.r.Close() // waits for frames in flight
	g.layer.Close()
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

// presentGPU sends the recorded frame to the layer. One frame may stay in
// flight, so the GPU draws it while the main thread returns to its events.
// A frame the GPU refuses drops the window to the CPU path for good, and the
// frame is drawn there instead.
func (w *Window) presentGPU() {
	g := w.gpu
	err := g.list.Err()
	if err == nil {
		err = g.r.Present(g.list, g.layer)
	}
	for err == nil && g.r.InFlight() > 1 {
		err = g.r.Wait()
	}
	if err != nil {
		w.gpuErr = err
		w.stopGPU()
		w.draw()
		w.presentFull()
	}
}

// resizeGPU follows a new framebuffer size or render scale.
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
		err = g.layer.Resize(w.w, w.h)
	}
	if err != nil {
		w.gpuErr = err
		w.stopGPU()
		return
	}
	g.list = list
	objc.ID(g.layer.ID()).Send(objc.Sel("setContentsScale:"), w.scale)
}

// GPU reports whether frames are being drawn on the GPU.
func (w *Window) GPU() bool { return w.gpu != nil }

// GPUError is why a window that asked for the GPU is not using it: nil when
// it is, or when it never asked.
func (w *Window) GPUError() error { return w.gpuErr }
