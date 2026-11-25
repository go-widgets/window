// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package gtk

import (
	"strconv"
	"strings"

	"github.com/go-widgets/toolkit"
)

// The pure half of the GTK4 table, tree and tab hosting: how a row or a tree node
// is flattened into the one string a GtkStringObject holds, and read back. It has
// no GTK in it, so it is unit-tested on every platform.
//
// A GtkStringList is the cheapest GListModel that can hold 100 000 items (one
// small GObject each, built by a single splice), and the GtkColumnView binds only
// the rows on screen, so the items carry a row's cells as one string and a column
// reads its own cell out at bind time.

// cellSep separates the cells of one row inside an item string. It is the ASCII
// unit separator, which a cell cannot contain: encodeRow drops it (and NUL, which
// would end the C string early) from every cell.
const cellSep = "\x1f"

// cleanCell removes the two bytes an item string cannot carry.
func cleanCell(s string) string {
	if !strings.ContainsAny(s, "\x00\x1f") {
		return s
	}
	return strings.Map(func(r rune) rune {
		if r == 0 || r == 0x1f {
			return -1
		}
		return r
	}, s)
}

// encodeRow flattens the first ncols cells of a row into one item string; a short
// row is padded with empty cells so every column finds its index.
func encodeRow(cells []string, ncols int) string {
	var b strings.Builder
	for i := 0; i < ncols; i++ {
		if i > 0 {
			b.WriteString(cellSep)
		}
		if i < len(cells) {
			b.WriteString(cleanCell(cells[i]))
		}
	}
	return b.String()
}

// cellAt reads cell idx of an encoded row, "" when the row has fewer cells.
func cellAt(row string, idx int) string {
	for i := 0; i < idx; i++ {
		j := strings.Index(row, cellSep)
		if j < 0 {
			return ""
		}
		row = row[j+len(cellSep):]
	}
	if j := strings.Index(row, cellSep); j >= 0 {
		return row[:j]
	}
	return row
}

// treeItem encodes one tree node as "path SEP title".
func treeItem(path []int, title string) string {
	return treePathString(path) + cellSep + cleanCell(title)
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

// splitTreeItem is treeItem's inverse.
func splitTreeItem(item string) (path, title string) {
	if j := strings.Index(item, cellSep); j >= 0 {
		return item[:j], item[j+len(cellSep):]
	}
	return item, ""
}

// treeNodeAt returns the node a path names, or nil when it names nothing.
func treeNodeAt(tree []toolkit.NativeTreeNode, path []int) *toolkit.NativeTreeNode {
	var n *toolkit.NativeTreeNode
	level := tree
	for _, i := range path {
		if i < 0 || i >= len(level) {
			return nil
		}
		n = &level[i]
		level = n.Children
	}
	return n
}

// childItems encodes the children of the node at path (nil path: the top level).
func childItems(tree []toolkit.NativeTreeNode, path []int) []string {
	level := tree
	if len(path) > 0 {
		n := treeNodeAt(tree, path)
		if n == nil {
			return nil
		}
		level = n.Children
	}
	out := make([]string, len(level))
	for i, c := range level {
		p := make([]int, len(path)+1)
		copy(p, path)
		p[len(path)] = i
		out[i] = treeItem(p, c.Title)
	}
	return out
}
