// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows && integration

// The live HiDPI proof, on a real Windows machine.
//
// Windows is the one back-end that always drew a LOGICAL framebuffer and let
// StretchDIBits up-sample it. That is a deliberate model — it is what keeps a UI
// readable at 200% with no DPI knowledge in the toolkit — but it left no way to
// ask for the panel's own pixels, which every other back-end now offers through
// RenderScale: NativeScale.
//
// The measurement is the same window twice on the same machine: default and
// native, side by side, with the monitor's DPI logged so a run at 100% says so
// rather than quietly proving nothing. At 96 dpi the two are identical BY
// DEFINITION, which is why the test insists on being told what the DPI was.
package win32

import (
	"os"
	"testing"
	"time"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// The window asked for, in logical points.
const dpiProbeW, dpiProbeH = 320, 240

// TestLiveWin32NativeScale opens ONE window -- the default model, or the native
// one when WINDOW_WIN32_NATIVE is set -- and reports the monitor scale, the
// framebuffer it allocated and the physical client it fills.
//
// One per run, because this back-end is single-window by design: a native GUI
// application owns the one main-thread message loop, and a second window in the
// same process would share the WNDPROC's single `active`. An earlier version of
// this test opened both and hung, which is the model telling the truth about
// itself.
//
// The two runs are compared by the script that launches them. What each run can
// assert on its own is the invariant: the default framebuffer is the LOGICAL
// size whatever the DPI, the native one is the physical client, and RenderScale
// says which. At 96 dpi those coincide, so the run also says what the scale was
// -- a green result at 100% is not evidence about HiDPI and must not read like
// one.
func TestLiveWin32NativeScale(t *testing.T) {
	skipUnlessLive(t)
	native := os.Getenv("WINDOW_WIN32_NATIVE") != ""

	w, err := NewScaled("go-widgets dpi proof", dpiProbeW, dpiProbeH, nil, native)
	if err != nil {
		t.Fatalf("NewScaled(native=%v): %v", native, err)
	}
	fw, fh := w.Size()
	t.Logf("MEASURE native=%v scale=%v framebuffer=%dx%d physical=%dx%d RenderScale=%v",
		native, w.scale, fw, fh, w.physW, w.physH, w.RenderScale())

	if native {
		if fw != w.physW || fh != w.physH {
			t.Errorf("the native framebuffer is %dx%d, want the physical client %dx%d",
				fw, fh, w.physW, w.physH)
		}
		if got := w.RenderScale(); got != w.scale {
			t.Errorf("the native RenderScale is %v, want the monitor's %v", got, w.scale)
		}
	} else {
		if fw != dpiProbeW || fh != dpiProbeH {
			t.Errorf("the default framebuffer is %dx%d, want the logical %dx%d",
				fw, fh, dpiProbeW, dpiProbeH)
		}
		if got := w.RenderScale(); got != 1 {
			t.Errorf("the default RenderScale is %v, want 1 -- its framebuffer IS one pixel per point", got)
		}
	}
	if w.scale <= 1 {
		t.Logf("NOTE this machine is at %v device pixels per point, so the two models "+
			"cannot differ here: run it at 200%% for that", w.scale)
	}

	// With WINDOW_WIN32_HOLD set the window is painted with one-pixel stripes and
	// left on screen, so a screendump taken from the host can measure what
	// actually reached the panel. Four flat quadrants would prove nothing here:
	// an up-sampled flat colour is pixel-identical to the real thing, while a
	// stripe cannot survive being doubled.
	hold, err := time.ParseDuration(os.Getenv("WINDOW_WIN32_HOLD"))
	if err != nil {
		return
	}
	root := &liveStripeRoot{}
	go func() { _ = w.Run(root) }()
	t.Logf("HOLD_BEGIN native=%v for %v", native, hold)
	time.Sleep(hold)
	t.Logf("HOLD_END native=%v", native)
	_ = w.Close()
}

// liveStripeRoot paints alternating one-pixel vertical stripes, which is what
// makes an up-sampled buffer distinguishable from a native one.
type liveStripeRoot struct{ toolkit.Base }

func (r *liveStripeRoot) Draw(p painter.Painter, _ *toolkit.Theme) {
	b := r.Bounds()
	for x := 0; x < b.W; x++ {
		c := painter.RGBA{R: 230, G: 30, B: 30, A: 255}
		if x%2 == 1 {
			c = painter.RGBA{R: 30, G: 230, B: 30, A: 255}
		}
		p.FillRect(painter.Rect{X: b.X + x, Y: b.Y, W: 1, H: b.H}, c)
	}
}

func (r *liveStripeRoot) OnEvent(toolkit.Event) {}
