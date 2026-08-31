// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package cocoa

import (
	"testing"

	"github.com/go-widgets/toolkit"
)

// TestNativeKey covers the identity a control is kept under across frames: a
// caller's explicit Key when set, else the widget's own address (stable in this
// retained-mode toolkit).
func TestNativeKey(t *testing.T) {
	keyed := toolkit.NewNativeButton("b", nil)
	keyed.Key = "save"
	if got := nativeKey(keyed); got != "save" {
		t.Errorf("nativeKey with Key = %q, want save", got)
	}

	anon := toolkit.NewNativeButton("b", nil)
	got := nativeKey(anon)
	if got == "" || got == "save" {
		t.Errorf("nativeKey without Key = %q, want a pointer string", got)
	}
	// Stable for the same widget, distinct for different widgets.
	if nativeKey(anon) != got {
		t.Error("nativeKey not stable for the same widget")
	}
	if nativeKey(toolkit.NewNativeButton("b", nil)) == got {
		t.Error("nativeKey collided for two different widgets")
	}
}
