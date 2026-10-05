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
