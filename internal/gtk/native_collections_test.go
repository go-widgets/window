// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package gtk

import (
	"reflect"
	"testing"

	"github.com/go-gtk/gtk4"
	"github.com/go-widgets/toolkit"
)

func TestToTreeNodes(t *testing.T) {
	tree := []toolkit.NativeTreeNode{
		{Title: "A", Expanded: true, Children: []toolkit.NativeTreeNode{{Title: "A0"}, {Title: "A1", Children: []toolkit.NativeTreeNode{{Title: "deep"}}}}},
		{Title: "B"},
	}
	want := []gtk4.TreeNode{
		{Title: "A", Expanded: true, Children: []gtk4.TreeNode{{Title: "A0"}, {Title: "A1", Children: []gtk4.TreeNode{{Title: "deep"}}}}},
		{Title: "B"},
	}
	if got := toTreeNodes(tree); !reflect.DeepEqual(got, want) {
		t.Errorf("toTreeNodes = %+v", got)
	}
	if toTreeNodes(nil) != nil {
		t.Error("an empty tree must stay empty")
	}
}

func TestTreePaths(t *testing.T) {
	if got := treePathString([]int{2, 0, 1}); got != "2.0.1" {
		t.Errorf("treePathString = %q", got)
	}
	if treePathString(nil) != "" {
		t.Error("empty path must be the empty string")
	}
	if got := parsePath("2.0.1"); !reflect.DeepEqual(got, []int{2, 0, 1}) {
		t.Errorf("parsePath = %v", got)
	}
	for _, bad := range []string{"", "x", "1..2", "-1"} {
		if got := parsePath(bad); got != nil {
			t.Errorf("parsePath(%q) = %v, want nil", bad, got)
		}
	}
}
