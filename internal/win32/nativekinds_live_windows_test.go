// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows && integration

package win32

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"testing"
	"unsafe"

	"github.com/go-mswin/win32"
	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

var (
	procEnumChildWindows = user32.NewProc("EnumChildWindows")
	procGetClassNameW    = user32.NewProc("GetClassNameW")
)

// childClasses enumerates the child windows of hwnd (EnumChildWindows) and
// returns each one's window class name (GetClassNameW): the Win32 answer to
// "which controls does this window really hold", independent of our own tables.
func childClasses(hwnd uintptr) []string {
	var classes []string
	cb := win32.NewCallback(func(child, _ uintptr) uintptr {
		buf := make([]uint16, 256)
		n, _, _ := procGetClassNameW.Call(child, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
		classes = append(classes, syscall.UTF16ToString(buf[:n]))
		return 1 // keep enumerating
	})
	procEnumChildWindows.Call(hwnd, cb, 0)
	sort.Strings(classes)
	return classes
}

// TestLiveWin32NativeKinds is the CI proof that the Win32 host creates REAL
// controls: an app describing an Entry plus the five kinds this host added
// (List, Link, Date, Color, Segmented) must end up as child windows of exactly
// those classes. A missing class fails the lane. It then drives each control's
// own state and notification path, so a control that exists but is not wired is
// caught as well.
func TestLiveWin32NativeKinds(t *testing.T) {
	skipUnlessLive(t)
	// A window belongs to the thread that created it, and creating a child from
	// another thread sends the parent's thread a message it is not pumping: the
	// lane hung that way, intermittently, whenever the scheduler moved this
	// goroutine. Open pins its loop goroutine for the same reason.
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)

	win, err := New("go-widgets/window win32 native kinds", 480, 400, toolkit.DefaultDark())
	if err != nil {
		t.Fatalf("window: %v", err)
	}
	t.Cleanup(func() { win.Close() })

	var (
		gotRow   float64 = -9
		gotDate  string
		gotColor string
		gotSeg   string
		links    int

		gotTable float64 = -9
		gotTree  string
		gotTab   float64 = -9
	)
	r := toolkit.Rect{X: 0, Y: 0, W: 200, H: 60}
	specs := []toolkit.NativeControl{
		{Kind: toolkit.NativeEntry, Key: "entry", Rect: r, Visible: true, Text: "hello"},
		{Kind: toolkit.NativeList, Key: "list", Rect: r, Visible: true, Items: []string{"a", "b", "c"}, Number: 1,
			OnNumber: func(v float64) { gotRow = v }},
		{Kind: toolkit.NativeLink, Key: "link", Rect: r, Visible: true, Text: "Terms & <conditions>",
			OnActivate: func() { links++ }},
		{Kind: toolkit.NativeDate, Key: "date", Rect: r, Visible: true, Text: "2026-02-14",
			OnText: func(s string) { gotDate = s }},
		{Kind: toolkit.NativeColor, Key: "color", Rect: r, Visible: true, Text: "#3366cc",
			OnText: func(s string) { gotColor = s }},
		{Kind: toolkit.NativeSegmented, Key: "seg", Rect: r, Visible: true, Items: []string{"one", "two", "three"}, Text: "two",
			OnText: func(s string) { gotSeg = s }},
		{Kind: toolkit.NativeTable, Key: "table", Rect: toolkit.Rect{W: 300, H: 120}, Visible: true,
			Columns: []string{"id", "name"}, Rows: tableRows(10000), Revision: 1, Number: 2,
			OnNumber: func(v float64) { gotTable = v }},
		{Kind: toolkit.NativeTree, Key: "tree", Rect: toolkit.Rect{W: 200, H: 120}, Visible: true,
			Tree: sampleTree(), Revision: 1, Text: "0.1",
			OnText: func(s string) { gotTree = s }},
		{Kind: toolkit.NativeTabs, Key: "tabs", Rect: toolkit.Rect{W: 300, H: 30}, Visible: true,
			Items: []string{"one", "two", "three"}, Revision: 1, Number: 1,
			OnNumber: func(v float64) { gotTab = v }},
	}
	root := &specRoot{specs: specs}
	win.root = root
	// One control at a time, logging first: a control that cannot be created
	// must name itself rather than hang the whole lane.
	all := specs
	for i := range all {
		t.Logf("creating %s", all[i].Key)
		root.specs = all[:i+1]
		win.syncNative(root)
	}
	root.specs = all

	// 1. Every class is a child window of ours.
	want := map[string]string{
		"entry": "Edit", "list": "ListBox", "link": "SysLink",
		"date": "SysDateTimePick32", "color": "GoWidgetsColorWell", "seg": "SysTabControl32",
		"table": "SysListView32", "tree": "SysTreeView32", "tabs": "SysTabControl32",
	}
	classes := childClasses(win.hwnd)
	t.Logf("child window classes: %v", classes)
	for key, class := range want {
		found := false
		for _, c := range classes {
			if strings.EqualFold(c, class) {
				found = true
			}
		}
		if !found {
			t.Errorf("%s: no child window of class %q (have %v)", key, class, classes)
		}
		if win.nativeControls[key] == nil {
			t.Errorf("%s: no liveControl", key)
		}
	}
	if t.Failed() {
		return
	}

	// 2. Initial state reached the controls.
	if got := listSelected(win.nativeControls["list"].hwnd); got != 1 {
		t.Errorf("list selection = %d, want 1", got)
	}
	if got, _ := dateGet(win.nativeControls["date"].hwnd); got != "2026-02-14" {
		t.Errorf("date = %q, want 2026-02-14", got)
	}
	if got := colorHex(wellColor(win.nativeControls["color"].hwnd)); got != "#3366cc" {
		t.Errorf("color = %q, want #3366cc", got)
	}
	if got := tabSelected(win.nativeControls["seg"].hwnd); got != 1 {
		t.Errorf("segment = %d, want 1", got)
	}

	// 3. app -> native: a changed descriptor is pushed in on the next reconcile.
	specs[1].Number = 2
	specs[3].Text = "2027-12-31"
	specs[4].Text = "#ff0000"
	specs[5].Text = "three"
	win.syncNative(root)
	if got := listSelected(win.nativeControls["list"].hwnd); got != 2 {
		t.Errorf("after push, list = %d, want 2", got)
	}
	if got, _ := dateGet(win.nativeControls["date"].hwnd); got != "2027-12-31" {
		t.Errorf("after push, date = %q", got)
	}
	if got := colorHex(wellColor(win.nativeControls["color"].hwnd)); got != "#ff0000" {
		t.Errorf("after push, color = %q", got)
	}
	if got := tabSelected(win.nativeControls["seg"].hwnd); got != 2 {
		t.Errorf("after push, segment = %d, want 2", got)
	}

	// 4. native -> app: the person's own change is reported through the same
	// message the control would send the window.
	lst := win.nativeControls["list"]
	listSelect(lst.hwnd, 0)
	win.onCommand(uintptr(lbnSelChange)<<16|uintptr(lst.id), lst.hwnd)
	if gotRow != 0 {
		t.Errorf("list report = %v, want 0", gotRow)
	}

	seg := win.nativeControls["seg"]
	tabSelect(seg.hwnd, 0)
	notify(t, win, seg.hwnd, tcnSelChange)
	if gotSeg != "one" {
		t.Errorf("segment report = %q, want one", gotSeg)
	}

	dt := win.nativeControls["date"]
	dateSet(dt.hwnd, "2030-01-02")
	notify(t, win, dt.hwnd, dtnDateTimeChange)
	if gotDate != "2030-01-02" {
		t.Errorf("date report = %q, want 2030-01-02", gotDate)
	}

	col := win.nativeControls["color"]
	wellSet(col.hwnd, 0x00ff00)
	win.onCommand(0, col.hwnd)
	if gotColor != "#00ff00" {
		t.Errorf("color report = %q, want #00ff00", gotColor)
	}

	lk := win.nativeControls["link"]
	notify(t, win, lk.hwnd, nmClick)
	notify(t, win, lk.hwnd, nmReturn)
	if links != 2 {
		t.Errorf("link activations = %d, want 2", links)
	}
	if !lk.link {
		t.Errorf("link was hosted as the push-button fallback, not a SysLink")
	}

	// 5. The collections. Both SysTabControl32 children must exist (the
	// segmented control and the tab strip), told apart by TCS_BUTTONS.
	if n := countClass(classes, "SysTabControl32"); n != 2 {
		t.Errorf("SysTabControl32 children = %d, want 2 (segmented + tabs)", n)
	}
	tbl := win.nativeControls["table"]
	if style := getWindowStyle(tbl.hwnd); style&lvsOwnerData == 0 || style&lvsReport == 0 {
		t.Errorf("table style %#x lacks LVS_OWNERDATA|LVS_REPORT", style)
	}
	if got := sendMessage(tbl.hwnd, lvmGetItemCount, 0, 0); got != 10000 {
		t.Errorf("table item count = %d, want 10000 (virtual, one SETITEMCOUNT)", got)
	}
	if got := tableSelected(tbl.hwnd); got != 2 {
		t.Errorf("table selection = %d, want 2", got)
	}
	// The control asks for cells; answer through the real notification path.
	if got := dispInfo(t, win, tbl, 7, 1); got != "name7" {
		t.Errorf("cell(7,1) = %q, want name7", got)
	}
	if got := dispInfo(t, win, tbl, 99999, 5); got != "" {
		t.Errorf("cell past the data = %q, want empty", got)
	}
	// An unchanged Revision costs nothing: no SETITEMCOUNT, so a count the test
	// plants by hand survives the reconcile; a moved Revision re-reads.
	sendMessage(tbl.hwnd, lvmSetItemCount, 5, lvsicfNoInvalidateAll)
	win.syncNative(root)
	if got := sendMessage(tbl.hwnd, lvmGetItemCount, 0, 0); got != 5 {
		t.Errorf("unchanged revision re-read the table (count %d)", got)
	}
	specs[6].Revision = 2
	specs[6].Number = 4
	win.syncNative(root)
	if got := sendMessage(tbl.hwnd, lvmGetItemCount, 0, 0); got != 10000 {
		t.Errorf("moved revision did not re-read (count %d)", got)
	}
	if got := tableSelected(tbl.hwnd); got != 4 {
		t.Errorf("after push, table selection = %d, want 4", got)
	}
	// native -> app: the person picks row 9.
	tableSelect(tbl.hwnd, 9)
	notifyItemChanged(t, win, tbl, 9)
	if gotTable != 9 {
		t.Errorf("table report = %v, want 9", gotTable)
	}

	tree := win.nativeControls["tree"]
	if sendMessage(tree.hwnd, tvmGetCount, 0, 0) != 5 {
		t.Errorf("tree node count = %d, want 5", sendMessage(tree.hwnd, tvmGetCount, 0, 0))
	}
	if got := treeSelectedPath(tree); got != "0.1" {
		t.Errorf("tree selection = %q, want 0.1", got)
	}
	specs[7].Text = "1"
	win.syncNative(root)
	if got := treeSelectedPath(tree); got != "1" {
		t.Errorf("after push, tree selection = %q, want 1", got)
	}
	treeSelect(tree, treeSelection(tree.coll.treeIdx, "0.0"))
	notifyTreeSel(t, win, tree, "0.0")
	if gotTree != "0.0" {
		t.Errorf("tree report = %q, want 0.0", gotTree)
	}

	tabs := win.nativeControls["tabs"]
	if style := getWindowStyle(tabs.hwnd); style&tcsButtons != 0 {
		t.Errorf("tab strip was made with TCS_BUTTONS (%#x)", style)
	}
	if got := tabSelected(tabs.hwnd); got != 1 {
		t.Errorf("tab = %d, want 1", got)
	}
	specs[8].Number = 2
	win.syncNative(root)
	if got := tabSelected(tabs.hwnd); got != 2 {
		t.Errorf("after push, tab = %d, want 2", got)
	}
	tabSelect(tabs.hwnd, 0)
	notify(t, win, tabs.hwnd, tcnSelChange)
	if gotTab != 0 {
		t.Errorf("tab report = %v, want 0", gotTab)
	}
}

const (
	lvmGetItemCount = 0x1004 // LVM_GETITEMCOUNT
	lvmGetNextItem  = 0x100C // LVM_GETNEXTITEM
	lvniSelected    = 0x0002 // LVNI_SELECTED
	tvmGetCount     = 0x1105 // TVM_GETCOUNT
	tvmGetNextItem  = 0x110A // TVM_GETNEXTITEM
	tvmGetItemW     = 0x113E // TVM_GETITEMW
	gwlStyle        = -16
)

func getWindowStyle(hwnd uintptr) uint32 {
	r, _, _ := procGetWindowLongPtrW.Call(hwnd, ^uintptr(15)) // GWL_STYLE (-16)
	return uint32(r)
}

func countClass(classes []string, class string) int {
	n := 0
	for _, c := range classes {
		if strings.EqualFold(c, class) {
			n++
		}
	}
	return n
}

func tableRows(n int) [][]string {
	rows := make([][]string, n)
	for i := range rows {
		rows[i] = []string{fmt.Sprint(i), fmt.Sprintf("name%d", i)}
	}
	return rows
}

func tableSelected(hwnd uintptr) int {
	return int(int32(sendMessage(hwnd, lvmGetNextItem, ^uintptr(0), lvniSelected)))
}

// dispInfo delivers an LVN_GETDISPINFOW the way the list view does and returns
// the text the host put in the buffer the control supplied.
func dispInfo(t *testing.T, w *Window, lc *liveControl, row, col int) string {
	t.Helper()
	buf := make([]uint16, 64)
	di := nmLvDispInfoW{
		hdr:  nmHdr{hwndFrom: lc.hwnd, code: uint32(lvnGetDispInfoW & 0xFFFFFFFF)},
		item: lvItemW{mask: lvifText, iItem: int32(row), iSubItem: int32(col), pszText: &buf[0], cchTextMax: int32(len(buf))},
	}
	if !w.onNotify(uintptr(unsafe.Pointer(&di))) {
		t.Errorf("LVN_GETDISPINFO was not handled")
	}
	return syscall.UTF16ToString(buf)
}

func notifyItemChanged(t *testing.T, w *Window, lc *liveControl, row int) {
	t.Helper()
	nm := nmListView{
		hdr:       nmHdr{hwndFrom: lc.hwnd, code: uint32(lvnItemChanged & 0xFFFFFFFF)},
		iItem:     int32(row),
		uChanged:  lvifState,
		uNewState: lvisSelected,
	}
	if !w.onNotify(uintptr(unsafe.Pointer(&nm))) {
		t.Errorf("LVN_ITEMCHANGED was not handled")
	}
}

// treeSelectedPath asks the control which node is selected (TVM_GETNEXTITEM,
// TVGN_CARET) and returns that node's path from its lParam.
func treeSelectedPath(lc *liveControl) string {
	h := sendMessage(lc.hwnd, tvmGetNextItem, tvgnCaret, 0)
	if h == 0 {
		return ""
	}
	it := tvItemW{mask: tvifParam, hItem: h}
	sendMessage(lc.hwnd, tvmGetItemW, 0, uintptr(unsafe.Pointer(&it)))
	if i := int(it.lParam); i >= 0 && i < len(lc.coll.tree) {
		return lc.coll.tree[i].Path
	}
	return ""
}

func notifyTreeSel(t *testing.T, w *Window, lc *liveControl, path string) {
	t.Helper()
	nm := nmTreeViewW{hdr: nmHdr{hwndFrom: lc.hwnd, code: uint32(tvnSelChangedW & 0xFFFFFFFF)}}
	nm.itemNew.hItem = 1
	nm.itemNew.lParam = uintptr(lc.coll.treeIdx[path])
	if !w.onNotify(uintptr(unsafe.Pointer(&nm))) {
		t.Errorf("TVN_SELCHANGED was not handled")
	}
}

// notify delivers a WM_NOTIFY for the control to the window.
func notify(t *testing.T, w *Window, from uintptr, code int32) {
	t.Helper()
	h := nmHdr{hwndFrom: from, code: uint32(code)}
	if !w.onNotify(uintptr(unsafe.Pointer(&h))) {
		t.Errorf("onNotify(%d) was not handled", code)
	}
}

// specRoot is a root that supplies its descriptors directly, the way a
// toolkit.Surface does.
type specRoot struct {
	toolkit.Base
	specs []toolkit.NativeControl
}

func (r *specRoot) NativeControls() []toolkit.NativeControl { return r.specs }
func (r *specRoot) Draw(painter.Painter, *toolkit.Theme)    {}
