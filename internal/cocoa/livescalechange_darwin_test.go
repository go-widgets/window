// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin && integration

package cocoa

import (
	"testing"

	"github.com/go-macos/objc"
	"github.com/go-widgets/toolkit"
)

// A window that follows the panel and is moved to a display of another density
// must tell its owner the new scale BEFORE it lays the frame out again.
//
// The toolkit lays out in framebuffer pixels and learns what a point is worth
// from toolkit.SetMetricScale, which the window package called once, at open.
// Dragging the window from a 1x to a 2x display changed the framebuffer and the
// divisor of every embedded native control's frame, but not the metric scale, so
// the controls came out half the size they were laid out for (and double, the
// other way).
//
// A single-display machine cannot make AppKit report a move, so the move is
// simulated the way the callback sees it: the window is put back to the density
// it "opened" on, and the callback AppKit would deliver is invoked on the
// receiver AppKit would pass. The witness is the real backingScaleFactor the
// window then reads, so the test means the same on a 1x or a 2x machine only
// when the two differ -- it skips where the panel is 1x.
func TestLiveScaleChangeReachesTheOwnerOfTheWindowThatMoved(t *testing.T) {
	skipUnlessIntegration(t)

	var (
		skipped                bool
		mainGot, childGot      []float64
		childScale, mainScale  float64
		childFrameW, wantFrame float64
	)
	callOnMain(func() {
		main, err := NewWithOptions(Options{Title: "scale change main", Width: 320, Height: 200, RenderScale: -1,
			OnScale: func(s float64) { mainGot = append(mainGot, s) }})
		if err != nil {
			t.Errorf("open: %v", err)
			return
		}
		defer func() { _ = main.Close() }()
		if main.backing <= 1 {
			skipped = true
			return
		}
		surf := toolkit.NewSurface(func() ([]byte, int, int) { return make([]byte, 320*200*4), 320, 200 })
		surf.Controls = func() []toolkit.NativeControl {
			return []toolkit.NativeControl{{
				Kind: toolkit.NativeSecureEntry, Key: "pw", Visible: true,
				Rect: toolkit.Rect{X: 10, Y: 10, W: 200, H: 24},
			}}
		}
		child, err := main.OpenChild(Options{Title: "scale change child", Width: 320, Height: 200, RenderScale: -1,
			OnScale: func(s float64) { childGot = append(childGot, s) }}, surf, nil)
		if err != nil {
			t.Errorf("open child: %v", err)
			return
		}

		// Both windows "opened" on a 1x panel; only the CHILD is then moved.
		for _, w := range []*Window{main, child} {
			w.resize(320, 200, 1)
		}
		mainGot, childGot = nil, nil
		viewDidChangeBackingProperties(child.view, 0)

		childScale, mainScale = child.RenderScale(), main.RenderScale()
		// The embedded control is the content view's only subview (it is not clipped).
		subs := child.view.Send(objc.RegisterName("subviews"))
		if objc.Send[uint64](subs, objc.RegisterName("count")) == 1 {
			v := subs.Send(objc.RegisterName("objectAtIndex:"), uint64(0))
			childFrameW = objc.Send[nsRect](v, objc.RegisterName("frame")).Size.W
		}
		wantFrame = 200 / child.backing
	})
	if skipped {
		t.Skip("the panel is 1x: a move to a different density cannot be simulated")
	}
	if len(childGot) != 1 || childGot[0] != childScale || childScale <= 1 {
		t.Errorf("child OnScale = %v, scale %v; want exactly one call with the new scale > 1", childGot, childScale)
	}
	if len(mainGot) != 0 || mainScale != 1 {
		t.Errorf("main OnScale = %v, scale %v; the callback acted on the wrong window", mainGot, mainScale)
	}
	if childFrameW != wantFrame {
		t.Errorf("native control width = %v points, want %v (200 pixels / %vx)", childFrameW, wantFrame, childScale)
	}
}
