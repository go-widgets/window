// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build integration && linux

// The live HiDPI proof, on a real compositor whose output is a scale-2 one.
//
// It is deliberately NOT named TestLiveWayland..., because the ordinary live
// lane filters on that prefix and runs against a scale-1 sway: a HiDPI test
// there would fail for the environment rather than for the code. This runs in a
// lane of its own, against a second sway configured with `output HEADLESS-1
// scale 2`, and filters on TestLiveWlHiDPI.
//
// What makes the capture decisive is the pattern. Four flat quadrants would
// prove nothing: a 2× upscale of flat colour is pixel-identical to the real
// thing. So the window paints ALTERNATING ONE-PIXEL STRIPES, which cannot
// survive an upscale — from a half-resolution buffer every stripe comes back
// two pixels wide. Counting colour changes along one scanline of the compositor's
// own output therefore says, in one number, at what resolution the window was
// really drawn.
package window

import (
	"fmt"
	"image"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// stripeRoot paints alternating one-pixel vertical stripes over its whole area.
type stripeRoot struct{ toolkit.Base }

func (r *stripeRoot) Draw(p painter.Painter, _ *toolkit.Theme) {
	b := r.Bounds()
	for x := 0; x < b.W; x++ {
		c := painter.RGBA{R: 230, G: 30, B: 30, A: 255}
		if x%2 == 1 {
			c = painter.RGBA{R: 30, G: 230, B: 30, A: 255}
		}
		p.FillRect(painter.Rect{X: b.X + x, Y: b.Y, W: 1, H: b.H}, c)
	}
}

func (r *stripeRoot) OnEvent(toolkit.Event) {}

// runsAcross counts how many pixels wide the stripes are in the captured image,
// by walking one scanline and measuring the runs of equal colour.
//
// The median run rather than the mean: a window's edge, a border pixel or an
// anti-aliased seam contributes one long run, and one outlier must not move the
// answer.
func runsAcross(t *testing.T, img image.Image, y int) int {
	t.Helper()
	b := img.Bounds()
	var runs []int
	cur, prev := 0, uint32(0)
	for x := b.Min.X; x < b.Max.X; x++ {
		r, g, bl, _ := img.At(x, y).RGBA()
		key := (r>>8)<<16 | (g>>8)<<8 | (bl >> 8)
		if x == b.Min.X || key == prev {
			cur++
		} else {
			runs = append(runs, cur)
			cur = 1
		}
		prev = key
	}
	runs = append(runs, cur)
	if len(runs) < 8 {
		t.Fatalf("only %d colour runs across the scanline: the window is not showing stripes at all", len(runs))
	}
	// The median of the interior runs.
	interior := runs[1 : len(runs)-1]
	for i := 1; i < len(interior); i++ {
		for j := i; j > 0 && interior[j] < interior[j-1]; j-- {
			interior[j], interior[j-1] = interior[j-1], interior[j]
		}
	}
	return interior[len(interior)/2]
}

// captureStripeWidth opens a window with the given render scale, lets the
// compositor show it, captures the screen and reports how wide one stripe came
// out.
func captureStripeWidth(t *testing.T, name string, renderScale float64) int {
	t.Helper()
	b, err := Open(Config{
		Title:       fmt.Sprintf("gw-hidpi-%s-%d", name, os.Getpid()),
		Class:       "gwwltest",
		Width:       200,
		Height:      150,
		RenderScale: renderScale,
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	root := &stripeRoot{}
	done := make(chan error, 1)
	go func() { done <- b.Run(root) }()
	defer func() {
		_ = b.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
		}
	}()

	time.Sleep(2 * time.Second) // map, configure, first present

	if s, ok := b.(Scaler); ok {
		t.Logf("%s: the back-end reports RenderScale %v", name, s.RenderScale())
	}
	w, h := b.Size()
	t.Logf("%s: framebuffer %dx%d", name, w, h)

	shot := filepath.Join(t.TempDir(), name+".png")
	mustRun(t, "grim", shot)
	img := decodePNG(t, shot)
	if data, err := os.ReadFile(shot); err == nil {
		_ = os.WriteFile("live-hidpi-"+name+".png", data, 0o644)
	}
	return runsAcross(t, img, img.Bounds().Dy()/2)
}

// On a scale-2 screen, a window that asked for the screen's own resolution
// draws stripes that are ONE output pixel wide; one that did not gets two,
// because the compositor is stretching a half-resolution buffer.
//
// Both halves run here, in the same lane, on the same compositor: the second is
// the control, and without it "the stripes are one pixel wide" would be a claim
// about the capture rather than about the code.
func TestLiveWlHiDPIBufferScale(t *testing.T) {
	if os.Getenv("WINDOW_WAYLAND_HIDPI") == "" {
		t.Skip("set WINDOW_WAYLAND_HIDPI=1 (under a scale-2 compositor) to enable")
	}
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Fatal("WAYLAND_DISPLAY is not set")
	}
	requireTool(t, "grim")

	native := captureStripeWidth(t, "native", NativeScale)
	if native != 1 {
		t.Errorf("with NativeScale a stripe is %d output pixels wide, want 1 -- "+
			"the window is not drawing at the panel's resolution", native)
	} else {
		t.Log("live HiDPI: NativeScale draws one stripe per output pixel")
	}

	upscaled := captureStripeWidth(t, "logical", 0)
	if upscaled != 2 {
		t.Errorf("without NativeScale a stripe is %d output pixels wide, want 2 -- "+
			"the control does not show the compositor upscaling, so the measurement above "+
			"proves nothing", upscaled)
	} else {
		t.Log("live HiDPI: without it, every stripe comes back two pixels wide (the compositor upscaling)")
	}
}
