// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build integration && linux

// The live proof for the Repainter capability on Wayland, against a real
// compositor (headless sway in CI). The name begins with TestLiveWayland
// because that is what the lane filters on — a test outside that filter never
// runs and only looks like proof.
//
// countingRoot and frameColour are shared with the X11 proof: the two back-ends
// are asked the same question, and an answer that differed between them would be
// worth seeing.
package window

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A window nobody is talking to must stay as it is, and a Repaint from another
// goroutine must put the next frame on the screen.
//
// The control matters as much as the assertion. Wayland has a frame-callback
// mechanism a compositor uses to pace animation, and a back-end that repainted
// on every callback would pass the second half of this test while doing nothing
// the capability promises. So the window is left alone first, and has to prove
// it is genuinely idle.
func TestLiveWaylandRepaintFromAnotherGoroutine(t *testing.T) {
	if os.Getenv("WINDOW_WAYLAND_INTEGRATION") == "" {
		t.Skip("set WINDOW_WAYLAND_INTEGRATION=1 (under a Wayland compositor) to enable")
	}
	requireTool(t, "grim")

	b, err := Open(Config{Title: fmt.Sprintf("gwwl-repaint-%d", os.Getpid()),
		Class: "gwwltest", Width: 200, Height: 160})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	r, ok := b.(Repainter)
	if !ok {
		t.Fatalf("the Wayland back-end %T does not implement Repainter", b)
	}
	root := &countingRoot{}
	done := make(chan error, 1)
	go func() { done <- b.Run(root) }()

	time.Sleep(1500 * time.Millisecond) // map, configure, first present

	dir := t.TempDir()
	shot := func(name string) (rr, gg, bb uint32) {
		t.Helper()
		p := filepath.Join(dir, name+".png")
		mustRun(t, "grim", p)
		img := decodePNG(t, p)
		ib := img.Bounds()
		c := img.At(ib.Dx()/2, ib.Dy()/2)
		r16, g16, b16, _ := c.RGBA()
		return r16 >> 8, g16 >> 8, b16 >> 8
	}

	first := frameColour(root.frames())
	r0, g0, b0 := shot("first")
	if r0 != uint32(first.R) || g0 != uint32(first.G) || b0 != uint32(first.B) {
		t.Fatalf("the first frame on screen is (%d,%d,%d), want frame %d's (%d,%d,%d)",
			r0, g0, b0, root.frames(), first.R, first.G, first.B)
	}

	// The control: left alone, the window must not repaint.
	idle := root.frames()
	time.Sleep(1200 * time.Millisecond)
	if now := root.frames(); now != idle {
		t.Fatalf("an idle window drew %d more frames on its own, so this test cannot "+
			"prove anything about Repaint", now-idle)
	}

	// The capability, from a goroutine that is not the one in Run.
	ask := make(chan struct{})
	go func() { r.Repaint(); close(ask) }()
	<-ask

	deadline := time.Now().Add(5 * time.Second)
	for root.frames() == idle && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if root.frames() == idle {
		t.Fatal("Repaint from another goroutine never reached the run loop")
	}
	time.Sleep(500 * time.Millisecond) // let the compositor present the new buffer

	want := frameColour(root.frames())
	r2, g2, b2 := shot("repainted")
	if r2 != uint32(want.R) || g2 != uint32(want.G) || b2 != uint32(want.B) {
		t.Errorf("after Repaint the window shows (%d,%d,%d), want frame %d's (%d,%d,%d)",
			r2, g2, b2, root.frames(), want.R, want.G, want.B)
	} else {
		t.Logf("live repaint: frame %d reached the screen as (%d,%d,%d) with no input at all",
			root.frames(), r2, g2, b2)
	}

	if err := b.Close(); err != nil {
		t.Logf("close: %v", err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Log("the run loop did not exit promptly after close")
	}
}
