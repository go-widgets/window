// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package win32

import (
	"reflect"
	"testing"

	"github.com/go-widgets/toolkit"
)

func TestMenuPlan(t *testing.T) {
	f := func() {}
	got := MenuPlan([]toolkit.NativeMenuItem{
		{Label: "Open", Pick: f},
		{},
		{Label: "Remove"},
		{Label: "Save & close", Pick: f},
	})
	want := []MenuSlot{
		{Flags: mfString, ID: 1, Text: "Open", Index: 0},
		{Flags: mfSeparator, Index: 1},
		{Flags: mfString | mfGrayed, ID: 3, Text: "Remove", Index: 2},
		{Flags: mfString, ID: 4, Text: "Save && close", Index: 3},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("MenuPlan =\n %+v\nwant\n %+v", got, want)
	}
	if len(MenuPlan(nil)) != 0 {
		t.Error("an empty menu must plan nothing")
	}
}

func TestPickIndex(t *testing.T) {
	for _, c := range []struct {
		cmd  uintptr
		n    int
		want int
		ok   bool
	}{
		{0, 3, 0, false}, // dismissed
		{1, 3, 0, true},
		{3, 3, 2, true},
		{4, 3, 0, false}, // not ours
	} {
		if got, ok := PickIndex(c.cmd, c.n); got != c.want || ok != c.ok {
			t.Errorf("PickIndex(%d,%d) = %d,%v want %d,%v", c.cmd, c.n, got, ok, c.want, c.ok)
		}
	}
}

func TestMenuPoint(t *testing.T) {
	x, y, kb := MenuPoint(uintptr(uint32(100) | uint32(200)<<16))
	if x != 100 || y != 200 || kb {
		t.Errorf("got %d,%d,%v", x, y, kb)
	}
	// Screen coordinates are signed: a second monitor to the left is negative.
	x, y, _ = MenuPoint(uintptr(uint16(0xFF9C)) | uintptr(uint16(50))<<16)
	if x != -100 || y != 50 {
		t.Errorf("negative x: got %d,%d", x, y)
	}
	// The keyboard sends -1,-1 (all bits set in each word).
	if _, _, kb := MenuPoint(0xFFFFFFFF); !kb {
		t.Error("lParam -1 must read as the keyboard")
	}
	if _, _, kb := MenuPoint(^uintptr(0)); !kb {
		t.Error("a sign-extended -1 must read as the keyboard")
	}
}
