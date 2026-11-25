// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package gtk

import (
	"reflect"
	"testing"

	"github.com/go-widgets/toolkit"
)

func TestEncodeRowAndCellAt(t *testing.T) {
	row := encodeRow([]string{"a", "b\x1fc", "d\x00e"}, 4)
	if row != "a\x1fbc\x1fde\x1f" {
		t.Fatalf("encodeRow = %q", row)
	}
	for i, want := range []string{"a", "bc", "de", "", ""} {
		if got := cellAt(row, i); got != want {
			t.Errorf("cellAt(%d) = %q, want %q", i, got, want)
		}
	}
	if got := encodeRow([]string{"x", "y", "z"}, 2); got != "x\x1fy" {
		t.Errorf("extra cells must be cut to the columns: %q", got)
	}
	if got := encodeRow(nil, 0); got != "" {
		t.Errorf("no columns = %q", got)
	}
}

func TestTreeItems(t *testing.T) {
	tree := []toolkit.NativeTreeNode{
		{Title: "A", Expanded: true, Children: []toolkit.NativeTreeNode{{Title: "A0"}, {Title: "A1", Children: []toolkit.NativeTreeNode{{Title: "deep"}}}}},
		{Title: "B"},
	}
	if got, want := childItems(tree, nil), []string{"0\x1fA", "1\x1fB"}; !reflect.DeepEqual(got, want) {
		t.Errorf("top level = %q", got)
	}
	if got, want := childItems(tree, []int{0}), []string{"0.0\x1fA0", "0.1\x1fA1"}; !reflect.DeepEqual(got, want) {
		t.Errorf("children of 0 = %q", got)
	}
	if got := childItems(tree, []int{1}); len(got) != 0 {
		t.Errorf("a leaf has children %q", got)
	}
	if got := childItems(tree, []int{5}); got != nil {
		t.Errorf("a missing node has children %q", got)
	}
	if n := treeNodeAt(tree, []int{0, 1, 0}); n == nil || n.Title != "deep" {
		t.Errorf("treeNodeAt(0.1.0) = %v", n)
	}
	for _, bad := range [][]int{{2}, {0, 2}, {-1}} {
		if treeNodeAt(tree, bad) != nil {
			t.Errorf("treeNodeAt(%v) should be nil", bad)
		}
	}
	path, title := splitTreeItem(treeItem([]int{2, 0, 1}, "x\x1fy"))
	if path != "2.0.1" || title != "xy" {
		t.Errorf("split = %q %q", path, title)
	}
	if p, ti := splitTreeItem("noseparator"); p != "noseparator" || ti != "" {
		t.Errorf("split without a separator = %q %q", p, ti)
	}
	if treePathString(nil) != "" {
		t.Error("empty path must be the empty string")
	}
}
