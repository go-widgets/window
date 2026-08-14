// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build integration && linux

// The live proof for the Repainter capability on X11, against a real server
// (Xvfb in CI). The name begins with TestLiveX11 because that is what the lane
// filters on — a test outside that filter never runs and only looks like proof.
package window

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// countingRoot paints one flat colour that changes on every frame, so a capture
// says not just "something is on screen" but WHICH frame is on screen.
type countingRoot struct {
	toolkit.Base
	mu    sync.Mutex
	frame int
}

func (r *countingRoot) Draw(p painter.Painter, _ *toolkit.Theme) {
	r.mu.Lock()
	r.frame++
	n := r.frame
	r.mu.Unlock()
	p.FillRect(r.Bounds(), frameColour(n))
}

func (r *countingRoot) OnEvent(toolkit.Event) {}

func (r *countingRoot) frames() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.frame
}

// frameColour is the nth frame's colour: far enough apart that a capture cannot
// confuse two of them, and never black or white, which is what an empty or
// unmapped window would give.
func frameColour(n int) painter.RGBA {
	return painter.RGBA{R: uint8(40 + 60*(n%4)), G: uint8(90 + 30*(n%3)), B: 200, A: 255}
}

// A window nobody touches must stay on screen unchanged, and a Repaint from
// another goroutine must put the next frame there.
//
// Both halves matter. Without the first, "the colour changed" proves nothing:
// an event loop that redraws on a timer would pass. Without the second, the
// capability is a no-op that nobody notices until an application shows its first
// frame forever — which is what X11 did before this, and what the reader's
// completed fetches ran into.
func TestLiveX11RepaintFromAnotherGoroutine(t *testing.T) {
	if os.Getenv("WINDOW_X11_INTEGRATION") == "" {
		t.Skip("set WINDOW_X11_INTEGRATION=1 (and run under an X server) to enable")
	}
	requireTool(t, "xdotool")
	requireTool(t, "import")

	const W, H = 160, 120
	title := fmt.Sprintf("gwwindow-repaint-%d", os.Getpid())

	b, err := Open(Config{Title: title, Class: "gwwindow", Width: W, Height: H})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	r, ok := b.(Repainter)
	if !ok {
		t.Fatalf("the X11 back-end %T does not implement Repainter", b)
	}
	root := &countingRoot{}
	done := make(chan error, 1)
	go func() { done <- b.Run(root) }()

	id := waitForWindowID(t, title)
	time.Sleep(700 * time.Millisecond) // the map and the first PutImage

	dir := t.TempDir()
	shot := func(name string) (rr, gg, bb uint32) {
		t.Helper()
		p := filepath.Join(dir, name+".png")
		mustRun(t, "import", "-window", id, p)
		img := decodePNG(t, p)
		c := img.At(W/2, H/2)
		r16, g16, b16, _ := c.RGBA()
		return r16 >> 8, g16 >> 8, b16 >> 8
	}

	first := frameColour(root.frames())
	r0, g0, b0 := shot("first")
	if r0 != uint32(first.R) || g0 != uint32(first.G) || b0 != uint32(first.B) {
		t.Fatalf("the first frame on screen is (%d,%d,%d), want frame %d's (%d,%d,%d)",
			r0, g0, b0, root.frames(), first.R, first.G, first.B)
	}

	// The control: left alone, the window must NOT repaint. If it did, the
	// assertion below would pass without the capability doing anything.
	idleFrames := root.frames()
	time.Sleep(1200 * time.Millisecond)
	if now := root.frames(); now != idleFrames {
		t.Fatalf("an idle window drew %d more frames on its own, so this test cannot "+
			"prove anything about Repaint", now-idleFrames)
	}
	r1, g1, b1 := shot("idle")
	if r1 != r0 || g1 != g0 || b1 != b0 {
		t.Fatalf("an idle window changed on screen from (%d,%d,%d) to (%d,%d,%d)", r0, g0, b0, r1, g1, b1)
	}

	// The capability, from a goroutine that is not the one in Run.
	ask := make(chan struct{})
	go func() { r.Repaint(); close(ask) }()
	<-ask

	deadline := time.Now().Add(5 * time.Second)
	for root.frames() == idleFrames && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if root.frames() == idleFrames {
		t.Fatal("Repaint from another goroutine never reached the run loop")
	}
	time.Sleep(300 * time.Millisecond) // let the server process the PutImage

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
