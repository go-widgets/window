// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"testing"

	"github.com/go-widgets/toolkit"
)

// A window that asked for the panel's own resolution must tell the toolkit what
// a point is now worth, and one that did not must leave it alone.
//
// The second half is the one worth a test. This is a package-level setting: an
// application that never mentioned DPI, linked into the same binary or opening a
// window after one that did, must not find its metrics silently doubled.
func TestApplyMetricScale(t *testing.T) {
	defer toolkit.SetMetricScale(1)

	for _, tc := range []struct {
		name              string
		requested, actual float64
		start, want       float64
	}{
		{"asked and got two", NativeScale, 2, 1, 2},
		{"asked and got one", NativeScale, 1, 1, 1},
		{"asked on a screen that says three", NativeScale, 3, 1, 3},
		{"never asked", 0, 2, 1, 1},
		{"never asked, and a previous window had", 0, 2, 2, 2},
		{"asked for a fixed scale of its own", 1.5, 2, 1, 1},
	} {
		toolkit.SetMetricScale(tc.start)
		applyMetricScale(tc.requested, tc.actual)
		if got := toolkit.MetricScale(); got != tc.want {
			t.Errorf("%s: metric scale = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// The X11 back-end resolves its scale at bring-up, so opening a NativeScale
// window on a 192-dpi desktop leaves the toolkit knowing it.
func TestX11NativeScaleTellsTheToolkit(t *testing.T) {
	defer toolkit.SetMetricScale(1)

	toolkit.SetMetricScale(1)
	dialFakeWithResources(t, Config{Width: 100, Height: 80}, "Xft.dpi:\t192\n")
	if got := toolkit.MetricScale(); got != 1 {
		t.Errorf("a window that did not ask left the toolkit at %v, want 1", got)
	}

	dialFakeWithResources(t, Config{Width: 100, Height: 80, RenderScale: NativeScale}, "Xft.dpi:\t192\n")
	if got := toolkit.MetricScale(); got != 2 {
		t.Errorf("after a NativeScale window on a 192 dpi desktop the toolkit is at %v, want 2 -- "+
			"its widgets will lay out at half the size the window is drawing at", got)
	}
}
