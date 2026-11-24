// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows && integration

package win32

import (
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
