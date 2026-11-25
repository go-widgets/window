// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux && !android

package gtk

import (
	"github.com/go-gtk/gtk4"
	"github.com/go-widgets/toolkit"
)

// GTK4 peers of the three collection kinds, hosted through github.com/go-gtk/gtk4
// (which owns the binding, and is a stub off linux/amd64 and linux/arm64: its
// constructors return nil there, so these kinds are simply not hosted and the
// painted fallback stays):
//
//   - NativeTable: a gtk4.ColumnView (GtkColumnView over a GtkStringList). The rows
//     are replaced only when the descriptor's Revision moves.
//   - NativeTree: a gtk4.TreeView (GtkListView over a GtkTreeListModel).
//   - NativeTabs: a gtk4.Notebook whose pages are empty: the strip only, the
//     toolkit lays the content out below it.

// collection is the GTK side of one hosted table, tree or tab strip.
type collection struct {
	lc   *liveControl
	kind toolkit.NativeKind

	table *gtk4.ColumnView
	tree  *gtk4.TreeView
	tabs  *gtk4.Notebook

	rev      uint64
	rebuilds int // how many times the data was rebuilt (tests read it)
}

// newCollection builds the GTK widget for a NativeTable, NativeTree or NativeTabs
// descriptor and fills it. It returns nil when GTK4 cannot be bound.
func newCollection(lc *liveControl, spec toolkit.NativeControl) *collection {
	c := &collection{lc: lc, kind: spec.Kind}
	switch spec.Kind {
	case toolkit.NativeTable:
		if c.table = gtk4.ColumnViewNew(spec.Columns); c.table == nil {
			return nil
		}
		c.table.OnSelect(c.rowSelected)
		c.setRows(spec.Rows)
		if i := int(spec.Number); i >= 0 {
			c.table.SetSelected(i)
		}
	case toolkit.NativeTree:
		if c.tree = gtk4.TreeViewNew(); c.tree == nil {
			return nil
		}
		c.tree.OnSelect(c.pathSelected)
		c.setTree(spec.Tree)
		c.tree.SetSelectedPath(parsePath(spec.Text))
	case toolkit.NativeTabs:
		if c.tabs = gtk4.NotebookNew(); c.tabs == nil {
			return nil
		}
		c.tabs.OnSwitch(c.pageSwitched)
		c.setTabs(spec.Items)
		c.tabs.SetCurrent(int(spec.Number))
	default:
		return nil
	}
	c.rev = spec.Revision
	return c
}

func (c *collection) widget() gtk4.Widget {
	switch c.kind {
	case toolkit.NativeTable:
		return c.table.Widget()
	case toolkit.NativeTree:
		return c.tree.Widget()
	}
	return c.tabs.Widget()
}

func (c *collection) release() {
	switch c.kind {
	case toolkit.NativeTable:
		c.table.Destroy()
	case toolkit.NativeTree:
		c.tree.Destroy()
	case toolkit.NativeTabs:
		c.tabs.Destroy()
	}
}

func (c *collection) setRows(rows [][]string) {
	c.table.SetRows(rows)
	c.rebuilds++
}

func (c *collection) setTree(tree []toolkit.NativeTreeNode) {
	c.tree.SetNodes(toTreeNodes(tree))
	c.rebuilds++
}

func (c *collection) setTabs(items []string) {
	c.tabs.SetTabs(items)
	c.rebuilds++
}

// ---- signals (the person's changes only: gtk4 does not report the host's own) ----

func (c *collection) rowSelected(i int) {
	c.lc.lastNum = float64(i)
	if c.lc.onNumber != nil {
		c.lc.onNumber(c.lc.lastNum)
	}
}

func (c *collection) pathSelected(path []int) {
	p := treePathString(path)
	c.lc.lastText = p
	if c.lc.onText != nil {
		c.lc.onText(p)
	}
}

func (c *collection) pageSwitched(i int) {
	c.lc.lastNum = float64(i)
	if c.lc.onNumber != nil {
		c.lc.onNumber(c.lc.lastNum)
	}
}

// apply reconciles a frame's descriptor: the data is re-read only when the
// Revision moved (so an unchanged 10 000-row table costs a comparison), and a
// selection is pushed only when the app moved it.
func (c *collection) apply(lc *liveControl, spec toolkit.NativeControl) {
	rebuilt := false
	if spec.Revision != c.rev {
		switch c.kind {
		case toolkit.NativeTable:
			c.setRows(spec.Rows)
		case toolkit.NativeTree:
			c.setTree(spec.Tree)
		case toolkit.NativeTabs:
			c.setTabs(spec.Items)
		}
		c.rev = spec.Revision
		rebuilt = true
	}
	switch c.kind {
	case toolkit.NativeTable:
		if rebuilt || spec.Number != lc.lastNum {
			c.table.SetSelected(int(spec.Number))
			lc.lastNum = spec.Number
		}
	case toolkit.NativeTree:
		if rebuilt || spec.Text != lc.lastText {
			c.tree.SetSelectedPath(parsePath(spec.Text))
			lc.lastText = spec.Text
		}
	case toolkit.NativeTabs:
		if rebuilt || spec.Number != lc.lastNum {
			c.tabs.SetCurrent(int(spec.Number))
			lc.lastNum = spec.Number
		}
	}
}
