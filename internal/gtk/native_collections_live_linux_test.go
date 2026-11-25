// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux && !android && (amd64 || arm64)

package gtk

import (
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/go-widgets/toolkit"
)

func collSurface(w, h int, controls func() []toolkit.NativeControl) *toolkit.Surface {
	s := toolkit.NewSurface(func() ([]byte, int, int) { return make([]byte, w*h*4), w, h })
	s.Controls = controls
	return s
}

func manyRows(n int) [][]string {
	rows := make([][]string, n)
	for i := range rows {
		rows[i] = []string{"row " + strconv.Itoa(i), strconv.Itoa(i * 2), "x"}
	}
	return rows
}

// TestGTKBackendTable proves a NativeTable hosts as a GtkColumnView over a
// 10 000-row model, round-trips its selection both ways, and re-reads the rows
// only when the Revision moves.
func TestGTKBackendTable(t *testing.T) {
	win, err := Open("gtk table test", 480, 320, nil, 1)
	if err != nil {
		t.Skipf("no GTK display: %v", err)
	}
	defer win.Close()

	rows, rev, num := manyRows(10000), uint64(1), 5.0
	var reported []float64
	surf := collSurface(480, 320, func() []toolkit.NativeControl {
		return []toolkit.NativeControl{{
			Kind: toolkit.NativeTable, Key: "tbl", Visible: true,
			Rect:    toolkit.Rect{X: 10, Y: 10, W: 400, H: 240},
			Columns: []string{"Name", "Double", "Tag"}, Rows: rows, Revision: rev, Number: num,
			OnNumber: func(v float64) { reported = append(reported, v); num = v },
		}}
	})
	win.root = surf
	win.frame()
	lc := win.native["tbl"]
	if lc == nil || lc.coll == nil {
		t.Fatal("no table control created")
	}
	c := lc.coll
	if got := c.table.Class(); got != "GtkColumnView" {
		t.Errorf("view class = %q, want GtkColumnView", got)
	}
	if got := c.table.Len(); got != 10000 {
		t.Errorf("model holds %d rows, want 10000", got)
	}
	if got := c.table.Selected(); got != 5 {
		t.Errorf("selected = %d, want 5", got)
	}
	if len(reported) != 0 {
		t.Errorf("the app's own selection was reported back: %v", reported)
	}

	// The person picks row 7: reported through OnNumber.
	c.table.Select(7)
	if len(reported) != 1 || reported[0] != 7 {
		t.Errorf("after the person selected 7, reported = %v", reported)
	}
	// The app moves the selection: pushed into the widget, not echoed.
	num = 9
	win.frame()
	if got := c.table.Selected(); got != 9 {
		t.Errorf("after the app chose 9, selected = %d", got)
	}
	if len(reported) != 1 {
		t.Errorf("the app's move was echoed: %v", reported)
	}
	num = -1
	win.frame()
	if got := c.table.Selected(); got != -1 {
		t.Errorf("after the app cleared it, selected = %d", got)
	}

	// Revision unchanged: nothing is rebuilt, and the reconcile is ~free.
	before := c.rebuilds
	const loops = 2000
	t0 := time.Now()
	for i := 0; i < loops; i++ {
		win.syncNative(surf)
	}
	same := time.Since(t0) / loops
	if c.rebuilds != before {
		t.Errorf("an unchanged Revision rebuilt the model %d times", c.rebuilds-before)
	}

	// Revision moved: one re-read.
	rows, rev = manyRows(9000), rev+1
	t0 = time.Now()
	win.syncNative(surf)
	change := time.Since(t0)
	if c.rebuilds != before+1 {
		t.Errorf("a moved Revision rebuilt %d times, want 1", c.rebuilds-before)
	}
	if got := c.table.Len(); got != 9000 {
		t.Errorf("after the change the model holds %d rows, want 9000", got)
	}
	t.Logf("10k-row table: unchanged Revision reconcile = %v/op; 9000-row replace = %v", same, change)
}

// TestGTKBackendTree proves a NativeTree hosts as a GtkListView over a
// GtkTreeListModel: Expanded nodes start open, the selected path round-trips.
func TestGTKBackendTree(t *testing.T) {
	win, err := Open("gtk tree test", 480, 320, nil, 1)
	if err != nil {
		t.Skipf("no GTK display: %v", err)
	}
	defer win.Close()

	tree := []toolkit.NativeTreeNode{
		{Title: "A", Expanded: true, Children: []toolkit.NativeTreeNode{
			{Title: "A0"},
			{Title: "A1", Children: []toolkit.NativeTreeNode{{Title: "deep"}}},
		}},
		{Title: "B"},
	}
	sel, rev := "0.1", uint64(1)
	var reported []string
	surf := collSurface(480, 320, func() []toolkit.NativeControl {
		return []toolkit.NativeControl{{
			Kind: toolkit.NativeTree, Key: "tree", Visible: true,
			Rect: toolkit.Rect{X: 10, Y: 10, W: 300, H: 240},
			Tree: tree, Revision: rev, Text: sel,
			OnText: func(p string) { reported = append(reported, p); sel = p },
		}}
	})
	win.root = surf
	win.frame()
	lc := win.native["tree"]
	if lc == nil || lc.coll == nil {
		t.Fatal("no tree control created")
	}
	c := lc.coll
	if got := c.tree.Class(); got != "GtkListView" {
		t.Errorf("view class = %q, want GtkListView", got)
	}
	// A is expanded, A1 is not; A1 is the selected node.
	if got := c.tree.SelectedPath(); !reflect.DeepEqual(got, []int{0, 1}) {
		t.Errorf("selected path = %v, want [0 1]", got)
	}
	if len(reported) != 0 {
		t.Errorf("the app's own selection was reported back: %v", reported)
	}

	// The person picks B (position 3): its path is reported.
	c.tree.SelectPath([]int{1})
	if len(reported) != 1 || reported[0] != "1" {
		t.Errorf("after the person selected B, reported = %v", reported)
	}
	// The app selects a node under a collapsed parent: it opens to show it, and
	// the app's own move is not reported back.
	sel = "0.1.0"
	win.frame()
	if got := c.tree.SelectedPath(); !reflect.DeepEqual(got, []int{0, 1, 0}) || len(reported) != 1 {
		t.Errorf("after the app chose 0.1.0: selected %v, reported %v", got, reported)
	}
	// New data under a new Revision is re-read.
	before := c.rebuilds
	tree, rev, sel = []toolkit.NativeTreeNode{{Title: "only"}}, rev+1, ""
	win.frame()
	if c.rebuilds != before+1 || c.tree.SelectedPath() != nil {
		t.Errorf("rebuilds +%d, selected %v; want +1 and none", c.rebuilds-before, c.tree.SelectedPath())
	}
}

// TestGTKBackendTabs proves a NativeTabs hosts as a GtkNotebook strip whose
// current page round-trips through Number.
func TestGTKBackendTabs(t *testing.T) {
	win, err := Open("gtk tabs test", 480, 320, nil, 1)
	if err != nil {
		t.Skipf("no GTK display: %v", err)
	}
	defer win.Close()

	items, rev, num := []string{"One", "Two", "Three"}, uint64(1), 1.0
	var reported []float64
	surf := collSurface(480, 320, func() []toolkit.NativeControl {
		return []toolkit.NativeControl{{
			Kind: toolkit.NativeTabs, Key: "tabs", Visible: true,
			Rect:  toolkit.Rect{X: 10, Y: 10, W: 300, H: 40},
			Items: items, Revision: rev, Number: num,
			OnNumber: func(v float64) { reported = append(reported, v); num = v },
		}}
	})
	win.root = surf
	win.frame()
	lc := win.native["tabs"]
	if lc == nil || lc.coll == nil {
		t.Fatal("no tabs control created")
	}
	c := lc.coll
	if got := c.tabs.Class(); got != "GtkNotebook" {
		t.Errorf("view class = %q, want GtkNotebook", got)
	}
	if got := c.tabs.Len(); got != 3 {
		t.Errorf("pages = %d, want 3", got)
	}
	if got := c.tabs.Current(); got != 1 {
		t.Errorf("current = %d, want 1", got)
	}
	if len(reported) != 0 {
		t.Errorf("the app's own selection was reported back: %v", reported)
	}
	c.tabs.Switch(2) // the person clicks the third tab
	if len(reported) != 1 || reported[0] != 2 {
		t.Errorf("after the person picked tab 2, reported = %v", reported)
	}
	num = 0
	win.frame()
	if got := c.tabs.Current(); got != 0 || len(reported) != 1 {
		t.Errorf("after the app chose 0: current %d, reported %v", got, reported)
	}
	items, rev = []string{"A", "B"}, rev+1
	win.frame()
	if got := c.tabs.Len(); got != 2 {
		t.Errorf("after the titles changed, pages = %d, want 2", got)
	}
}
