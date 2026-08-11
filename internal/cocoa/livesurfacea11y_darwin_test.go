// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin && integration

// The live proof that a root which renders its OWN pixels publishes an
// accessibility tree. It opens a real NSWindow, so it runs only under
// -tags=integration with WINDOW_COCOA_INTEGRATION set.
//
// This is a regression test with a specific history. refreshA11y used to run
// BEFORE the draw, which for a widget tree is indistinguishable — it describes
// itself the same either way — but for a toolkit.Surface it published the
// PREVIOUS frame's tree. On the first paint that is no tree at all, and with
// nothing repainting an idle window, the application stayed unreadable for as
// long as it ran. It was found by pointing a real accessibility client at a
// real application, not by a unit test, because every part in isolation was
// correct.
package cocoa

import (
	"testing"

	"github.com/go-macos/objc"
	"github.com/go-widgets/toolkit"
)

func TestLiveSurfaceRootPublishesItsTreeOnTheFirstPaint(t *testing.T) {
	skipUnlessIntegration(t)

	const w, h = 320, 200
	pix := make([]byte, w*h*4*4) // generous: the surface may be asked at 2x

	surf := toolkit.NewSurface(func() ([]byte, int, int) { return pix, w, h })
	// Elements answers only AFTER the frame has been asked for, exactly like an
	// application that learns what it is showing by drawing it.
	drawn := false
	surf.Frame = func() ([]byte, int, int) {
		drawn = true
		return pix, w, h
	}
	surf.Elements = func() []toolkit.SurfaceElement {
		if !drawn {
			return nil
		}
		return []toolkit.SurfaceElement{
			{Role: toolkit.RoleButton, Name: "Refresh", X: 4, Y: 4, W: 40, H: 20},
			{Role: toolkit.RoleText, Name: "Today", X: 4, Y: 30, W: 100, H: 20},
		}
	}

	// Read back what was PUSHED to AppKit, not what the tree would say if asked
	// again now. Re-walking the widget tree at assertion time passes whether the
	// publication happened before or after the draw -- the first version of this
	// test did exactly that and was green against the bug it was written for.
	selChildren := objc.RegisterName("accessibilityChildren")
	selCount := objc.RegisterName("count")

	var nodes int
	callOnMain(func() {
		win, err := NewScaled("surface a11y", w, h, nil, 0)
		if err != nil {
			t.Errorf("open: %v", err)
			return
		}
		defer func() { _ = win.Close() }()
		win.root = surf
		win.paintFrame(false)
		if kids := win.view.Send(selChildren); kids != 0 {
			nodes = int(objc.Send[uint64](kids, selCount))
		}
	})

	if !drawn {
		t.Fatal("the surface was never asked for a frame")
	}
	if nodes != 2 {
		t.Errorf("AppKit holds %d accessibility children after one paint, want 2 — the tree is being published before the frame that fills it", nodes)
	}
}
