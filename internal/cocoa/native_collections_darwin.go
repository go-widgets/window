// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package cocoa

import (
	"github.com/go-macos/appkit"
	"github.com/go-widgets/toolkit"
)

// The Cocoa backing of toolkit's table, tree and tab strip: an NSTableView in
// data-source mode, an NSOutlineView and an NSTabView. Their data is NOT compared
// cell by cell every frame; a table or tree is pushed when the descriptor's
// Revision moves, and the selection is pushed when it differs from the one the
// control last reported, as for every other kind.

// isCollection reports whether a kind is one of the three.
func isCollection(k toolkit.NativeKind) bool {
	return k == toolkit.NativeTable || k == toolkit.NativeTree || k == toolkit.NativeTabs
}

// newCollection builds the AppKit control of a table, tree or tab strip.
func newCollection(spec toolkit.NativeControl) (*appkit.Control, error) {
	switch spec.Kind {
	case toolkit.NativeTable:
		return appkit.NewGridView(spec.Columns, spec.Rows)
	case toolkit.NativeTree:
		return appkit.NewOutlineView(treeItems(spec.Tree))
	default:
		return appkit.NewTabView(spec.Items)
	}
}

// treeItems is the descriptor's tree in appkit's terms.
func treeItems(nodes []toolkit.NativeTreeNode) []appkit.TreeItem {
	if len(nodes) == 0 {
		return nil
	}
	out := make([]appkit.TreeItem, len(nodes))
	for i, n := range nodes {
		out[i] = appkit.TreeItem{Title: n.Title, Expanded: n.Expanded, Children: treeItems(n.Children)}
	}
	return out
}

// initCollection chooses what the descriptor says is chosen, on a control just
// built, and records the revision the control was built at.
func (lc *liveControl) initCollection(spec toolkit.NativeControl) {
	lc.collRev = spec.Revision
	switch spec.Kind {
	case toolkit.NativeTable, toolkit.NativeTabs:
		_ = lc.ctl.SetDouble(spec.Number)
	case toolkit.NativeTree:
		if spec.Text != "" {
			_ = lc.ctl.SetStringValue(spec.Text)
		}
	}
}

// wireCollection routes the control's selection changes out through the
// descriptor's callbacks: a row or a tab as a number, a tree node as its path.
func (lc *liveControl) wireCollection(kind toolkit.NativeKind) {
	switch kind {
	case toolkit.NativeTable, toolkit.NativeTabs:
		lc.ctl.OnChange(func() { lc.reportNumber() })
	case toolkit.NativeTree:
		lc.ctl.OnChange(func() { lc.reportText() })
	}
}

// applyCollection brings a live table, tree or tab strip up to this frame's
// descriptor. Replacing the data drops AppKit's selection, so the selection is
// pushed again after it even when the application has not moved it.
func (lc *liveControl) applyCollection(spec toolkit.NativeControl) {
	revalue := false
	switch spec.Kind {
	case toolkit.NativeTable:
		if spec.Revision != lc.collRev {
			_ = lc.ctl.SetGrid(spec.Columns, spec.Rows)
			lc.collRev, revalue = spec.Revision, true
		}
		if spec.Number != lc.lastNum || revalue {
			_ = lc.ctl.SetDouble(spec.Number)
			lc.lastNum = spec.Number
		}
	case toolkit.NativeTree:
		if spec.Revision != lc.collRev {
			_ = lc.ctl.SetTree(treeItems(spec.Tree))
			lc.collRev, revalue = spec.Revision, true
		}
		if spec.Text != lc.lastText || revalue {
			_ = lc.ctl.SetStringValue(spec.Text)
			lc.lastText = spec.Text
		}
	case toolkit.NativeTabs:
		if !sameStrings(spec.Items, lc.lastItems) {
			_ = lc.ctl.SetItems(spec.Items)
			lc.lastItems = append(lc.lastItems[:0], spec.Items...)
			revalue = true
		}
		if spec.Number != lc.lastNum || revalue {
			_ = lc.ctl.SetDouble(spec.Number)
			lc.lastNum = spec.Number
		}
	}
}
