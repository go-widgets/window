// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"sync"
	"testing"
)

// Close and painting may run at the same time, because Close may be called from
// any goroutine.
//
// This is not a hypothetical. Closing unmaps the shared memory the paint path
// writes into — the MIT-SHM segment here, the wl_shm pool on Wayland — so the
// two racing is not a wrong pixel but a SEGMENTATION FAULT, which is how it
// showed up: a whole CI lane died inside PackARGB8888 when a live test closed
// its windows while their loops were still drawing.
//
// Run under -race this fails on the unguarded version and passes on the guarded
// one; run without, it is still worth having, since the fault it provokes is
// fatal either way.
func TestCloseWhilePainting(t *testing.T) {
	w, _ := dialFake(t, Config{Width: 64, Height: 48})
	w.root = &recWidget{}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			// The frame the run loop would be painting. It must be safe to be in
			// here when somebody else closes the window.
			_ = w.paintFrame(false, false)
		}
	}()

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	wg.Wait()

	// Closing twice is not a second teardown, from any goroutine or in any order.
	if err := w.Close(); err != nil {
		t.Errorf("the second Close returned %v, want nil", err)
	}
}

// Several goroutines closing at once still tear the window down exactly once.
func TestCloseIsIdempotentUnderConcurrency(t *testing.T) {
	w, _ := dialFake(t, Config{Width: 32, Height: 24})

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = w.Close()
		}()
	}
	wg.Wait()

	if !w.closed {
		t.Error("after sixteen concurrent Closes the window does not consider itself closed")
	}
	if w.seg != nil {
		t.Error("the shared segment survived Close")
	}
}
