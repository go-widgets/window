// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package cocoa

import (
	"github.com/go-macos/objc"
	"github.com/go-widgets/toolkit"
)

// This file is the Cocoa backing for toolkit.Material — the platform half of the
// vibrancy seam. The toolkit widget is OS-neutral and only marks WHERE a
// translucent material goes; here we install a real NSVisualEffectView behind
// each one so the desktop (or the window content) shows through, softened by the
// system blur, and punch a transparent hole in the framebuffer over that region
// so the effect view is visible through the pixel view drawn on top.
//
// It is opt-in and regression-free: a scene with no Material leaves the window
// exactly as before — opaque, a single content view, a Copy blit. Only the
// presence of a Material flips the window into translucent mode.

// NSVisualEffectMaterial values (AppKit). Generic UI roles map onto them; the
// toolkit names none of these.
const (
	nsvfxTitlebar         = 3
	nsvfxSelection        = 4
	nsvfxMenu             = 5
	nsvfxPopover          = 6
	nsvfxSidebar          = 7
	nsvfxWindowBackground = 12
	nsvfxHUDWindow        = 13
)

// NSVisualEffectBlendingMode values.
const (
	nsvfxBehindWindow = 0
	nsvfxWithinWindow = 1
)

// NSVisualEffectState values.
const nsvfxStateActive = 1

// NSCompositingOperationSourceOver — the blit op the pixel view uses in
// translucent mode so its transparent holes reveal the effect views behind it.
const nsCompositingSourceOver = 2

// NSViewWidthSizable | NSViewHeightSizable — the framebuffer view tracks the
// container on resize.
const nsViewWidthHeightSizable = 2 | 16

var (
	selSetOpaque          = objc.RegisterName("setOpaque:")
	selSetBackgroundColor = objc.RegisterName("setBackgroundColor:")
	selClearColor         = objc.RegisterName("clearColor")
	selSetWantsLayer      = objc.RegisterName("setWantsLayer:")
	selFrame              = objc.RegisterName("frame")
	selSetFrame           = objc.RegisterName("setFrame:")
	selSetAutoresizing    = objc.RegisterName("setAutoresizingMask:")
	selAddSubview         = objc.RegisterName("addSubview:")
	selRemoveFromSuper    = objc.RegisterName("removeFromSuperview")
	selSetMaterial        = objc.RegisterName("setMaterial:")
	selSetBlendingMode    = objc.RegisterName("setBlendingMode:")
	selSetVfxState        = objc.RegisterName("setState:")
)

// materialConstant maps a toolkit MaterialKind onto its NSVisualEffectMaterial.
func materialConstant(kind toolkit.MaterialKind) int {
	switch kind {
	case toolkit.MaterialWindowBackground:
		return nsvfxWindowBackground
	case toolkit.MaterialSidebar:
		return nsvfxSidebar
	case toolkit.MaterialTitlebar:
		return nsvfxTitlebar
	case toolkit.MaterialMenu:
		return nsvfxMenu
	case toolkit.MaterialPopover:
		return nsvfxPopover
	case toolkit.MaterialHUD:
		return nsvfxHUDWindow
	default: // MaterialSelection and anything new
		return nsvfxSelection
	}
}

// blendingConstant maps a toolkit MaterialBlend onto an NSVisualEffectBlendingMode.
func blendingConstant(b toolkit.MaterialBlend) int {
	if b == toolkit.BlendWithinWindow {
		return nsvfxWithinWindow
	}
	return nsvfxBehindWindow
}

// syncMaterials reconciles the window's NSVisualEffectViews with the Materials in
// the current tree. It is called after layout (bounds are known) and after a
// resize. With no materials it does nothing, so the ordinary path is untouched.
//
// On the first call carrying a material it flips the window into translucent mode
// (see enableTranslucent), then, for every material, installs an effect view at
// its bounds, marks the material native-backed so its fallback stands down, and
// records the region as a hole to punch transparent in the framebuffer.
func (w *Window) syncMaterials(root toolkit.Widget) {
	mats := toolkit.CollectMaterials(root)
	if len(mats) == 0 {
		return
	}
	if !w.translucent {
		w.enableTranslucent()
	}
	// Rebuild the effect views from scratch — a handful of materials, once per
	// layout, so simplicity beats diffing.
	for _, ev := range w.effectViews {
		ev.Send(selRemoveFromSuper)
		ev.Send(selRelease)
	}
	w.effectViews = w.effectViews[:0]
	w.holes = w.holes[:0]
	w.materials = mats

	// Effect views must sit BEHIND the framebuffer view. Rather than the
	// variadic addSubview:positioned:relativeTo:, lift the framebuffer view out,
	// add the effect views, then re-add the framebuffer view so it lands on top —
	// using only plain addSubview:, which the reparent already proved works.
	w.view.Send(selRemoveFromSuper)
	containerH := objc.Send[nsRect](w.container, selBounds).Size.H
	for _, m := range mats {
		spec := m.Spec()
		ev := w.makeEffectView(spec, containerH)
		if ev == 0 {
			continue
		}
		w.effectViews = append(w.effectViews, ev)
		w.holes = append(w.holes, spec.Rect)
		m.SetNativeBacked(true)
	}
	w.container.Send(selAddSubview, w.view) // pixel view back on top
}

// makeEffectView builds one NSVisualEffectView for spec, framed under its region
// (render pixels, top-left) converted to the container's points, bottom-left, and
// adds it to the container (currently the frontmost subview; the caller re-adds
// the framebuffer view afterwards so the pixel view returns to the top). Returns
// 0 if NSVisualEffectView is unavailable.
func (w *Window) makeEffectView(spec toolkit.MaterialSpec, containerHpts float64) objc.ID {
	cls := objc.ID(objc.GetClass("NSVisualEffectView"))
	if cls == 0 {
		return 0
	}
	ev := cls.Send(selAlloc).Send(selInitWithFrame, effectFrame(spec.Rect, w.scale, containerHpts))
	ev.Send(selRetain)
	ev.Send(selSetMaterial, uint(materialConstant(spec.Kind)))
	ev.Send(selSetBlendingMode, uint(blendingConstant(spec.Blend)))
	ev.Send(selSetVfxState, uint(nsvfxStateActive))
	w.container.Send(selAddSubview, ev)
	return ev
}

// effectFrame converts a render-pixel, top-left rectangle to a point-sized,
// bottom-left NSRect in the container's coordinate space.
func effectFrame(r toolkit.Rect, scale, containerHpts float64) nsRect {
	xp := float64(r.X) / scale
	yp := float64(r.Y) / scale
	wp := float64(r.W) / scale
	hp := float64(r.H) / scale
	return nsRect{Origin: nsPoint{X: xp, Y: containerHpts - (yp + hp)}, Size: nsSize{W: wp, H: hp}}
}

// enableTranslucent flips the window into translucent compositing exactly once:
// the window goes non-opaque with a clear background, a plain layer-backed
// container becomes the content view, and the existing framebuffer view is
// re-parented on top of it (filling it, auto-resizing). Effect views are added
// between the two by syncMaterials.
func (w *Window) enableTranslucent() {
	w.translucent = true
	w.win.Send(selSetOpaque, false)
	clear := objc.ID(objc.GetClass("NSColor")).Send(selClearColor)
	w.win.Send(selSetBackgroundColor, clear)

	frame := objc.Send[nsRect](w.view, selFrame)
	container := objc.ID(objc.GetClass("NSView")).Send(selAlloc).Send(selInitWithFrame, frame)
	container.Send(selRetain)
	container.Send(selSetWantsLayer, true)
	w.win.Send(selSetContentView, container) // replaces the framebuffer view

	w.view.Send(selSetFrame, nsRect{Size: frame.Size})
	w.view.Send(selSetAutoresizing, uint(nsViewWidthHeightSizable))
	container.Send(selAddSubview, w.view)
	w.container = container
}

// punchHoles zeroes the framebuffer over each native material region so the
// effect views behind the pixel view show through. Called while w.mu is held,
// right after the tree is drawn. A no-op unless translucent.
func (w *Window) punchHoles() {
	if !w.translucent {
		return
	}
	for _, h := range w.holes {
		x0 := clamp(h.X, 0, w.w)
		y0 := clamp(h.Y, 0, w.h)
		x1 := clamp(h.X+h.W, 0, w.w)
		y1 := clamp(h.Y+h.H, 0, w.h)
		for y := y0; y < y1; y++ {
			row := (y*w.w + x0) * 4
			end := (y*w.w + x1) * 4
			for i := row; i < end; i++ {
				w.buf[i] = 0
			}
		}
	}
}

// clamp bounds v to [lo, hi].
func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}
