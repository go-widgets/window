// Copyright (c) 2026, the go-widgets authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file.

package cocoa

import "testing"

// TestSameStringsDecidesWhenAListIsReloaded covers the comparison that keeps a
// native list usable.
//
// Reloading an NSTableView throws its selection away. Doing that on every
// frame — which is what "push the rows unconditionally" means — would clear the
// chosen row sixty times a second, so the rows are only pushed when they have
// actually changed.
func TestSameStringsDecidesWhenAListIsReloaded(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		what string
		a, b []string
		want bool
	}{
		{"the same rows", []string{"un", "deux"}, []string{"un", "deux"}, true},
		{"both empty", nil, nil, true},
		{"empty against nil", []string{}, nil, true},
		{"one row changed", []string{"un", "deux"}, []string{"un", "DEUX"}, false},
		{"a row added", []string{"un"}, []string{"un", "deux"}, false},
		{"a row removed", []string{"un", "deux"}, []string{"un"}, false},
		{"the same rows reordered", []string{"un", "deux"}, []string{"deux", "un"}, false},
	} {
		if got := sameStrings(tc.a, tc.b); got != tc.want {
			t.Errorf("%s: sameStrings = %v, want %v", tc.what, got, tc.want)
		}
	}
}
