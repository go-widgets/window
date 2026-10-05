// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package win32

import "testing"

func TestVisibilityPlan(t *testing.T) {
	for _, tc := range []struct {
		name       string
		op         VisibilityOp
		iconic     bool
		cmd        int32
		foreground bool
		ok         bool
	}{
		{"show", OpShow, false, SWShowNA, false, true},
		{"show while minimised stays a show", OpShow, true, SWShowNA, false, true},
		{"hide", OpHide, false, SWHide, false, true},
		{"raise", OpRaise, false, SWShow, true, true},
		// SW_SHOW leaves a minimised window minimised: Raise has to restore it.
		{"raise a minimised window", OpRaise, true, SWRestore, true, true},
		{"not one of ours", VisibilityOp(0), false, 0, false, false},
		{"not one of ours either", OpRaise + 1, false, 0, false, false},
	} {
		cmd, fg, ok := VisibilityPlan(tc.op, tc.iconic)
		if cmd != tc.cmd || fg != tc.foreground || ok != tc.ok {
			t.Errorf("%s: VisibilityPlan = (%d, %v, %v), want (%d, %v, %v)", tc.name, cmd, fg, ok, tc.cmd, tc.foreground, tc.ok)
		}
	}
	if WMAppVisibility == WMAppRepaint || WMAppVisibility < 0x8000 || WMAppVisibility > 0xBFFF {
		t.Errorf("WMAppVisibility = %#x, want a WM_APP message other than the repaint one", WMAppVisibility)
	}
}
