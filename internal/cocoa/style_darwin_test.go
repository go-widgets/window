// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package cocoa

import "testing"

// TestFramedStyle: a fixed-size window is one without the resizable mask, and
// nothing else changes.
//
// The mask is the whole of the behaviour -- AppKit draws no resize control,
// refuses a drag on an edge and disables the zoom button from this bit alone --
// so this is the assertion, and it needs no display.
func TestFramedStyle(t *testing.T) {
	loose := framedStyle(false)
	fixed := framedStyle(true)

	if loose&styleResizable == 0 {
		t.Error("an ordinary window is not resizable")
	}
	if fixed&styleResizable != 0 {
		t.Error("a fixed-size window kept the resizable mask")
	}
	// Everything else is the same window: a caller asking for a fixed size did
	// not ask to lose the title bar or the close button.
	if loose&^styleResizable != fixed {
		t.Errorf("the two masks differ by more than resizable: %b vs %b", loose, fixed)
	}
	for name, bit := range map[string]uint{
		"titled":         styleTitled,
		"closable":       styleClosable,
		"miniaturizable": styleMiniaturizable,
	} {
		if fixed&bit == 0 {
			t.Errorf("a fixed-size window is not %s", name)
		}
	}
}
