// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package win32

// The OS-independent half of the three collection kinds the Win32 host gained
// after the value controls: NativeTable (SysListView32 in virtual mode),
// NativeTree (SysTreeView32) and NativeTabs (SysTabControl32). The glue
// (native_collections_windows.go) only moves bytes into and out of controls; the
// decisions -- when to re-read the data, how a tree flattens into insertions,
// how a cell is looked up and how text is cut to a buffer -- are here, where the
// Linux and macOS lanes hold them to 100% coverage.

import (
	"strconv"
	"unicode/utf16"

	"github.com/go-widgets/toolkit"
)

// minColumnWidth is the narrowest column the table gives a header, in pixels.
const minColumnWidth = 60

// dataChanged reports whether a descriptor's data must be re-read: its revision
// differs from the one the control last took. This is the whole per-frame cost
// of an unchanged 10,000-row table: one integer compare, no walk of the rows.
func dataChanged(last, now uint64) bool { return last != now }

// cellText is the text of one table cell, "" for a row or column the data does
// not have (a short row, or a request past the end). It answers the virtual
// list's LVN_GETDISPINFO, so it must never panic on what the control asks for.
func cellText(rows [][]string, row, col int) string {
	if row < 0 || row >= len(rows) || col < 0 || col >= len(rows[row]) {
		return ""
	}
	return rows[row][col]
}

// columnWidth shares a table's pixel width among its columns, never narrower
// than minColumnWidth (the table then scrolls horizontally).
func columnWidth(total int32, columns int) int32 {
	if columns < 1 {
		return minColumnWidth
	}
	w := total / int32(columns)
	if w < minColumnWidth {
		return minColumnWidth
	}
	return w
}

// copyUTF16 writes s into dst as NUL-terminated UTF-16, cutting it to fit and
// never leaving half a surrogate pair at the cut. It returns the units written
// before the NUL. A control's text buffer (NMLVDISPINFO.pszText) has the size it
// says in cchTextMax; writing past it would corrupt the sender's memory.
func copyUTF16(dst []uint16, s string) int {
	if len(dst) == 0 {
		return 0
	}
	u := utf16.Encode([]rune(s))
	n := len(u)
	if n > len(dst)-1 {
		n = len(dst) - 1
		if n > 0 && utf16.IsSurrogate(rune(u[n-1])) && u[n-1] < 0xDC00 {
			n-- // a high surrogate with no low one after it
		}
	}
	copy(dst, u[:n])
	dst[n] = 0
	return n
}

// treeEntry is one tree node in the order a host inserts them: parents before
// their children. Parent indexes the entry list (-1 for a top-level node), Path
// is the toolkit path ("2.0.1") the selection speaks.
type treeEntry struct {
	Title    string
	Path     string
	Parent   int
	Expanded bool
	Leaf     bool
}

// flattenTree walks nodes pre-order into entries. The toolkit already bounded
// the tree (MaxNativeTreeDepth / MaxNativeTreeNodes); the depth guard here keeps
// the recursion bounded for a descriptor that did not come through it.
func flattenTree(nodes []toolkit.NativeTreeNode) []treeEntry {
	var out []treeEntry
	var walk func(ns []toolkit.NativeTreeNode, parent int, prefix string, depth int)
	walk = func(ns []toolkit.NativeTreeNode, parent int, prefix string, depth int) {
		if depth >= toolkit.MaxNativeTreeDepth {
			return
		}
		for i, n := range ns {
			p := strconv.Itoa(i)
			if prefix != "" {
				p = prefix + "." + p
			}
			out = append(out, treeEntry{Title: n.Title, Path: p, Parent: parent, Expanded: n.Expanded, Leaf: len(n.Children) == 0})
			walk(n.Children, len(out)-1, p, depth+1)
		}
	}
	walk(nodes, -1, "", 0)
	return out
}

// treeIndex maps each path to its entry's index, for pushing a selection.
func treeIndex(entries []treeEntry) map[string]int {
	m := make(map[string]int, len(entries))
	for i, e := range entries {
		m[e.Path] = i
	}
	return m
}

// treeSelection resolves a descriptor's selection path to an entry index: -1
// when the path is empty, malformed or names no node.
func treeSelection(index map[string]int, path string) int {
	if _, ok := toolkit.ParseNativeTreePath(path); !ok {
		return -1
	}
	if i, ok := index[path]; ok {
		return i
	}
	return -1
}

// sameStrings reports whether two string lists are equal element by element.
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
