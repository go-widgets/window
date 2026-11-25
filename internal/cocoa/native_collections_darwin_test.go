// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package cocoa

import (
	"reflect"
	"testing"

	"github.com/go-macos/appkit"
	"github.com/go-widgets/toolkit"
)

func TestTreeItems(t *testing.T) {
	if got := treeItems(nil); got != nil {
		t.Errorf("treeItems(nil) = %v, want nil", got)
	}
	got := treeItems([]toolkit.NativeTreeNode{
		{Title: "a", Expanded: true, Children: []toolkit.NativeTreeNode{{Title: "a0"}}},
		{Title: "b"},
	})
	want := []appkit.TreeItem{
		{Title: "a", Expanded: true, Children: []appkit.TreeItem{{Title: "a0"}}},
		{Title: "b"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("treeItems = %+v, want %+v", got, want)
	}
}

func TestIsCollection(t *testing.T) {
	for _, k := range []toolkit.NativeKind{toolkit.NativeTable, toolkit.NativeTree, toolkit.NativeTabs} {
		if !isCollection(k) {
			t.Errorf("isCollection(%v) = false", k)
		}
	}
	for _, k := range []toolkit.NativeKind{toolkit.NativeList, toolkit.NativeButton, toolkit.NativeSegmented} {
		if isCollection(k) {
			t.Errorf("isCollection(%v) = true", k)
		}
	}
}
