// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package cocoa

import "testing"

// TestVisibleScreenSize exercises the on-device AppKit query directly (the
// window package's screen_darwin.go is a one-line forwarder to this). It loads
// the frameworks and reads NSScreen.mainScreen.visibleFrame; the assertion is
// the same invariant the public API promises so it passes both on a headed
// runner (a real, strictly-positive size) and on a headless build (ok=false,
// zero size).
func TestVisibleScreenSize(t *testing.T) {
	w, h, ok := VisibleScreenSize()
	if !ok {
		if w != 0 || h != 0 {
			t.Fatalf("VisibleScreenSize unavailable but returned %dx%d, want 0x0", w, h)
		}
		return
	}
	if w <= 0 || h <= 0 {
		t.Fatalf("VisibleScreenSize ok but returned non-positive size %dx%d", w, h)
	}
}
