// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin && integration

// The live proof that the render scale reaches the framebuffer. It opens real
// NSWindows, so it runs only under -tags=integration with
// WINDOW_COCOA_INTEGRATION set.
//
// The witness is the window's own backingScaleFactor, read from AppKit rather
// than assumed: a machine with a 1x display must see 1x here and the test still
// has to mean something there, so it asserts the RELATIONSHIP (framebuffer =
// points x backing) rather than a hard-coded 2.
package cocoa

import "testing"

func TestLiveRenderScaleFollowsThePanel(t *testing.T) {
	skipUnlessIntegration(t)

	const pw, ph = 320, 200

	// Every AppKit call goes through callOnMain: TestMain reserves the process
	// main OS thread, and creating an NSWindow anywhere else crashes rather than
	// erroring, which is how the first run of this test failed.
	measure := func(scale float64) (w, h int, backing float64, err error) {
		callOnMain(func() {
			var win *Window
			win, err = NewScaled("render scale proof", pw, ph, nil, scale)
			if err != nil {
				return
			}
			defer func() { _ = win.Close() }()
			w, h = win.Size()
			backing = win.backing
		})
		return w, h, backing, err
	}

	w, h, _, err := measure(0)
	if err != nil {
		t.Fatalf("logical window: %v", err)
	}
	if w != pw || h != ph {
		t.Errorf("default framebuffer = %dx%d, want the logical %dx%d", w, h, pw, ph)
	}

	nw, nh, backing, err := measure(-1)
	if err != nil {
		t.Fatalf("native window: %v", err)
	}
	if backing <= 0 {
		t.Fatal("the window reports no backing scale factor")
	}
	wantW, wantH := int(float64(pw)*backing), int(float64(ph)*backing)
	if nw != wantW || nh != wantH {
		t.Errorf("native framebuffer = %dx%d, want %dx%d (%.0fx backing)", nw, nh, wantW, wantH, backing)
	}
	t.Logf("backing=%.0fx logical=%dx%d native=%dx%d", backing, pw, ph, nw, nh)

	// An explicit scale is used as given, neither clamped nor rounded to the
	// panel: a caller asking for 3x on a 2x display means it.
	ew, eh, _, err := measure(3)
	if err != nil {
		t.Fatalf("explicit window: %v", err)
	}
	if ew != pw*3 || eh != ph*3 {
		t.Errorf("explicit 3x framebuffer = %dx%d, want %dx%d", ew, eh, pw*3, ph*3)
	}
}
