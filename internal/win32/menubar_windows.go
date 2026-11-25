// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package win32

import (
	"sync"
	"unsafe"

	"github.com/go-mswin/win32"
	"github.com/go-widgets/tray"
	"golang.org/x/sys/windows"
)

var (
	// CreatePopupMenu, AppendMenuW and DestroyMenu are declared with the native
	// context menus (nativeextras_windows.go).
	procCreateMenu  = user32.NewProc("CreateMenu")
	procSetMenu     = user32.NewProc("SetMenu")
	procGetMenu     = user32.NewProc("GetMenu")
	procDrawMenuBar = user32.NewProc("DrawMenuBar")
)

const wmSysKeyDown = 0x0104

// menuState is the window's menu bar: the plan whose handlers are current, and
// the one built into the HMENU.
type menuState struct {
	mu      sync.Mutex
	plan    BarPlan // current handlers; read by the UI thread
	pending bool
	has     bool
	built   string // signature of what the HMENU holds
}

// SetMenuBar implements the window.MenuBar capability: it installs m as the
// window's menu bar. Safe from any goroutine. The handlers are current at once;
// the HMENU is built on the window's own thread (a menu belongs to the thread of
// its window) and only when the menu's signature changed.
func (w *Window) SetMenuBar(m *tray.Menu) error {
	hwnd := w.hwnd
	if w.gone.Load() || hwnd == 0 {
		return ErrClosed
	}
	plan := PlanBar(m)
	w.menu.mu.Lock()
	w.menu.plan = plan
	w.menu.has = true
	post := !w.menu.pending
	w.menu.pending = true
	w.menu.mu.Unlock()
	if post && !win32.PostMessage(win32.HWND(hwnd), WMAppMenu, 0, 0) {
		return ErrClosed
	}
	return nil
}

// onMenuApply is the UI-thread half: WMAppMenu's handler.
func (w *Window) onMenuApply() {
	w.menu.mu.Lock()
	plan := w.menu.plan
	w.menu.pending = false
	same := plan.Signature == w.menu.built && w.menu.built != ""
	w.menu.mu.Unlock()
	if same {
		return
	}
	w.applyMenu(plan)
}

// applyMenu builds plan into an HMENU and sets it on the window. UI thread only.
func (w *Window) applyMenu(plan BarPlan) {
	bar, _, _ := procCreateMenu.Call()
	appendEntries(bar, plan.Entries)
	old, _, _ := procGetMenu.Call(w.hwnd)
	procSetMenu.Call(w.hwnd, bar)
	if old != 0 {
		procDestroyMenu.Call(old)
	}
	procDrawMenuBar.Call(w.hwnd)
	w.menu.mu.Lock()
	w.menu.built = plan.Signature
	if plan.Signature == "" {
		w.menu.built = "-"
	}
	w.menu.mu.Unlock()
}

func appendEntries(hmenu uintptr, entries []MenuEntry) {
	for _, e := range entries {
		switch {
		case e.Flags&MFSeparator != 0:
			procAppendMenuW.Call(hmenu, MFSeparator, 0, 0)
		case e.Flags&MFPopup != 0:
			sub, _, _ := procCreatePopupMenu.Call()
			appendEntries(sub, e.Sub)
			label, _ := windows.UTF16PtrFromString(e.Label)
			procAppendMenuW.Call(hmenu, uintptr(e.Flags), sub, uintptr(unsafe.Pointer(label)))
		default:
			label, _ := windows.UTF16PtrFromString(e.Label)
			procAppendMenuW.Call(hmenu, uintptr(e.Flags), uintptr(e.ID), uintptr(unsafe.Pointer(label)))
		}
	}
}

// onMenuCommand runs a menu command (WM_COMMAND with lParam 0 and a zero
// notification code). It reports whether the message was a command of ours.
func (w *Window) onMenuCommand(wParam, lParam uintptr) bool {
	if lParam != 0 || hiWord(uint32(wParam)) != 0 {
		return false
	}
	w.menu.mu.Lock()
	plan := w.menu.plan
	w.menu.mu.Unlock()
	return plan.Dispatch(uint16(loWord(uint32(wParam))))
}

// onMenuKey matches a key press against the menu's accelerators and runs the
// command, reporting whether it did (the key is then not given to the widgets).
func (w *Window) onMenuKey(vk uintptr) bool {
	w.menu.mu.Lock()
	plan, has := w.menu.plan, w.menu.has
	w.menu.mu.Unlock()
	if !has || len(plan.Accels) == 0 {
		return false
	}
	var mods AccelMods
	if keyDown(0x11) { // VK_CONTROL
		mods |= AccelCtrl
	}
	if keyDown(0x10) { // VK_SHIFT
		mods |= AccelShift
	}
	if keyDown(0x12) { // VK_MENU
		mods |= AccelAlt
	}
	id := plan.MatchAccel(uint32(vk), mods)
	return id != 0 && plan.Dispatch(id)
}

func keyDown(vk uintptr) bool {
	s, _, _ := procGetKeyState.Call(vk)
	return int16(s) < 0
}
