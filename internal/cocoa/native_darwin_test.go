// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package cocoa

import (
	"testing"

	"github.com/go-widgets/toolkit"
)

// fakeProvider is a root that supplies native controls directly, like a Surface.
type fakeProvider struct {
	toolkit.Base
	controls []toolkit.NativeControl
}

func (f *fakeProvider) NativeControls() []toolkit.NativeControl { return f.controls }

// fakeTree is a plain widget-tree root (no provider), walked by WalkNative.
type fakeTree struct {
	toolkit.Base
	kids []toolkit.Widget
}

func (f *fakeTree) Children() []toolkit.Widget { return f.kids }

func TestGatherNativeFromProvider(t *testing.T) {
	p := &fakeProvider{controls: []toolkit.NativeControl{
		{Key: "a", Kind: toolkit.NativeButton},
		{Key: "b", Kind: toolkit.NativeEntry},
	}}
	got := gatherNative(p)
	if len(got) != 2 || got[0].Key != "a" || got[1].Key != "b" {
		t.Fatalf("gatherNative(provider) = %+v, want the provider's two controls", got)
	}
}

func TestGatherNativeFromWalk(t *testing.T) {
	n := toolkit.NewNativeButton("b", nil)
	n.Key = "k"
	n.SetBounds(toolkit.Rect{W: 10, H: 10})
	got := gatherNative(&fakeTree{kids: []toolkit.Widget{n}})
	if len(got) != 1 || got[0].Key != "k" || got[0].Kind != toolkit.NativeButton {
		t.Fatalf("gatherNative(tree) = %+v, want one button keyed k", got)
	}
}

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
