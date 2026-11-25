// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows && integration

package win32

import (
	"runtime"
	"syscall"
	"testing"
	"unsafe"

	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/tray"
)

// GetMenuItemCount, GetMenuStringW and mfByPosition come from
// nativeextras_live_windows_test.go.
var procGetSubMenu = user32.NewProc("GetSubMenu")

const wmCommandMsg = 0x0111

func menuString(hmenu uintptr, pos int) string {
	buf := make([]uint16, 256)
	n, _, _ := procGetMenuStringW.Call(hmenu, uintptr(pos), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)), mfByPosition)
	return syscall.UTF16ToString(buf[:n])
}

// TestLiveWin32MenuBar is the CI proof that the menu bar is a REAL Win32 menu:
// GetMenu finds it on the window, GetMenuItemCount and GetMenuString read its
// items back, and a WM_COMMAND for an item runs its handler.
func TestLiveWin32MenuBar(t *testing.T) {
	skipUnlessLive(t)
	runtime.LockOSThread()
	t.Cleanup(runtime.UnlockOSThread)

	win, err := New("go-widgets/window win32 menu bar", 320, 200, toolkit.DefaultDark())
	if err != nil {
		t.Fatalf("window: %v", err)
	}
	t.Cleanup(func() { win.Close() })

	saved, flagged := 0, false
	m := tray.NewMenu().Add(
		tray.SubMenu("File", tray.NewMenu().Add(
			tray.KeyItem("Save", nil, "s", tray.ModCommand, func() { saved++ }),
			tray.Separator(),
			tray.Checkbox("Flag", false, func(on bool) { flagged = on }),
		)),
		tray.EditMenu(),
	)
	if err := win.SetMenuBar(m); err != nil {
		t.Fatalf("SetMenuBar: %v", err)
	}
	// SetMenuBar posted the build to the window's thread, which is this one; run
	// the handler the message loop would.
	win.onMenuApply()

	bar, _, _ := procGetMenu.Call(win.hwnd)
	if bar == 0 {
		t.Fatal("GetMenu found no menu on the window")
	}
	if n, _, _ := procGetMenuItemCount.Call(bar); n != 2 {
		t.Fatalf("GetMenuItemCount(bar) = %d, want 2 (File, Edit)", n)
	}
	if got := []string{menuString(bar, 0), menuString(bar, 1)}; got[0] != "File" || got[1] != "Edit" {
		t.Fatalf("bar items = %v, want [File Edit]", got)
	}
	file, _, _ := procGetSubMenu.Call(bar, 0)
	if n, _, _ := procGetMenuItemCount.Call(file); n != 3 {
		t.Fatalf("GetMenuItemCount(File) = %d, want 3", n)
	}
	if got := menuString(file, 0); got != "Save\tCtrl+S" {
		t.Errorf("File > Save reads %q, want \"Save\\tCtrl+S\"", got)
	}
	edit, _, _ := procGetSubMenu.Call(bar, 1)
	if n, _, _ := procGetMenuItemCount.Call(edit); n != 7 {
		t.Errorf("GetMenuItemCount(Edit) = %d, want 7", n)
	}
	if got := menuString(edit, 4); got != "Copy\tCtrl+C" {
		t.Errorf("Edit > Copy reads %q", got)
	}

	// A click: WM_COMMAND with the item's id, sent the way the menu loop does.
	procSendMessageW.Call(win.hwnd, wmCommandMsg, 1, 0)
	if saved != 1 {
		t.Errorf("WM_COMMAND for Save ran its handler %d times, want 1", saved)
	}
	procSendMessageW.Call(win.hwnd, wmCommandMsg, 2, 0)
	if !flagged {
		t.Error("WM_COMMAND for the Flag checkbox did not toggle it")
	}

	// The same menu again must not rebuild the HMENU; a changed one must.
	// Toggling the checkbox changed the menu (a checkmark is part of its
	// signature), so settle on the current one before comparing.
	if err := win.SetMenuBar(m); err != nil {
		t.Fatal(err)
	}
	win.onMenuApply()
	bar, _, _ = procGetMenu.Call(win.hwnd)
	if err := win.SetMenuBar(m); err != nil {
		t.Fatal(err)
	}
	win.onMenuApply()
	if again, _, _ := procGetMenu.Call(win.hwnd); again != bar {
		t.Error("an unchanged menu was built again")
	}
	m.Items[0].Submenu.Items[0].Label = "Save As"
	if err := win.SetMenuBar(m); err != nil {
		t.Fatal(err)
	}
	win.onMenuApply()
	bar2, _, _ := procGetMenu.Call(win.hwnd)
	file2, _, _ := procGetSubMenu.Call(bar2, 0)
	if got := menuString(file2, 0); got != "Save As\tCtrl+S" {
		t.Errorf("after the change File > Save reads %q", got)
	}
}
