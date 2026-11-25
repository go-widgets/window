// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package win32

import (
	"testing"
	"unsafe"
)

// TestCollectionStructLayouts pins the sizes of the structures a list view, tree
// view and their notifications are read and written through to the sizes
// commctrl.h gives them (LP64 Windows: amd64 and arm64). A control reads these
// blindly; a Go struct that drifted would corrupt the sender's memory, and no
// type system would say so.
func TestCollectionStructLayouts(t *testing.T) {
	if unsafe.Sizeof(uintptr(0)) != 8 {
		t.Skip("sizes below are the 64-bit ones")
	}
	for name, c := range map[string][2]uintptr{
		"NMHDR":          {unsafe.Sizeof(nmHdr{}), 24},
		"LVCOLUMNW":      {unsafe.Sizeof(lvColumnW{}), 56},
		"LVITEMW":        {unsafe.Sizeof(lvItemW{}), 88},
		"NMLVDISPINFOW":  {unsafe.Sizeof(nmLvDispInfoW{}), 112},
		"NMLISTVIEW":     {unsafe.Sizeof(nmListView{}), 64},
		"TVITEMW":        {unsafe.Sizeof(tvItemW{}), 56},
		"TVITEMEXW":      {unsafe.Sizeof(tvItemExW{}), 80},
		"TVINSERTSTRUCT": {unsafe.Sizeof(tvInsertStructW{}), 96},
		"NMTREEVIEWW":    {unsafe.Sizeof(nmTreeViewW{}), 152},
	} {
		if c[0] != c[1] {
			t.Errorf("%s: Go size %d, commctrl.h size %d", name, c[0], c[1])
		}
	}
}
