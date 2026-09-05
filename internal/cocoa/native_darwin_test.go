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

// TestAMenuIsRebuiltOnlyWhenItReadsDifferently covers what decides that a
// control's menu is replaced.
//
// Rebuilding on every frame would tear the menu down while it is open: a
// person holding it with the pointer over "Remove" would find it replaced
// under them, which is a way to make somebody click the wrong verb.
func TestAMenuIsRebuiltOnlyWhenItReadsDifferently(t *testing.T) {
	t.Parallel()

	pick := func() {}
	base := []toolkit.NativeMenuItem{
		{Label: "Retry", Pick: pick},
		{},
		{Label: "Remove", Pick: pick},
	}

	for _, tc := range []struct {
		what string
		a, b []toolkit.NativeMenuItem
		want bool
	}{
		{"the same menu", base, base, true},
		// The handlers are closures the application rebuilds every frame, so
		// comparing them would say "changed" for ever.
		{"the same words, fresh closures", base, []toolkit.NativeMenuItem{
			{Label: "Retry", Pick: func() {}},
			{},
			{Label: "Remove", Pick: func() {}},
		}, true},
		{"a word changed", base, []toolkit.NativeMenuItem{
			{Label: "Retry", Pick: pick}, {}, {Label: "Delete", Pick: pick},
		}, false},
		// Whether a verb applies IS part of what the menu reads like.
		{"a verb that stopped applying", base, []toolkit.NativeMenuItem{
			{Label: "Retry", Pick: pick}, {}, {Label: "Remove"},
		}, false},
		{"an item added", base, append(append([]toolkit.NativeMenuItem{}, base...),
			toolkit.NativeMenuItem{Label: "Open"}), false},
		{"both empty", nil, nil, true},
	} {
		if got := sameMenu(tc.a, tc.b); got != tc.want {
			t.Errorf("%s: sameMenu = %v, want %v", tc.what, got, tc.want)
		}
	}
}
