// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package win32

import (
	"strings"

	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/window/internal/nativemenu"
)

// AppendMenu flags (winuser.h).
const (
	mfString    = 0x0000 // MF_STRING
	mfGrayed    = 0x0001 // MF_GRAYED
	mfSeparator = 0x0800 // MF_SEPARATOR
)

// MenuSlot is one AppendMenuW call: what to append, and the command id a pick
// of it comes back as. Index is the position in the descriptor's list, which is
// what a pick is resolved against.
type MenuSlot struct {
	Flags uint32
	ID    uintptr // 1-based; 0 for a separator
	Text  string
	Index int
}

// MenuPlan lays a descriptor's context menu out as the AppendMenuW calls that
// build it: a separator for an empty label, a greyed entry for a verb with no
// handler, and a plain entry otherwise. Ids are 1-based so TrackPopupMenu's 0
// ("dismissed") is never a command.
func MenuPlan(items []toolkit.NativeMenuItem) []MenuSlot {
	out := make([]MenuSlot, 0, len(items))
	for i, it := range items {
		switch {
		case nativemenu.IsSeparator(it):
			out = append(out, MenuSlot{Flags: mfSeparator, Index: i})
		case !nativemenu.Enabled(it):
			out = append(out, MenuSlot{Flags: mfString | mfGrayed, ID: uintptr(i + 1), Text: menuText(it.Label), Index: i})
		default:
			out = append(out, MenuSlot{Flags: mfString, ID: uintptr(i + 1), Text: menuText(it.Label), Index: i})
		}
	}
	return out
}

// menuText escapes the ampersand, which Win32 reads as the mnemonic marker: a
// label "Save & close" must not lose its ampersand and underline the next letter.
func menuText(label string) string { return strings.ReplaceAll(label, "&", "&&") }

// PickIndex resolves the command id TrackPopupMenu returned to the position in
// the descriptor's list, and false for "dismissed" or an id outside the menu.
func PickIndex(cmd uintptr, n int) (int, bool) {
	if cmd == 0 || cmd > uintptr(n) {
		return 0, false
	}
	return int(cmd) - 1, true
}

// MenuPoint decodes WM_CONTEXTMENU's lParam: the screen position of the click,
// and keyboard true when the menu was asked for with the menu key or Shift+F10
// (lParam -1), in which case the caller positions it by the control instead.
func MenuPoint(lParam uintptr) (x, y int32, keyboard bool) {
	lo, hi := int16(lParam&0xFFFF), int16((lParam>>16)&0xFFFF)
	if lo == -1 && hi == -1 {
		return 0, 0, true
	}
	return int32(lo), int32(hi), false
}
