// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package win32

// The Win32 hosting of NativeTable, NativeTree and NativeTabs. Each is one child
// HWND, reconciled by native_windows.go like every other kind; this file holds
// the constants, structures, the control calls and the notification handling.
//
// Controls, and the documentation each was written from (Microsoft Learn,
// learn.microsoft.com/en-us/windows/win32/controls/):
//
//	Table "SysListView32" list-view-controls-overview ("Virtual List-View
//	      Style": LVS_OWNERDATA is fixed at creation, LVM_SETITEMCOUNT must be
//	      sent before items show, the control keeps only selection and focus,
//	      the owner answers LVN_GETDISPINFO; LVM_SETITEMSTATE changes only those)
//	Tree  "SysTreeView32" tree-view-controls (TVM_INSERTITEM under a parent
//	      handle, TVS_HASBUTTONS|HASLINES|LINESATROOT, TVM_EXPAND, TVN_SELCHANGED,
//	      TVM_SELECTITEM)
//	Tabs  "SysTabControl32" tab-controls (TCM_INSERTITEM, TCN_SELCHANGE,
//	      TCM_SETCURSEL, TCM_DELETEALLITEMS; the control is the strip, the app
//	      lays the page out in the display area itself)
//
// The numeric values are those of commctrl.h.

import (
	"unsafe"

	"github.com/go-widgets/toolkit"
)

const (
	// SysListView32.
	lvsReport        = 0x0001 // LVS_REPORT
	lvsSingleSel     = 0x0004 // LVS_SINGLESEL
	lvsShowSelAlways = 0x0008 // LVS_SHOWSELALWAYS
	lvsOwnerData     = 0x1000 // LVS_OWNERDATA: virtual, the owner supplies cells

	lvmSetItemCount    = 0x102F // LVM_SETITEMCOUNT
	lvmSetItemState    = 0x102B // LVM_SETITEMSTATE
	lvmEnsureVisible   = 0x1013 // LVM_ENSUREVISIBLE
	lvmDeleteColumn    = 0x101C // LVM_DELETECOLUMN
	lvmInsertColumnW   = 0x1061 // LVM_INSERTCOLUMNW
	lvmSetExtStyle     = 0x1036 // LVM_SETEXTENDEDLISTVIEWSTYLE
	lvsExFullRowSelect = 0x0020 // LVS_EX_FULLROWSELECT
	lvsExDoubleBuffer  = 0x10000

	lvsicfNoInvalidateAll = 0x0001 // LVSICF_NOINVALIDATEALL
	lvifText              = 0x0001 // LVIF_TEXT
	lvifState             = 0x0008 // LVIF_STATE
	lvcfWidth             = 0x0002 // LVCF_WIDTH
	lvcfText              = 0x0004 // LVCF_TEXT
	lvisFocused           = 0x0001 // LVIS_FOCUSED
	lvisSelected          = 0x0002 // LVIS_SELECTED

	lvnItemChanged   = -101 // LVN_ITEMCHANGED (LVN_FIRST - 1)
	lvnGetDispInfoW  = -177 // LVN_GETDISPINFOW (LVN_FIRST - 77)
	tvnSelChangedW   = -451 // TVN_SELCHANGEDW (TVN_FIRST - 51)
	iccListViewClass = 0x0001
	iccTreeViewClass = 0x0002

	// SysTreeView32.
	tvsHasButtons    = 0x0001 // TVS_HASBUTTONS
	tvsHasLines      = 0x0002 // TVS_HASLINES
	tvsLinesAtRoot   = 0x0004 // TVS_LINESATROOT
	tvsShowSelAlways = 0x0020 // TVS_SHOWSELALWAYS

	tvmDeleteItem = 0x1101 // TVM_DELETEITEM
	tvmExpand     = 0x1102 // TVM_EXPAND
	tvmSelectItem = 0x110B // TVM_SELECTITEM
	tvmInsertW    = 0x1132 // TVM_INSERTITEMW
	tveExpand     = 0x0002 // TVE_EXPAND
	tvgnCaret     = 0x0009 // TVGN_CARET
	tvifText      = 0x0001 // TVIF_TEXT
	tvifParam     = 0x0004 // TVIF_PARAM

	wmSetRedraw = 0x000B // WM_SETREDRAW

	// SysTabControl32 (the strip; TCS_BUTTONS belongs to NativeSegmented).
	tcmDeleteAll = 0x1309 // TCM_DELETEALLITEMS
)

var (
	classListView = mustUTF16("SysListView32")
	classTreeView = mustUTF16("SysTreeView32")
)

// tviRoot and tviLast are the pseudo handles TVI_ROOT (-0x10000) and TVI_LAST
// (-0xFFFE), as pointer-sized values.
const (
	tviRoot = ^uintptr(0xFFFF)
	tviLast = ^uintptr(0xFFFD)
)

type lvColumnW struct {
	mask       uint32
	fmt        int32
	cx         int32
	pszText    *uint16
	cchTextMax int32
	iSubItem   int32
	iImage     int32
	iOrder     int32
	cxMin      int32
	cxDefault  int32
	cxIdeal    int32
}

type lvItemW struct {
	mask       uint32
	iItem      int32
	iSubItem   int32
	state      uint32
	stateMask  uint32
	pszText    *uint16
	cchTextMax int32
	iImage     int32
	lParam     uintptr
	iIndent    int32
	iGroupID   int32
	cColumns   uint32
	puColumns  *uint32
	piColFmt   *int32
	iGroup     int32
}

type nmLvDispInfoW struct {
	hdr  nmHdr
	item lvItemW
}

type nmListView struct {
	hdr       nmHdr
	iItem     int32
	iSubItem  int32
	uNewState uint32
	uOldState uint32
	uChanged  uint32
	ptX, ptY  int32
	lParam    uintptr
}

type tvItemW struct {
	mask           uint32
	hItem          uintptr
	state          uint32
	stateMask      uint32
	pszText        *uint16
	cchTextMax     int32
	iImage         int32
	iSelectedImage int32
	cChildren      int32
	lParam         uintptr
}

type tvItemExW struct {
	tvItemW
	iIntegral      int32
	uStateEx       uint32
	hwnd           uintptr
	iExpandedImage int32
	iReserved      int32
}

type tvInsertStructW struct {
	hParent      uintptr
	hInsertAfter uintptr
	item         tvItemExW
}

type nmTreeViewW struct {
	hdr     nmHdr
	action  uint32
	itemOld tvItemW
	itemNew tvItemW
	ptX     int32
	ptY     int32
}

// collectionState is what a table, tree or tab strip keeps beside the control:
// the revision it last took, the data the control asks for (virtual table), and
// the tree's flattened nodes with their handles.
type collectionState struct {
	rev  uint64
	cols []string
	rows [][]string

	tree    []treeEntry
	treeIdx map[string]int
	treeH   []uintptr

	// busy is true while the host itself rebuilds the control: the selection
	// notifications that rebuilding causes are not the person's.
	busy bool
}

// collectionKind reports whether k is one of the three kinds of this file.
func collectionKind(k toolkit.NativeKind) bool {
	return k == toolkit.NativeTable || k == toolkit.NativeTree || k == toolkit.NativeTabs
}

// collectionSync reconciles a table, tree or tab strip with its descriptor. The
// data is re-read only when the revision moved (or on first); the selection is
// pushed only when it differs from what the control last reported.
func (w *Window) collectionSync(lc *liveControl, spec toolkit.NativeControl, first bool) {
	reload := first || dataChanged(lc.coll.rev, spec.Revision)
	if reload {
		lc.coll.rev = spec.Revision
		_, _, cw, _ := w.controlRect(spec.Rect)
		lc.coll.busy = true
		switch lc.kind {
		case toolkit.NativeTable:
			tableLoad(lc, spec, cw)
		case toolkit.NativeTree:
			treeLoad(lc, spec)
		case toolkit.NativeTabs:
			lc.items = spec.Items
			tabsLoad(lc.hwnd, spec.Items)
		}
		lc.coll.busy = false
	}
	switch lc.kind {
	case toolkit.NativeTable:
		if reload || spec.Number != lc.lastNum {
			lc.coll.busy = true
			tableSelect(lc.hwnd, rowIndex(spec.Number, len(lc.coll.rows)))
			lc.coll.busy = false
			lc.lastNum = spec.Number
		}
	case toolkit.NativeTabs:
		if reload || spec.Number != lc.lastNum {
			lc.coll.busy = true
			tabSelect(lc.hwnd, rowIndex(spec.Number, len(lc.items)))
			lc.coll.busy = false
			lc.lastNum = spec.Number
		}
	case toolkit.NativeTree:
		if reload || spec.Text != lc.lastText {
			lc.coll.busy = true
			treeSelect(lc, treeSelection(lc.coll.treeIdx, spec.Text))
			lc.coll.busy = false
			lc.lastText = spec.Text
		}
	}
}

// --- table (virtual list view) ---------------------------------------------------

// tableLoad takes the descriptor's rows and columns. It never inserts an item:
// the control only learns how many rows there are and asks for the cells it
// paints. Columns are rebuilt only when their titles changed.
func tableLoad(lc *liveControl, spec toolkit.NativeControl, width int32) {
	if !sameStrings(lc.coll.cols, spec.Columns) {
		for sendMessage(lc.hwnd, lvmDeleteColumn, 0, 0) != 0 {
		}
		cw := columnWidth(width, len(spec.Columns))
		for i, c := range spec.Columns {
			col := lvColumnW{mask: lvcfText | lvcfWidth, cx: cw, pszText: mustUTF16(c), iSubItem: int32(i)}
			sendMessage(lc.hwnd, lvmInsertColumnW, uintptr(i), uintptr(unsafe.Pointer(&col)))
		}
	}
	lc.coll.cols = spec.Columns
	lc.coll.rows = spec.Rows
	sendMessage(lc.hwnd, lvmSetItemCount, uintptr(len(spec.Rows)), lvsicfNoInvalidateAll)
	procInvalidateRect.Call(lc.hwnd, 0, 1) // once, for the new row count
}

// tableSelect selects row i (and makes it visible), or clears the selection for
// -1. A virtual list view keeps only selection and focus, so these are the only
// state bits there are to set.
func tableSelect(hwnd uintptr, i int) {
	if i < 0 {
		it := lvItemW{stateMask: lvisSelected}
		sendMessage(hwnd, lvmSetItemState, ^uintptr(0), uintptr(unsafe.Pointer(&it))) // -1: every item
		return
	}
	it := lvItemW{state: lvisSelected | lvisFocused, stateMask: lvisSelected | lvisFocused}
	sendMessage(hwnd, lvmSetItemState, uintptr(i), uintptr(unsafe.Pointer(&it)))
	sendMessage(hwnd, lvmEnsureVisible, uintptr(i), 0)
}

// --- tree ---------------------------------------------------------------------

// treeLoad rebuilds the tree from the descriptor: drawing is held while the
// nodes go in (parents before children), then the nodes asked to be expanded are
// opened, deepest first.
func treeLoad(lc *liveControl, spec toolkit.NativeControl) {
	hwnd := lc.hwnd
	sendMessage(hwnd, wmSetRedraw, 0, 0)
	sendMessage(hwnd, tvmDeleteItem, 0, tviRoot)
	entries := flattenTree(spec.Tree)
	handles := make([]uintptr, len(entries))
	for i, e := range entries {
		parent := tviRoot
		if e.Parent >= 0 {
			parent = handles[e.Parent]
		}
		ins := tvInsertStructW{hParent: parent, hInsertAfter: tviLast}
		ins.item.mask = tvifText | tvifParam
		ins.item.pszText = mustUTF16(e.Title)
		ins.item.lParam = uintptr(i)
		handles[i] = sendMessage(hwnd, tvmInsertW, 0, uintptr(unsafe.Pointer(&ins)))
	}
	for i := len(entries) - 1; i >= 0; i-- {
		if entries[i].Expanded && !entries[i].Leaf && handles[i] != 0 {
			sendMessage(hwnd, tvmExpand, tveExpand, handles[i])
		}
	}
	lc.coll.tree, lc.coll.treeH, lc.coll.treeIdx = entries, handles, treeIndex(entries)
	sendMessage(hwnd, wmSetRedraw, 1, 0)
	procInvalidateRect.Call(hwnd, 0, 1)
}

// treeSelect selects entry i, or clears the selection for -1 (a null item).
func treeSelect(lc *liveControl, i int) {
	h := uintptr(0)
	if i >= 0 && i < len(lc.coll.treeH) {
		h = lc.coll.treeH[i]
	}
	sendMessage(lc.hwnd, tvmSelectItem, tvgnCaret, h)
}

// --- tabs ---------------------------------------------------------------------

// tabsLoad replaces the titles of a tab strip.
func tabsLoad(hwnd uintptr, items []string) {
	sendMessage(hwnd, tcmDeleteAll, 0, 0)
	for i, it := range items {
		ti := tcItemW{mask: tcifText, pszText: mustUTF16(it)}
		sendMessage(hwnd, tcmInsertW, uintptr(i), uintptr(unsafe.Pointer(&ti)))
	}
}

// --- notifications --------------------------------------------------------------

// collectionNotify handles a WM_NOTIFY from a table, tree or tab strip. handled
// is false for a code it does not know; repaint says whether the app's model may
// have changed (a virtual list asks for cells hundreds of times a second and must
// not repaint the frame for each).
func (w *Window) collectionNotify(lc *liveControl, h *nmHdr, lParam uintptr) (handled, repaint bool) {
	switch lc.kind {
	case toolkit.NativeTable:
		switch int32(h.code) {
		case lvnGetDispInfoW:
			di := (*nmLvDispInfoW)(ptrOf(lParam))
			if di.item.mask&lvifText != 0 && di.item.pszText != nil && di.item.cchTextMax > 0 {
				buf := unsafe.Slice(di.item.pszText, int(di.item.cchTextMax))
				copyUTF16(buf, cellText(lc.coll.rows, int(di.item.iItem), int(di.item.iSubItem)))
			}
			return true, false
		case lvnItemChanged:
			nm := (*nmListView)(ptrOf(lParam))
			if lc.coll.busy || nm.iItem < 0 || nm.uChanged&lvifState == 0 || nm.uNewState&lvisSelected == 0 {
				return true, false
			}
			if v := float64(nm.iItem); v != lc.lastNum {
				lc.lastNum = v
				if lc.onNumber != nil {
					lc.onNumber(v)
				}
				return true, true
			}
			return true, false
		}
	case toolkit.NativeTree:
		if int32(h.code) == tvnSelChangedW {
			nm := (*nmTreeViewW)(ptrOf(lParam))
			i := int(nm.itemNew.lParam)
			if lc.coll.busy || nm.itemNew.hItem == 0 || i < 0 || i >= len(lc.coll.tree) {
				return true, false
			}
			if p := lc.coll.tree[i].Path; p != lc.lastText {
				lc.lastText = p
				if lc.onText != nil {
					lc.onText(p)
				}
				return true, true
			}
			return true, false
		}
	case toolkit.NativeTabs:
		if int32(h.code) == tcnSelChange {
			if i := tabSelected(lc.hwnd); i >= 0 && i < len(lc.items) {
				if v := float64(i); v != lc.lastNum {
					lc.lastNum = v
					if lc.onNumber != nil {
						lc.onNumber(v)
					}
				}
			}
			return true, true
		}
	}
	return false, false
}
