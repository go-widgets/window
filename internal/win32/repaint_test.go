// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package win32

import (
	"sync"
	"testing"
)

// One wakeup in flight, and the next one still works.
//
// Windows coalesces WM_PAINT on our behalf but not a private WM_APP, so a
// caller repainting at 60 Hz against a busy pump would queue one message per
// tick and redraw once per queued message on catching up.
func TestRepaintFlagCoalesces(t *testing.T) {
	var f repaintFlag

	if !f.arm() {
		t.Fatal("the first arm declined to post a wakeup")
	}
	for i := 0; i < 4; i++ {
		if f.arm() {
			t.Fatalf("arm %d posted a second wakeup while one was in flight", i+2)
		}
	}

	f.take() // the pump has picked the message up
	if !f.arm() {
		t.Error("after the pump took the message, the next Repaint posted nothing: the flag is a latch, not a coalescer")
	}
}

// A message that could not be posted must not leave the flag armed, or every
// later Repaint is silenced by a wakeup that never left.
func TestRepaintFlagDisarm(t *testing.T) {
	var f repaintFlag

	if !f.arm() {
		t.Fatal("the first arm declined to post a wakeup")
	}
	f.disarm()
	if !f.arm() {
		t.Error("a disarmed flag still refused the next wakeup")
	}
}

// Repaint is documented as safe from any goroutine, so the flag has to be. The
// count is what matters: exactly one of the concurrent callers may post.
func TestRepaintFlagIsConcurrent(t *testing.T) {
	var f repaintFlag
	var mu sync.Mutex
	posted := 0

	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if f.arm() {
				mu.Lock()
				posted++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()

	if posted != 1 {
		t.Errorf("%d of 64 concurrent Repaints posted a wakeup, want exactly 1", posted)
	}
}

// The message id must be one Windows will never send us. WM_APP is where the
// system stops and an application begins.
func TestRepaintMessageIsPrivate(t *testing.T) {
	const wmApp = 0x8000
	if WMAppRepaint < wmApp {
		t.Errorf("WMAppRepaint = %#x, which is below WM_APP (%#x) and so may collide with a system message",
			WMAppRepaint, wmApp)
	}
}
