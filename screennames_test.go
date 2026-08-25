// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import "testing"

// The naming and primary-flag rules are shared by every back-end that
// enumerates displays, so they are tested here rather than under any one of
// them: these two functions decide what an X11, a Wayland and a Win32 caller
// all read out of Screen.Name and Screen.Primary, and the whole point of
// sharing them is that the three cannot drift apart.

// primaryFirst carries two promises of the Screen contract that nothing on
// X11 provides on its own.

// A model names the MODEL, and two identical monitors say the identical thing
// about themselves. Which display is which is then a question only the
// connector can answer.
func TestResolveNames(t *testing.T) {
	for _, tc := range []struct {
		name string
		ids  []displayName
		want []string
	}{
		{"a model that identifies one display",
			[]displayName{{Model: "DELL U2720Q", Connector: "DP-2"}},
			[]string{"DELL U2720Q"}},
		{"two identical monitors",
			[]displayName{
				{Model: "DELL U2720Q", Connector: "DP-1"},
				{Model: "DELL U2720Q", Connector: "DP-2"},
			},
			[]string{"DP-1", "DP-2"}},
		// wlroots publishes the literal "Unknown" for every headless output,
		// which is a placeholder and not a name.
		{"a compositor with one placeholder for everything",
			[]displayName{
				{Model: "Unknown", Connector: "HEADLESS-1", Vendor: "Unknown"},
				{Model: "Unknown", Connector: "HEADLESS-2", Vendor: "Unknown"},
			},
			[]string{"HEADLESS-1", "HEADLESS-2"}},
		{"no model at all",
			[]displayName{{Connector: "HDMI-1"}}, []string{"HDMI-1"}},
		{"nothing but a manufacturer",
			[]displayName{{Vendor: "DELL"}}, []string{"DELL"}},
		// An ambiguous model with no connector to fall back on is still all
		// the display has said.
		{"ambiguous, and nothing to disambiguate with",
			[]displayName{{Model: "Panel"}, {Model: "Panel"}},
			[]string{"Panel", "Panel"}},
		{"a display that says nothing", []displayName{{}}, []string{""}},
		{"no displays", nil, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveNames(tc.ids)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d names, want %d", len(got), len(tc.want))
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("name %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}
