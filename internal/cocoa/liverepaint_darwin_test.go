// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin && integration

// The live proof that a repaint can be asked for from another goroutine, which
// is the whole point: an application whose content arrives on its own has no
// event to ride in on.
package cocoa

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/go-macos/objc"
	"github.com/go-widgets/toolkit"
)

func TestLiveRepaintFromAnotherGoroutine(t *testing.T) {
	skipUnlessIntegration(t)

	var frames int64
	surf := toolkit.NewSurface(func() ([]byte, int, int) {
		atomic.AddInt64(&frames, 1)
		return make([]byte, 320*200*4), 320, 200
	})

	var win *Window
	callOnMain(func() {
		var err error
		win, err = NewScaled("repaint", 320, 200, nil, 0)
		if err != nil {
			t.Errorf("open: %v", err)
			return
		}
		win.root = surf
		win.paintFrame(false) // the frame an idle window would stop at
	})
	if win == nil {
		return
	}
	defer func() { callOnMain(func() { _ = win.Close() }) }()

	first := atomic.LoadInt64(&frames)
	if first == 0 {
		t.Fatal("the first paint never asked for a frame")
	}

	// Ask from a goroutine that is not the main thread, exactly as a fetch
	// completing would, and let the main run loop turn.
	done := make(chan struct{})
	go func() { win.Repaint(); close(done) }()
	<-done

	deadline := time.Now().Add(2 * time.Second)
	for atomic.LoadInt64(&frames) == first && time.Now().Before(deadline) {
		callOnMain(func() {
			// Turn the run loop so the queued selector is delivered.
			objc.ID(objc.GetClass("NSRunLoop")).Send(objc.RegisterName("currentRunLoop")).
				Send(objc.RegisterName("runUntilDate:"),
					objc.ID(objc.GetClass("NSDate")).Send(objc.RegisterName("dateWithTimeIntervalSinceNow:"), 0.05))
		})
	}

	if got := atomic.LoadInt64(&frames); got == first {
		t.Errorf("frames still %d after Repaint from another goroutine — the request never reached the main thread", got)
	} else {
		t.Logf("frames %d -> %d", first, got)
	}
}
