// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package nativemenu

import (
	"testing"

	"github.com/go-widgets/toolkit"
)

func TestSame(t *testing.T) {
	f := func() {}
	g := func() {}
	cases := []struct {
		name string
		a, b []toolkit.NativeMenuItem
		want bool
	}{
		{"both empty", nil, nil, true},
		{"closures differ, words same", []toolkit.NativeMenuItem{{Label: "Cut", Pick: f}}, []toolkit.NativeMenuItem{{Label: "Cut", Pick: g}}, true},
		{"label differs", []toolkit.NativeMenuItem{{Label: "Cut", Pick: f}}, []toolkit.NativeMenuItem{{Label: "Copy", Pick: f}}, false},
		{"enabled differs", []toolkit.NativeMenuItem{{Label: "Cut", Pick: f}}, []toolkit.NativeMenuItem{{Label: "Cut"}}, false},
		{"length differs", []toolkit.NativeMenuItem{{Label: "Cut"}}, nil, false},
	}
	for _, c := range cases {
		if got := Same(c.a, c.b); got != c.want {
			t.Errorf("%s: Same = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestSeparatorAndEnabled(t *testing.T) {
	f := func() {}
	if !IsSeparator(toolkit.NativeMenuItem{}) || IsSeparator(toolkit.NativeMenuItem{Label: "x"}) {
		t.Error("IsSeparator")
	}
	if Enabled(toolkit.NativeMenuItem{Pick: f}) {
		t.Error("a separator with a Pick must not be enabled")
	}
	if !Enabled(toolkit.NativeMenuItem{Label: "x", Pick: f}) || Enabled(toolkit.NativeMenuItem{Label: "x"}) {
		t.Error("Enabled")
	}
}
