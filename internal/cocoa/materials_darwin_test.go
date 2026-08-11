// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package cocoa

import (
	"testing"

	"github.com/go-widgets/toolkit"
)

func TestMaterialConstantMapping(t *testing.T) {
	cases := []struct {
		kind toolkit.MaterialKind
		want int
	}{
		{toolkit.MaterialWindowBackground, nsvfxWindowBackground},
		{toolkit.MaterialSidebar, nsvfxSidebar},
		{toolkit.MaterialTitlebar, nsvfxTitlebar},
		{toolkit.MaterialMenu, nsvfxMenu},
		{toolkit.MaterialPopover, nsvfxPopover},
		{toolkit.MaterialHUD, nsvfxHUDWindow},
		{toolkit.MaterialSelection, nsvfxSelection},
		{toolkit.MaterialKind(999), nsvfxSelection}, // default
	}
	for _, c := range cases {
		if got := materialConstant(c.kind); got != c.want {
			t.Errorf("materialConstant(%d) = %d, want %d", c.kind, got, c.want)
		}
	}
}

func TestBlendingConstantMapping(t *testing.T) {
	if got := blendingConstant(toolkit.BlendBehindWindow); got != nsvfxBehindWindow {
		t.Errorf("behind = %d, want %d", got, nsvfxBehindWindow)
	}
	if got := blendingConstant(toolkit.BlendWithinWindow); got != nsvfxWithinWindow {
		t.Errorf("within = %d, want %d", got, nsvfxWithinWindow)
	}
}

func TestEffectFrameFlipsAndScales(t *testing.T) {
	// A 200x100-render-px rect at (40,30), scale 2 (so 20x50 pts at 10,15),
	// inside a 160pt-tall container: the flipped bottom-left Y is
	// 160 - (15 + 50) = 95.
	r := toolkit.Rect{X: 40, Y: 30, W: 200, H: 100}
	f := effectFrame(r, 2, 160)
	if f.Origin.X != 20 || f.Size.W != 100 || f.Size.H != 50 {
		t.Errorf("scale wrong: %+v", f)
	}
	if f.Origin.Y != 160-(15+50) {
		t.Errorf("flip wrong: Y=%v, want %v", f.Origin.Y, 160-(15+50))
	}
}

func TestClampHelper(t *testing.T) {
	if clamp(-5, 0, 10) != 0 {
		t.Error("clamp low")
	}
	if clamp(15, 0, 10) != 10 {
		t.Error("clamp high")
	}
	if clamp(7, 0, 10) != 7 {
		t.Error("clamp mid")
	}
}
