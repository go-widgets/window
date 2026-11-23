// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package gtk

import (
	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// damageRenderer is the OPT-IN incremental-present capability, declared
// structurally so this package needs no import of the window package. It
// mirrors window.DamageRenderer exactly: RenderDamaged paints only what changed
// into p and returns the rectangles it repainted, in framebuffer pixels.
type damageRenderer interface {
	RenderDamaged(p painter.Painter, th *toolkit.Theme) []toolkit.Rect
}

// target is where a frame is presented: the GtkPicture in production, a
// recording mirror in the tests. buf is always the WHOLE framebuffer.
type target interface {
	// Full presents the whole frame and returns the bytes of buf it COPIED (0 when
	// the target shows buf's own memory).
	Full(buf []byte, w, h int) (copied int)
	// Update presents buf telling the target that only rects differ from what it
	// shows now, and returns the bytes it copied. ok is false, having presented
	// nothing, when it cannot (the runtime lacks the GTK 4.16 texture-update API,
	// or nothing is shown yet); the caller then presents the whole frame.
	Update(buf []byte, w, h int, rects []toolkit.Rect) (copied int, ok bool)
}

// framer decides, frame by frame, whether to present the whole framebuffer or
// only the rectangles a DamageRenderer root reports.
type framer struct {
	dmg    damageRenderer // non-nil when the root opts into incremental present
	seeded bool           // a whole frame has been presented since bind
}

// frameResult describes what one frame presented, for tests and measurement.
type frameResult struct {
	Whole  bool           // the whole frame was presented
	Rects  []toolkit.Rect // the damage handed to the target (clamped), when !Whole
	Pixels int            // pixels the presented region covers (4 bytes each)
	Copied int            // bytes the target copied to present it (0: none)
}

// bind records the root's capability. A new root is presented whole first.
func (f *framer) bind(root toolkit.Widget) {
	f.dmg, _ = root.(damageRenderer)
	f.seeded = false
}

// resize forgets what the target shows: the framebuffer was reallocated, so the
// next frame is presented whole and damage resumes after it.
func (f *framer) resize() { f.seeded = false }

// fbSize is the framebuffer, in pixels, for a window allocated allocW×allocH
// points at scale pixels per point. ok is false while there is no allocation yet.
func fbSize(allocW, allocH int, scale float64) (w, h int, ok bool) {
	if allocW <= 0 || allocH <= 0 || scale <= 0 {
		return 0, 0, false
	}
	return max(1, int(float64(allocW)*scale)), max(1, int(float64(allocH)*scale)), true
}

// fitSize decides what Window.fit does: given the window's allocation and the
// current framebuffer size curW×curH, it returns the size to reallocate to and
// whether to reallocate at all. No allocation yet, or an unchanged size, changes
// nothing.
func fitSize(allocW, allocH int, scale float64, curW, curH int) (w, h int, changed bool) {
	w, h, ok := fbSize(allocW, allocH, scale)
	if !ok || (w == curW && h == curH) {
		return curW, curH, false
	}
	return w, h, true
}

// incremental reports whether the bound root reports its own damage.
func (f *framer) incremental() bool { return f.dmg != nil }

// present draws one frame of an incremental root into buf (w×h pixels) and
// hands it to t. The FIRST frame is presented whole (its seed damage is consumed
// and the target holds nothing yet), an empty report presents nothing, and any
// other frame presents the clamped damage through t.Update, falling back to a
// whole present if t cannot. It must only be called when incremental() is true.
func (f *framer) present(root toolkit.Widget, th *toolkit.Theme, buf []byte, w, h int, t target) frameResult {
	frame := toolkit.Rect{X: 0, Y: 0, W: w, H: h}
	root.SetBounds(frame)
	rects := f.dmg.RenderDamaged(painter.NewPixelPainter(buf, w, h), th)
	whole := frameResult{Whole: true, Pixels: frame.Area()}
	if !f.seeded {
		f.seeded = true
		whole.Copied = t.Full(buf, w, h)
		return whole
	}
	var clamped []toolkit.Rect
	pixels := 0
	for _, r := range rects {
		if c := r.Intersect(frame); !c.Empty() {
			clamped = append(clamped, c)
			pixels += c.Area()
		}
	}
	if len(clamped) == 0 {
		return frameResult{}
	}
	copied, ok := t.Update(buf, w, h, clamped)
	if !ok {
		whole.Copied = t.Full(buf, w, h)
		return whole
	}
	return frameResult{Rects: clamped, Pixels: pixels, Copied: copied}
}
