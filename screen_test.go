// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import "testing"

// TestVisibleScreenSize asserts the platform-independent contract, so it holds
// whichever implementation is compiled in: on macOS it exercises the real
// Cocoa query (screen_darwin.go), on every other platform the ok=false stub
// (screen_other.go). Either way the invariant is the same — a reported size is
// strictly positive, and an unavailable size is exactly (0, 0) — which is all a
// caller may rely on.
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
