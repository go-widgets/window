// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// DamageRenderer is the OPT-IN capability a root handed to Run may implement to
// drive incremental (damage-region) present instead of full-surface present.
//
// A plain toolkit.Widget root keeps the full-surface path: every frame the
// whole framebuffer is repainted and blitted (correct, simple, unchanged). A
// root that ALSO implements DamageRenderer lets Run repaint and blit ONLY the
// rectangles that actually changed: Run draws the frame through RenderDamaged,
// takes the returned damage, and packs+presents just its (coalesced) union via
// the backend's small-rect present path (X11 MIT-SHM ShmPutImage over a
// framebuffer-mirroring segment, or a wl_shm sub-rect DamageBuffer). The very
// first frame, a resize and an X11 Expose still present the full surface — a
// resize because the framebuffer is reallocated, an Expose because the server
// discarded the window's contents — after which Run resumes incremental present.
//
// github.com/go-widgets/toolkit/scene provides the reference implementation
// (scene.HostRoot), which is pixel-identical to a full repaint by construction;
// the interface is declared here, structurally, so the backend needs no import
// of the scene layer.
type DamageRenderer interface {
	// RenderDamaged paints this frame into p (the framebuffer painter, whose
	// clip seam confines each rectangle's repaint to the damage) and returns
	// the rectangles it repainted, in surface pixel coordinates. An empty
	// result means nothing changed this frame, so the backend presents nothing.
	// The returned slice need only stay valid until the backend has presented
	// it (which it does immediately, before the next frame).
	RenderDamaged(p painter.Painter, th *toolkit.Theme) []toolkit.Rect
}

// rectContains reports whether outer wholly covers inner. An empty inner is
// contained by anything; nothing non-empty is contained by an empty outer.
func rectContains(outer, inner toolkit.Rect) bool {
	if inner.W <= 0 || inner.H <= 0 {
		return true
	}
	if outer.W <= 0 || outer.H <= 0 {
		return false
	}
	return inner.X >= outer.X && inner.Y >= outer.Y &&
		inner.X+inner.W <= outer.X+outer.W && inner.Y+inner.H <= outer.Y+outer.H
}

// addDamage merges r into set, keeping it coalesced by containment: an empty r
// is dropped, an r already covered by a member is dropped, and every member r
// subsumes is removed. This bounds a per-buffer damage accumulator to the
// working set instead of growing one rectangle per frame.
func addDamage(set []toolkit.Rect, r toolkit.Rect) []toolkit.Rect {
	if r.W <= 0 || r.H <= 0 {
		return set
	}
	for _, e := range set {
		if rectContains(e, r) {
			return set // already covered
		}
	}
	n := 0
	for _, e := range set {
		if !rectContains(r, e) { // keep members r does NOT subsume
			set[n] = e
			n++
		}
	}
	return append(set[:n], r)
}

// clampRect intersects r with the w×h surface, returning a possibly-empty
// rectangle (W or H <= 0 when r lies fully off-surface).
func clampRect(r toolkit.Rect, w, h int) toolkit.Rect {
	x, y, rw, rh := r.X, r.Y, r.W, r.H
	if x < 0 {
		rw += x
		x = 0
	}
	if y < 0 {
		rh += y
		y = 0
	}
	if x+rw > w {
		rw = w - x
	}
	if y+rh > h {
		rh = h - y
	}
	return toolkit.Rect{X: x, Y: y, W: rw, H: rh}
}
