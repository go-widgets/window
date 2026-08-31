// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux && !android

package gtk

import "testing"

// TestRepaintGating pins the Repainter contract the application present loop
// relies on: Repaint raises a flag (from any goroutine), and the frame clock
// consumes it exactly once — so an unchanged window presents nothing until the
// next Repaint. It touches only the flag, so it needs no display and runs on the
// plain linux test lane, not just the Xvfb live one.
func TestRepaintGating(t *testing.T) {
	var w Window
	if w.dirty.Load() {
		t.Fatal("a fresh window should not be dirty")
	}
	w.Repaint()
	if !w.dirty.Load() {
		t.Fatal("Repaint should raise the dirty flag")
	}
	// The frame clock's tick consumes the request once...
	if !w.dirty.Swap(false) {
		t.Fatal("the first tick after Repaint should see dirty and present")
	}
	// ...and finds nothing to do on the next tick, until Repaint is called again.
	if w.dirty.Swap(false) {
		t.Fatal("a tick with no intervening Repaint should present nothing")
	}
}
