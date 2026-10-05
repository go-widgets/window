// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package win32

// Show, Hide and Raise travel to the UI thread as one private message, its
// wParam saying which; what the UI thread then does is decided here, over plain
// values, so it is tested on every GOOS. The calls that carry it out are in
// visibility_windows.go.

// WMAppVisibility is the private message Show, Hide and Raise post. It sits
// next to WMAppRepaint, in the WM_APP range no system message uses.
const WMAppVisibility = WMAppRepaint + 1

// VisibilityOp is the wParam of WMAppVisibility.
type VisibilityOp uintptr

// The three requests.
const (
	OpShow VisibilityOp = iota + 1
	OpHide
	OpRaise
)

// ShowWindow commands (winuser.h).
const (
	SWHide    = 0 // SW_HIDE
	SWShow    = 5 // SW_SHOW
	SWShowNA  = 8 // SW_SHOWNA: show without activating
	SWRestore = 9 // SW_RESTORE
)

// VisibilityPlan is what the UI thread does for op: the ShowWindow command,
// and whether to ask for the foreground afterwards. ok is false for a wParam
// that is none of the three.
//
//   - Show shows without activating (SW_SHOWNA): Show is not a request for the
//     keyboard, and SW_SHOW would take it.
//   - Hide hides, which also takes the window off the taskbar.
//   - Raise restores a minimised window (SW_SHOW leaves it minimised) and shows
//     a hidden one, then asks for the foreground. Windows grants that only to a
//     process that has just had the user's input -- a click on its own
//     notification icon counts -- and otherwise flashes the taskbar button.
func VisibilityPlan(op VisibilityOp, iconic bool) (showCmd int32, foreground, ok bool) {
	switch op {
	case OpShow:
		return SWShowNA, false, true
	case OpHide:
		return SWHide, false, true
	case OpRaise:
		if iconic {
			return SWRestore, true, true
		}
		return SWShow, true, true
	}
	return 0, false, false
}
