// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import "github.com/go-widgets/window/internal/wayland"

// HiDPI on Wayland: drawing at the panel's resolution rather than at half of it.
//
// A compositor on a scale-2 screen treats a client's buffer as being in LOGICAL
// units unless the client says otherwise, and stretches it to the panel. So an
// application that never asks is drawn at half the resolution of the best
// screens it will ever run on, and then blown up — soft text, soft icons, and
// nothing anywhere reporting a problem.
//
// Asking is two things: allocate the framebuffer scale times larger, and tell
// the compositor with wl_surface.set_buffer_scale. Which screen's scale applies
// is a question only wl_surface.enter can answer, and the answer changes when
// the user drags the window to the other monitor.
//
// This is off unless the caller asked for [NativeScale], because it is a
// decision about what a point means to the widget tree, not a detail: at scale 2
// the framebuffer is 2× the surface, [Backend.Size] reports the larger number,
// and a root that lays out in framebuffer pixels draws everything twice the
// size unless it is told the scale. That is exactly what the Cocoa back-end
// already does, and [Scaler] is how a root asks.

// followOutputScale binds every wl_output and wires the surface's enter/leave,
// so the window learns which screen it is on and what that screen's scale is.
//
// A compositor that advertises no output at all leaves the window at scale 1,
// which is what it would have been anyway.
func (w *wlWindow) followOutputScale(reg *wayland.Registry) error {
	outs, err := reg.Outputs()
	if err != nil {
		return err
	}
	w.outputs = make(map[uint32]*wayland.Output, len(outs))
	for _, o := range outs {
		w.outputs[o.ID()] = o
		o.OnScale = func(int) { w.rescale() }
	}
	w.on = map[uint32]bool{}
	w.surface.OnEnter = func(out uint32) { w.on[out] = true; w.rescale() }
	w.surface.OnLeave = func(out uint32) { delete(w.on, out); w.rescale() }
	return nil
}

// outputScale is the scale the window should draw at: the largest of the
// screens it is currently shown on.
//
// The largest, because a window straddling a 1× and a 2× screen has to satisfy
// the sharper one — the compositor downscales for the other, which loses
// nothing a viewer can see, while the reverse would be visibly soft on half the
// window.
func (w *wlWindow) outputScale() int {
	best := 0
	for id := range w.on {
		if o := w.outputs[id]; o != nil && o.Scale() > best {
			best = o.Scale()
		}
	}
	if best < 1 {
		return 1 // not on any output yet, or none of them said
	}
	return best
}

// rescale re-derives the framebuffer from the surface size and the current
// output scale. It is called when the window enters or leaves a screen and when
// a screen's scale changes, which is what happens when the user drags the
// window to the other monitor.
func (w *wlWindow) rescale() {
	want := w.outputScale()
	if want == w.scale {
		return
	}
	w.scale = want
	w.resizeFramebuffer()
	// The compositor has to be told before it next reads the buffer, and the
	// surface has to be repainted at the new size: a buffer whose scale and
	// whose dimensions disagree is a protocol error, not a blurry window.
	_ = w.surface.SetBufferScale(w.scale)
	w.repaint = true
}

// resizeFramebuffer sets the pixel framebuffer from the logical size and the
// scale, and drops the old one.
func (w *wlWindow) resizeFramebuffer() {
	if w.scale < 1 {
		w.scale = 1 // a scale of zero is not a scale; a zero-value window is 1:1
	}
	pw, ph := w.logW*w.scale, w.logH*w.scale
	if pw <= 0 || ph <= 0 {
		return
	}
	if pw == w.w && ph == w.h {
		return
	}
	w.w, w.h = pw, ph
	w.buf = make([]byte, 4*w.w*w.h)
}

// RenderScale reports how many framebuffer pixels this window allocates per
// logical point. Implements the [Scaler] capability.
//
// It is 1 unless the caller asked for [NativeScale] and the compositor put the
// window on a screen that says otherwise.
func (w *wlWindow) RenderScale() float64 {
	if w.scale < 1 {
		return 1
	}
	return float64(w.scale)
}
