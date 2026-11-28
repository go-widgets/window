// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package cocoa

import "testing"

func TestPlanRaise(t *testing.T) {
	for _, tc := range []struct {
		mini, passive bool
		want          RaiseSteps
	}{
		{false, false, RaiseSteps{Activate: true, MakeKey: true}},
		{true, false, RaiseSteps{Deminiaturize: true, Activate: true, MakeKey: true}},
		// A passive window is shown and ordered front, and never handed the
		// keyboard: that is what it promised.
		{false, true, RaiseSteps{}},
		{true, true, RaiseSteps{Deminiaturize: true}},
	} {
		if got := PlanRaise(tc.mini, tc.passive); got != tc.want {
			t.Errorf("PlanRaise(miniaturized=%v, passive=%v) = %+v, want %+v", tc.mini, tc.passive, got, tc.want)
		}
	}
}

func TestPlanReopen(t *testing.T) {
	for _, tc := range []struct {
		visible, hide, closed, child, want bool
	}{
		{false, true, false, false, true},   // hidden by HideOnClose: shown again
		{true, true, false, false, false},   // a window is visible: nothing to do
		{false, false, false, false, false}, // no HideOnClose: AppKit's default stands
		{false, true, true, false, false},   // closed: nothing to show
		{false, true, false, true, false},   // a secondary window is not the app's window
	} {
		if got := PlanReopen(tc.visible, tc.hide, tc.closed, tc.child); got != tc.want {
			t.Errorf("PlanReopen(%+v) = %v, want %v", tc, got, tc.want)
		}
	}
}
