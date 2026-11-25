// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows && integration

package win32

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"reflect"
	"runtime"
	"syscall"
	"testing"
	"unsafe"

	"github.com/go-widgets/toolkit"
)

var (
	procGetMenuItemCount = user32.NewProc("GetMenuItemCount")
	procGetMenuStringW   = user32.NewProc("GetMenuStringW")
	procGetMenuState     = user32.NewProc("GetMenuState")
)

const mfByPosition = 0x0400 // MF_BYPOSITION

func livePNG(t *testing.T, side int) []byte {
	t.Helper()
	m := image.NewNRGBA(image.Rect(0, 0, side, side))
	for y := 0; y < side; y++ {
		for x := 0; x < side; x++ {
			m.SetNRGBA(x, y, color.NRGBA{R: 200, G: 30, B: 30, A: 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, m); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// menuRows reads a real HMENU back the way a person would see it.
func menuRows(hmenu uintptr) (labels []string, grayed, sep []bool) {
	n, _, _ := procGetMenuItemCount.Call(hmenu)
	for i := uintptr(0); i < n; i++ {
		buf := make([]uint16, 128)
		l, _, _ := procGetMenuStringW.Call(hmenu, i, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), mfByPosition)
		st, _, _ := procGetMenuState.Call(hmenu, i, mfByPosition)
		labels = append(labels, syscall.UTF16ToString(buf[:l]))
		grayed = append(grayed, st&mfGrayed != 0)
		sep = append(sep, st&mfSeparator != 0)
	}
	return
}

// TestLiveWin32ContextMenuAndImage proves, on a real Windows host, that a
// button's Menu is built as a real HMENU when WM_CONTEXTMENU is posted to the
// CHILD (which forwards it to the parent), that the pick runs the newest
// closure, and that a PNG lands on the button and is released with it.
// The modal TrackPopupMenu loop itself is replaced by a function that reads the
// real menu and answers for the person: it is not driven by a pointer.
func TestLiveWin32ContextMenuAndImage(t *testing.T) {
	skipUnlessLive(t)
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)

	win, err := New("go-widgets/window win32 menu+image", 360, 200, toolkit.DefaultDark())
	if err != nil {
		t.Fatalf("window: %v", err)
	}
	t.Cleanup(func() { win.Close() })

	var old, newest int
	spec := func(pick func(), png []byte) toolkit.NativeControl {
		return toolkit.NativeControl{
			Kind: toolkit.NativeButton, Key: "b", Visible: true, Text: "Go",
			Rect:  toolkit.Rect{W: 120, H: 40},
			Image: png,
			Menu: []toolkit.NativeMenuItem{
				{Label: "Open & run", Pick: pick},
				{},
				{Label: "Disabled"},
			},
		}
	}
	root := &specRoot{specs: []toolkit.NativeControl{spec(func() { old++ }, livePNG(t, 32))}}
	win.root = root
	win.syncNative(root)
	lc := win.nativeControls["b"]
	if lc == nil {
		t.Fatal("button not created")
	}
	if lc.imageMode == imageNone || lc.hbmp == 0 {
		t.Fatalf("no picture on the button: mode %d hbmp %#x", lc.imageMode, lc.hbmp)
	}
	t.Logf("image mode %d (1 = bare bitmap, 2 = beside caption, comctl32 v6)", lc.imageMode)
	firstBmp := lc.hbmp

	// An unreadable PNG changes nothing; the same bytes rebuild nothing.
	root.specs = []toolkit.NativeControl{spec(func() { newest++ }, []byte("not a png"))}
	win.syncNative(root)
	if lc.hbmp != firstBmp {
		t.Error("a bad PNG replaced the picture")
	}

	restore := trackPopupMenu
	t.Cleanup(func() { trackPopupMenu = restore })
	var gotLabels []string
	var gotGrayed, gotSep []bool
	trackPopupMenu = func(owner, hmenu uintptr, x, y int32) uintptr {
		gotLabels, gotGrayed, gotSep = menuRows(hmenu)
		return 1 // the person picks the first entry
	}
	// To the CHILD: its default handling forwards WM_CONTEXTMENU to the parent.
	sendMessage(lc.hwnd, wmContextMenu, lc.hwnd, 0x00640064)
	if want := []string{"Open && run", "", "Disabled"}; len(gotLabels) != 3 {
		t.Fatalf("menu rows = %q, want %q", gotLabels, want)
	}
	if gotLabels[0] != "Open && run" && gotLabels[0] != "Open & run" {
		t.Errorf("label %q", gotLabels[0])
	}
	// Windows itself reports a separator as grayed, so only the rows that are
	// not separators say anything about whether a verb is enabled.
	if len(gotGrayed) != 3 || gotGrayed[0] || !gotGrayed[2] {
		t.Errorf("grayed = %v, want the first row enabled and the last greyed", gotGrayed)
	}
	if want := []bool{false, true, false}; !reflect.DeepEqual(gotSep, want) {
		t.Errorf("separator = %v, want %v", gotSep, want)
	}
	if old != 0 || newest != 1 {
		t.Errorf("pick ran old=%d newest=%d, want the newest closure only", old, newest)
	}

	// A control with no menu leaves the message to the default handling.
	root.specs[0].Menu = nil
	win.syncNative(root)
	gotLabels = nil
	sendMessage(lc.hwnd, wmContextMenu, lc.hwnd, 0x00640064)
	if gotLabels != nil {
		t.Error("a menu was shown for a control without one")
	}

	// Removed from the tree: the picture is freed with the window.
	root.specs = nil
	win.syncNative(root)
	if lc.hbmp != 0 || lc.himl != 0 {
		t.Errorf("GDI objects kept after removal: hbmp %#x himl %#x", lc.hbmp, lc.himl)
	}
}
