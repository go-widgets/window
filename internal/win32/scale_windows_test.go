// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package win32

import "testing"

// A native window dragged to a monitor of another DPI must tell its owner the
// new render scale (the owner passes it to toolkit.SetMetricScale), because the
// tree lays out in the native framebuffer's pixels. A window that does not use
// the monitor's scale (the logical-framebuffer default) has nothing to report.
func TestReportScaleTellsTheOwnerOnlyForANativeWindow(t *testing.T) {
	var got []float64
	record := func(s float64) { got = append(got, s) }

	native := &Window{native: true, scale: 2}
	native.SetOnScale(record)
	native.reportScale()
	if len(got) != 1 || got[0] != 2 {
		t.Fatalf("native window reported %v, want [2]", got)
	}

	got = nil
	soft := &Window{native: false, scale: 2}
	soft.SetOnScale(record)
	soft.reportScale()
	if len(got) != 0 {
		t.Errorf("a logical-framebuffer window reported %v, want nothing", got)
	}

	(&Window{native: true, scale: 2}).reportScale() // no owner: must not panic
}
