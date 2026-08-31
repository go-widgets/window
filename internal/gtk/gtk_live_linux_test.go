// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux && !android

package gtk

import (
	"testing"

	"github.com/go-widgets/toolkit"
)

// TestGTKBackendReconcilesControls is the on-device proof of the GTK4-hosted
// back-end: it opens a real GTK window, presents a frame, and reconciles a
// self-rendering Surface's native controls into the GtkFixed — a real
// GtkEntry (secure) whose value round-trips, follows an app-side change, and is
// reconciled away when the Surface stops publishing it. Needs a display; under a
// headless runner run it with Xvfb (skips otherwise).
func TestGTKBackendReconcilesControls(t *testing.T) {
	win, err := Open("gtk back-end test", 320, 200, nil, 1)
	if err != nil {
		t.Skipf("no GTK display: %v", err)
	}
	defer win.Close()

	pw := "hunter2"
	surf := toolkit.NewSurface(func() ([]byte, int, int) {
		return make([]byte, 320*200*4), 320, 200
	})
	surf.Controls = func() []toolkit.NativeControl {
		return []toolkit.NativeControl{{
			Kind: toolkit.NativeSecureEntry, Key: "pw",
			Rect: toolkit.Rect{X: 10, Y: 10, W: 200, H: 24}, Visible: true,
			Text:   pw,
			OnText: func(s string) { pw = s },
		}}
	}
	win.root = surf

	// Present + reconcile: the secure entry is created with its value.
	win.frame()
	lc := win.native["pw"]
	if lc == nil {
		t.Fatal("no GTK control created for the pw descriptor")
	}
	if got := lc.widget.Text(); got != "hunter2" {
		t.Errorf("entry text = %q, want hunter2", got)
	}

	// App changes the value; the next frame pushes it (it differs).
	pw = "changed"
	win.frame()
	if got := lc.widget.Text(); got != "changed" {
		t.Errorf("after app change, entry text = %q, want changed", got)
	}

	// The Surface stops publishing it: the control is reconciled away.
	surf.Controls = func() []toolkit.NativeControl { return nil }
	win.frame()
	if _, ok := win.native["pw"]; ok {
		t.Error("control was not reconciled away after the descriptor disappeared")
	}
}
