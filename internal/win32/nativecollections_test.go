// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package win32

import (
	"fmt"
	"reflect"
	"testing"
	"unicode/utf16"

	"github.com/go-widgets/toolkit"
)

func TestDataChanged(t *testing.T) {
	if dataChanged(7, 7) {
		t.Error("equal revisions reported as changed")
	}
	if !dataChanged(7, 8) || !dataChanged(8, 7) {
		t.Error("different revisions not reported as changed")
	}
}

func TestCellText(t *testing.T) {
	rows := [][]string{{"a", "b"}, {"c"}}
	for _, c := range []struct {
		r, c int
		want string
	}{{0, 0, "a"}, {0, 1, "b"}, {1, 0, "c"}, {1, 1, ""}, {2, 0, ""}, {-1, 0, ""}, {0, -1, ""}} {
		if got := cellText(rows, c.r, c.c); got != c.want {
			t.Errorf("cellText(%d,%d) = %q, want %q", c.r, c.c, got, c.want)
		}
	}
	if cellText(nil, 0, 0) != "" {
		t.Error("nil rows must give an empty cell")
	}
}

func TestColumnWidth(t *testing.T) {
	for _, c := range []struct {
		total int32
		n     int
		want  int32
	}{{600, 3, 200}, {100, 4, minColumnWidth}, {500, 0, minColumnWidth}, {0, 2, minColumnWidth}} {
		if got := columnWidth(c.total, c.n); got != c.want {
			t.Errorf("columnWidth(%d,%d) = %d, want %d", c.total, c.n, got, c.want)
		}
	}
}

func TestCopyUTF16(t *testing.T) {
	dst := make([]uint16, 8)
	if n := copyUTF16(dst, "héllo"); n != 5 || string(utf16.Decode(dst[:n])) != "héllo" || dst[n] != 0 {
		t.Errorf("copy = %d %v", n, dst)
	}
	// Cut to the buffer: 3 units + NUL, so "abcdef" becomes "abc".
	small := make([]uint16, 4)
	if n := copyUTF16(small, "abcdef"); n != 3 || small[3] != 0 {
		t.Errorf("cut = %d %v", n, small)
	}
	// A surrogate pair is never split: the emoji needs 2 units, only 1 fits
	// after "a", so the cut drops it whole.
	pair := make([]uint16, 3)
	if n := copyUTF16(pair, "a😀"); n != 1 || pair[1] != 0 {
		t.Errorf("pair cut = %d %v", n, pair)
	}
	whole := make([]uint16, 4)
	if n := copyUTF16(whole, "a😀"); n != 3 || string(utf16.Decode(whole[:n])) != "a😀" {
		t.Errorf("whole pair = %d %v", n, whole)
	}
	if n := copyUTF16(nil, "x"); n != 0 {
		t.Errorf("empty buffer wrote %d", n)
	}
	one := []uint16{9}
	if n := copyUTF16(one, "x"); n != 0 || one[0] != 0 {
		t.Errorf("one-unit buffer = %d %v", n, one)
	}
}

func sampleTree() []toolkit.NativeTreeNode {
	return []toolkit.NativeTreeNode{
		{Title: "a", Expanded: true, Children: []toolkit.NativeTreeNode{{Title: "a0"}, {Title: "a1", Children: []toolkit.NativeTreeNode{{Title: "a1x"}}}}},
		{Title: "b"},
	}
}

func TestFlattenTree(t *testing.T) {
	got := flattenTree(sampleTree())
	want := []treeEntry{
		{Title: "a", Path: "0", Parent: -1, Expanded: true},
		{Title: "a0", Path: "0.0", Parent: 0, Leaf: true},
		{Title: "a1", Path: "0.1", Parent: 0},
		{Title: "a1x", Path: "0.1.0", Parent: 2, Leaf: true},
		{Title: "b", Path: "1", Parent: -1, Leaf: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("flattenTree = %+v\nwant %+v", got, want)
	}
	// Parents always come before their children, so a host can insert in order.
	for i, e := range got {
		if e.Parent >= i {
			t.Errorf("entry %d has parent %d after it", i, e.Parent)
		}
	}
	if flattenTree(nil) != nil {
		t.Error("no nodes must give no entries")
	}
}

func TestFlattenTreeDepthBound(t *testing.T) {
	n := toolkit.NativeTreeNode{Title: "leaf"}
	for i := 0; i < toolkit.MaxNativeTreeDepth+10; i++ {
		n = toolkit.NativeTreeNode{Title: "n", Children: []toolkit.NativeTreeNode{n}}
	}
	if got := len(flattenTree([]toolkit.NativeTreeNode{n})); got != toolkit.MaxNativeTreeDepth {
		t.Errorf("entries = %d, want the depth bound %d", got, toolkit.MaxNativeTreeDepth)
	}
}

func TestTreeSelection(t *testing.T) {
	idx := treeIndex(flattenTree(sampleTree()))
	for path, want := range map[string]int{"0": 0, "0.1": 2, "0.1.0": 3, "1": 4, "": -1, "7": -1, "x": -1, "0.": -1, "-1": -1} {
		if got := treeSelection(idx, path); got != want {
			t.Errorf("treeSelection(%q) = %d, want %d", path, got, want)
		}
	}
}

func TestSameStrings(t *testing.T) {
	if !sameStrings(nil, nil) || !sameStrings([]string{"a"}, []string{"a"}) {
		t.Error("equal lists reported different")
	}
	if sameStrings([]string{"a"}, []string{"b"}) || sameStrings([]string{"a"}, nil) {
		t.Error("different lists reported equal")
	}
}

// BenchmarkRevisionUnchanged is the per-frame price of reconciling a 10,000-row
// table whose Revision did not move: the descriptor's rows are never walked.
// BenchmarkRowWalk is what a host without a revision would pay (comparing every
// cell), for scale.
func BenchmarkRevisionUnchanged(b *testing.B) {
	b.ReportAllocs()
	var changed bool
	for i := 0; i < b.N; i++ {
		changed = dataChanged(41, 41)
	}
	_ = changed
}

func BenchmarkRowWalk(b *testing.B) {
	rows := make([][]string, 10000)
	for i := range rows {
		rows[i] = []string{fmt.Sprint(i), "name", "value"}
	}
	other := make([][]string, len(rows))
	copy(other, rows)
	b.ReportAllocs()
	b.ResetTimer()
	same := true
	for i := 0; i < b.N; i++ {
		for r := range rows {
			if !sameStrings(rows[r], other[r]) {
				same = false
			}
		}
	}
	_ = same
}
