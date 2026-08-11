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

import (
	"testing"
	"time"

	"github.com/go-macos/objc"
)

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

	// The scale is reported back, so a self-rendering root can turn framebuffer
	// pixels into the logical size the user sees. A window that lied here would
	// hand its renderer the wrong idea of what a point is worth.
	var reported float64
	callOnMain(func() {
		win, err := NewScaled("render scale readback", pw, ph, nil, -1)
		if err != nil {
			return
		}
		defer func() { _ = win.Close() }()
		reported = win.RenderScale()
	})
	if reported != backing {
		t.Errorf("RenderScale() = %v, want the backing factor %v it was asked to follow", reported, backing)
	}

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

// A window that asked to FOLLOW the panel must follow it when it moves.
//
// This needs two displays of DIFFERENT density, so it skips where there is only
// one — an environmental prerequisite, not a failure of the thing under test.
// It is the case that cannot be reproduced on a single-screen machine, and the
// fix for it was written and then discarded a day earlier because the machine
// reported only a 1x display: platform glue that cannot be exercised has no
// business being merged.
func TestLiveRenderScaleFollowsAMove(t *testing.T) {
	skipUnlessIntegration(t)

	const pw, ph = 320, 200
	var before, after float64
	var moved bool

	callOnMain(func() {
		// The window comes FIRST. NSScreen.screens is not populated until an
		// NSApplication exists, so asking before opening one reports a single
		// display on a machine that has two — which is how an earlier version
		// of this test skipped itself while a standalone probe, which happened
		// to open a window first, saw both screens. The same mistake produced a
		// confident wrong conclusion that the built-in panel was asleep.
		win, err := NewScaled("render scale move", pw, ph, nil, -1)
		if err != nil {
			t.Errorf("open: %v", err)
			return
		}
		defer func() { _ = win.Close() }()
		before = win.RenderScale()

		other, ok := screenWithBacking(2)
		if !ok {
			return
		}
		win.win.Send(objc.RegisterName("setFrame:display:"),
			nsRect{Origin: nsPoint{X: other.Origin.X + 80, Y: other.Origin.Y + 80},
				Size: nsSize{W: pw, H: ph}}, true)
		// The move is reported through viewDidChangeBackingProperties, which
		// arrives on the run loop rather than from setFrame: itself.
		runLoopFor(400 * time.Millisecond)
		after = win.RenderScale()
		moved = true
	})

	if !moved {
		t.Skip("only one display density available; this needs a 1x and a 2x screen")
	}
	if before != 1 {
		t.Fatalf("the window opened at %vx, so the move proves nothing", before)
	}
	if after != 2 {
		t.Errorf("after moving to a 2x panel the render scale is %v, want 2 — it asked to follow the panel", after)
	}
	t.Logf("render scale across the move: %vx -> %vx", before, after)
}

// screenWithBacking returns the frame of a screen at the given density.
func screenWithBacking(want float64) (nsRect, bool) {
	screens := objc.ID(objc.GetClass("NSScreen")).Send(objc.RegisterName("screens"))
	n := objc.Send[uint64](screens, objc.RegisterName("count"))
	for i := uint64(0); i < n; i++ {
		s := screens.Send(objc.RegisterName("objectAtIndex:"), i)
		if objc.Send[float64](s, objc.RegisterName("backingScaleFactor")) >= want {
			return objc.Send[nsRect](s, objc.RegisterName("frame")), true
		}
	}
	return nsRect{}, false
}

// runLoopFor turns the main run loop so queued AppKit callbacks are delivered.
func runLoopFor(d time.Duration) {
	rl := objc.ID(objc.GetClass("NSRunLoop")).Send(objc.RegisterName("currentRunLoop"))
	until := objc.ID(objc.GetClass("NSDate")).Send(objc.RegisterName("dateWithTimeIntervalSinceNow:"), d.Seconds())
	rl.Send(objc.RegisterName("runUntilDate:"), until)
}
