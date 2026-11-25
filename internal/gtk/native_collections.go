// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package gtk

import (
	"strconv"
	"strings"

	"github.com/go-gtk/gtk4"
	"github.com/go-widgets/toolkit"
)

// The pure half of the GTK4 table, tree and tab hosting: how the toolkit's tree
// and its "2.0.1" path strings map to github.com/go-gtk/gtk4's. It has no GTK in
// it, so it is unit-tested on every platform.

// toTreeNodes copies a toolkit tree into gtk4's node type.
func toTreeNodes(tree []toolkit.NativeTreeNode) []gtk4.TreeNode {
	if len(tree) == 0 {
		return nil
	}
	out := make([]gtk4.TreeNode, len(tree))
	for i, n := range tree {
		out[i] = gtk4.TreeNode{Title: n.Title, Expanded: n.Expanded, Children: toTreeNodes(n.Children)}
	}
	return out
}

// treePathString is the "2.0.1" form of a path, the one a tree reports.
func treePathString(path []int) string {
	var b strings.Builder
	for i, p := range path {
		if i > 0 {
			b.WriteByte('.')
		}
		b.WriteString(strconv.Itoa(p))
	}
	return b.String()
}

// parsePath is the path a "2.0.1" string names; nil for "" and for anything that
// is not a path.
func parsePath(s string) []int {
	p, ok := toolkit.ParseNativeTreePath(s)
	if !ok {
		return nil
	}
	return p
}
