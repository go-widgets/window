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

// TestAPassiveWindowCannotBecomeKey.
//
// The mask is not the whole of passive -- the pointer is turned away with
// -setIgnoresMouseEvents: and the application is made an accessory -- but this
// part is the one that decides whether the keyboard moves, and it is the part
// that can be checked without a display.
//
// It reads the flag off the ACTIVE window because a method on a registered class
// has no other way to reach Go state, and this process runs one window at a time.
// A window that has not been made active yet cannot be asked.
func TestAPassiveWindowCannotBecomeKey(t *testing.T) {
	was := active
	defer func() { active = was }()

	// No window at all: yes, because the alternative is a borderless window that
	// silently takes no keys for the whole of a process that never asked to be
	// passive.
	active = nil
	if !windowCanBecomeKey(0, 0) {
		t.Error("with no active window the answer is no")
	}
	active = &Window{}
	if !windowCanBecomeKey(0, 0) {
		t.Error("an ordinary window cannot become key")
	}
	active = &Window{passive: true}
	if windowCanBecomeKey(0, 0) {
		t.Error("a passive window can become key, so it takes the keyboard from " +
			"whatever the person was typing into")
	}
}

// TestThePassivePolicyIsAccessory: the two activation policies are the two
// numbers AppKit means, and they are not the same number.
//
// Worth pinning because they are untyped constants copied out of a header: a
// regular application shows in the Dock and can become active, an accessory one
// does neither, and swapping them makes a viewer steal the keyboard on launch.
func TestThePassivePolicyIsAccessory(t *testing.T) {
	if activationPolicyReg != 0 {
		t.Errorf("regular is %d, want 0", activationPolicyReg)
	}
	if activationPolicyAccessory != 1 {
		t.Errorf("accessory is %d, want 1", activationPolicyAccessory)
	}
}
