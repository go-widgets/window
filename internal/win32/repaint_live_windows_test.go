// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows && integration

// The live proof for the Repainter capability, on a real Windows machine.
//
// It runs as a scheduled task in the VM's INTERACTIVE session, the way the
// other live Windows proofs do: a window created from session 0 exists but is
// on no desktop, so nothing could be captured from it. The schedule is fixed
// and generous so a screendump taken from the host lands squarely inside each
// hold — the frame counts here are self-checking, and the captures are what
// show the pixels actually changed.
package win32

import (
	"os"
	"sync"
	"testing"
	"time"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// The two holds, long enough that a capture taken by hand or by a script lands
// inside one of them rather than on a boundary.
const (
	liveIdleHold     = 6 * time.Second
	liveRepaintHold  = 6 * time.Second
	liveSettleBefore = 2 * time.Second
)

// liveCountingRoot paints one flat colour per frame, so a capture says which
// frame is on screen rather than merely that something is.
type liveCountingRoot struct {
	toolkit.Base
	mu sync.Mutex
	n  int
}

func (r *liveCountingRoot) Draw(p painter.Painter, _ *toolkit.Theme) {
	r.mu.Lock()
	r.n++
	n := r.n
	r.mu.Unlock()
	// Frame 1 is a strong red, frame 2 a strong blue: two colours nobody can
	// confuse in a screendump, and neither the desktop's nor a default window
	// background.
	c := painter.RGBA{R: 220, G: 30, B: 30, A: 255}
	if n%2 == 0 {
		c = painter.RGBA{R: 30, G: 60, B: 220, A: 255}
	}
	p.FillRect(r.Bounds(), c)
}

func (r *liveCountingRoot) OnEvent(toolkit.Event) {}

func (r *liveCountingRoot) frames() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}

// A window nobody is touching must stay as it is, and a Repaint from another
// goroutine must produce the next frame.
func TestLiveWin32RepaintFromAnotherGoroutine(t *testing.T) {
	skipUnlessLive(t)

	w, err := New("go-widgets repaint proof", 320, 240, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	root := &liveCountingRoot{}

	go func() {
		time.Sleep(liveSettleBefore)

		idle := root.frames()
		t.Logf("IDLE_HOLD_BEGIN frames=%d (the window is RED and must stay red)", idle)
		time.Sleep(liveIdleHold)
		if now := root.frames(); now != idle {
			t.Errorf("an idle window drew %d more frames on its own, so this proves nothing about Repaint",
				now-idle)
		}
		t.Logf("IDLE_HOLD_END frames=%d", root.frames())

		w.Repaint()
		deadline := time.Now().Add(5 * time.Second)
		for root.frames() == idle && time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
		}
		if root.frames() == idle {
			t.Error("Repaint from another goroutine never reached the message loop")
		}
		t.Logf("REPAINTED frames=%d (the window is now BLUE)", root.frames())
		time.Sleep(liveRepaintHold)

		w.Close()
	}()

	// Run blocks on the message pump until the window is closed above. It has to
	// be on this goroutine: New locked it to the OS thread that owns the queue.
	if err := w.Run(root); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if root.frames() < 2 {
		t.Errorf("the window drew %d frames in total, want at least 2", root.frames())
	}
	t.Logf("DONE frames=%d pid=%d", root.frames(), os.Getpid())
}
