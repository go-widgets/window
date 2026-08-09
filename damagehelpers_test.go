// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"testing"

	"github.com/go-widgets/toolkit"
)

func r(x, y, w, h int) toolkit.Rect { return toolkit.Rect{X: x, Y: y, W: w, H: h} }

func TestRectContains(t *testing.T) {
	outer := r(0, 0, 100, 100)
	if !rectContains(outer, r(10, 10, 20, 20)) {
		t.Fatal("inner should be contained")
	}
	if rectContains(outer, r(90, 90, 20, 20)) {
		t.Fatal("overhanging inner should not be contained")
	}
	// An empty inner is contained by anything.
	if !rectContains(outer, r(5, 5, 0, 5)) {
		t.Fatal("empty inner is contained")
	}
	// Nothing non-empty is contained by an empty outer.
	if rectContains(r(0, 0, 0, 0), r(1, 1, 2, 2)) {
		t.Fatal("empty outer contains nothing non-empty")
	}
	// Off-origin non-containment on each axis.
	if rectContains(outer, r(-1, 10, 5, 5)) || rectContains(outer, r(10, -1, 5, 5)) {
		t.Fatal("negative-origin inner not contained")
	}
}

func TestAddDamage(t *testing.T) {
	var set []toolkit.Rect
	// Empty rect is dropped.
	set = addDamage(set, r(0, 0, 0, 10))
	if len(set) != 0 {
		t.Fatalf("empty rect must be dropped: %v", set)
	}
	// First real rect.
	set = addDamage(set, r(10, 10, 20, 20))
	// A rect already covered by a member is dropped.
	set = addDamage(set, r(12, 12, 5, 5))
	if len(set) != 1 {
		t.Fatalf("covered rect must be dropped: %v", set)
	}
	// A rect that subsumes an existing member removes it.
	set = addDamage(set, r(0, 0, 40, 40))
	if len(set) != 1 || set[0] != r(0, 0, 40, 40) {
		t.Fatalf("subsuming rect must replace member: %v", set)
	}
	// A disjoint rect is kept distinct.
	set = addDamage(set, r(100, 100, 10, 10))
	if len(set) != 2 {
		t.Fatalf("disjoint rect must be kept: %v", set)
	}
}

func TestClampRect(t *testing.T) {
	// Fully inside: unchanged.
	if got := clampRect(r(10, 10, 20, 20), 100, 100); got != r(10, 10, 20, 20) {
		t.Fatalf("inside clamp = %v", got)
	}
	// Negative origin clamps to 0 and shrinks.
	if got := clampRect(r(-5, -8, 20, 20), 100, 100); got != r(0, 0, 15, 12) {
		t.Fatalf("neg-origin clamp = %v", got)
	}
	// Overhang clamps to the surface edge.
	if got := clampRect(r(90, 95, 30, 30), 100, 100); got != r(90, 95, 10, 5) {
		t.Fatalf("overhang clamp = %v", got)
	}
	// Fully off-surface yields an empty rect.
	if got := clampRect(r(200, 200, 10, 10), 100, 100); got.W > 0 && got.H > 0 {
		t.Fatalf("off-surface clamp should be empty, got %v", got)
	}
}
