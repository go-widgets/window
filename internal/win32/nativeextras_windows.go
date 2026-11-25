// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package win32

// The context menu and the picture of a native control, on Win32.
//
// A menu is built when a menu is ASKED FOR (WM_CONTEXTMENU) and destroyed when
// it is dismissed, so a frame never touches an HMENU: the application rebuilds
// its descriptors sixty times a second, and replacing a menu that is open under
// the pointer would let somebody click a verb that is not the one they aimed at.
// A pick runs the NEWEST closure for the entry that was chosen.
//
// A picture is decoded, bounded, scaled to the button and composited over the
// button face in Go, then handed to the control as a DIB section, only when the
// PNG or ImageOnly changed.

import (
	"bytes"
	"unsafe"

	"github.com/go-mswin/win32"
	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/window/internal/nativeimg"
)

const (
	wmContextMenu = 0x007B // WM_CONTEXTMENU

	tpmRightButton = 0x0002 // TPM_RIGHTBUTTON
	tpmNoNotify    = 0x0080 // TPM_NONOTIFY
	tpmReturnCmd   = 0x0100 // TPM_RETURNCMD

	bsBitmap        = 0x0080 // BS_BITMAP
	bsTypeMask      = 0x000F // BS_TYPEMASK
	bmSetImage      = 0x00F7 // BM_SETIMAGE
	imageTypeBitmap = 0      // IMAGE_BITMAP
	bcmSetImgLst    = 0x1602 // BCM_SETIMAGELIST (comctl32 v6)
	bcmGetImgLst    = 0x1603 // BCM_GETIMAGELIST

	colorBtnFace = 15     // COLOR_BTNFACE
	ilcColor32   = 0x0020 // ILC_COLOR32
	wmNull       = 0x0000
)

var (
	procCreatePopupMenu    = user32.NewProc("CreatePopupMenu")
	procAppendMenuW        = user32.NewProc("AppendMenuW")
	procTrackPopupMenu     = user32.NewProc("TrackPopupMenu")
	procDestroyMenu        = user32.NewProc("DestroyMenu")
	procGetWindowRect      = user32.NewProc("GetWindowRect")
	procSetForegroundWnd   = user32.NewProc("SetForegroundWindow")
	procPostMessageW       = user32.NewProc("PostMessageW")
	procGetSysColor        = user32.NewProc("GetSysColor")
	procGetDC              = user32.NewProc("GetDC")
	procReleaseDC          = user32.NewProc("ReleaseDC")
	procGetWindowLongPtrW  = user32.NewProc("GetWindowLongPtrW")
	procSetWindowLongPtrW  = user32.NewProc("SetWindowLongPtrW")
	procCreateDIBSection   = win32.Gdi32.NewProc("CreateDIBSection")
	procImageListCreate    = comctl32.NewProc("ImageList_Create")
	procImageListAdd       = comctl32.NewProc("ImageList_Add")
	procImageListDestroy   = comctl32.NewProc("ImageList_Destroy")
	procGdiDeleteObjectRaw = win32.Gdi32.NewProc("DeleteObject")
)

// gwlStyleIdx is GWL_STYLE (-16) as the unsigned index GetWindowLongPtrW takes.
var gwlStyleIdx = ^uintptr(15)

// trackPopupMenu shows the menu and returns the command picked (0: dismissed).
// It is a variable so a test on a real window can look at the real HMENU the
// way a person would, and answer for them, without a modal loop to drive.
var trackPopupMenu = func(owner, hmenu uintptr, x, y int32) uintptr {
	// The documented dance: the owner must be foreground or the menu will not
	// dismiss when the user clicks elsewhere, and a WM_NULL afterwards lets the
	// menu loop finish (KB135788).
	procSetForegroundWnd.Call(owner)
	r, _, _ := procTrackPopupMenu.Call(hmenu, tpmReturnCmd|tpmRightButton|tpmNoNotify,
		uintptr(int64(x)), uintptr(int64(y)), 0, owner, 0)
	procPostMessageW.Call(owner, wmNull, 0, 0)
	return r
}

// nativeExtras is what a control carries beyond its value.
type nativeExtras struct {
	menuItems []toolkit.NativeMenuItem // the NEWEST items, for the next WM_CONTEXTMENU

	lastImage []byte
	lastOnly  bool
	hbmp      uintptr // the DIB section on the button; ours to delete
	himl      uintptr // the image list holding it (picture beside the caption); ours to destroy
	imageMode imageMode
}

type imageMode int

const (
	imageNone     imageMode = iota
	imageBitmap1            // BS_BITMAP + BM_SETIMAGE: the picture alone
	imageBesideTx           // BCM_SETIMAGELIST: the picture and the caption (comctl32 v6)
)

// applyMenu keeps the descriptor's menu for the next time one is asked for.
func (lc *liveControl) applyMenu(items []toolkit.NativeMenuItem) { lc.menuItems = items }

// onContextMenu answers WM_CONTEXTMENU for a hosted control: it shows the
// control's menu and runs the pick. It reports false when the control has no
// menu, so the message takes its default course (an edit's own Cut/Copy menu).
func (w *Window) onContextMenu(lc *liveControl, lParam uintptr) bool {
	if lc == nil || len(lc.menuItems) == 0 {
		return false
	}
	x, y, keyboard := MenuPoint(lParam)
	if keyboard {
		// The menu key and Shift+F10 carry no position: open under the control.
		var rc [4]int32
		procGetWindowRect.Call(lc.hwnd, uintptr(unsafe.Pointer(&rc)))
		x, y = rc[0], rc[3]
	}
	items := lc.menuItems
	hmenu, _, _ := procCreatePopupMenu.Call()
	if hmenu == 0 {
		return false
	}
	defer procDestroyMenu.Call(hmenu)
	for _, s := range MenuPlan(items) {
		text := uintptr(0)
		if s.Flags&mfSeparator == 0 {
			text = uintptr(unsafe.Pointer(mustUTF16(s.Text)))
		}
		procAppendMenuW.Call(hmenu, uintptr(s.Flags), s.ID, text)
	}
	cmd := trackPopupMenu(uintptr(w.hwnd), hmenu, x, y)
	if i, ok := PickIndex(cmd, len(items)); ok {
		// The newest closure for the entry that was chosen; the application may
		// have rebuilt its descriptors while the menu was up.
		if cur := lc.menuItems; i < len(cur) && cur[i].Pick != nil {
			cur[i].Pick()
		}
	}
	return true
}

// applyImage puts the descriptor's PNG on a button when it changed.
func (w *Window) applyImage(lc *liveControl, spec toolkit.NativeControl) {
	if lc.kind != toolkit.NativeButton {
		return
	}
	if bytes.Equal(spec.Image, lc.lastImage) && spec.ImageOnly == lc.lastOnly {
		return
	}
	lc.lastImage, lc.lastOnly = bytes.Clone(spec.Image), spec.ImageOnly
	if len(spec.Image) == 0 {
		lc.dropImage()
		return
	}
	img, err := nativeimg.Decode(spec.Image)
	if err != nil {
		return // keep what the button shows: an unreadable icon blanks nothing
	}
	_, _, cw, ch := w.controlRect(spec.Rect)
	face, _, _ := procGetSysColor.Call(colorBtnFace)
	fw, fh, bgra := painter.IconBGRAOver(img.RGBA, img.W, img.H, int(cw), int(ch), nativeimg.Pad,
		painter.RGB(uint8(face), uint8(face>>8), uint8(face>>16)))
	hbmp := dibSection(fw, fh, bgra)
	if hbmp == 0 {
		return
	}
	lc.dropImage()
	lc.hbmp = hbmp
	if !spec.ImageOnly && lc.setImageList(fw, fh) {
		lc.imageMode = imageBesideTx
		return
	}
	// The picture alone: BS_BITMAP replaces the caption on screen (it stays the
	// window text, which is the accessible name).
	style, _, _ := procGetWindowLongPtrW.Call(lc.hwnd, gwlStyleIdx)
	procSetWindowLongPtrW.Call(lc.hwnd, gwlStyleIdx, style&^(bsTypeMask|bsBitmap)|bsBitmap)
	sendMessage(lc.hwnd, bmSetImage, imageTypeBitmap, hbmp)
	lc.imageMode = imageBitmap1
	procInvalidateRect.Call(lc.hwnd, 0, 1)
}

// buttonImageList is BUTTON_IMAGELIST (commctrl.h).
type buttonImageList struct {
	himl                   uintptr
	left, top, right, down int32
	align                  uint32
}

// setImageList asks the button to draw the picture beside its caption. Only
// comctl32 v6 knows the message: an application with no manifest gets v5, where
// it does nothing and answers 0, and the caller falls back to the bare picture.
func (lc *liveControl) setImageList(w, h int) bool {
	himl, _, _ := procImageListCreate.Call(uintptr(w), uintptr(h), ilcColor32, 1, 1)
	if himl == 0 {
		return false
	}
	if idx, _, _ := procImageListAdd.Call(himl, lc.hbmp, 0); int32(idx) < 0 {
		procImageListDestroy.Call(himl)
		return false
	}
	bil := buttonImageList{himl: himl, left: 4, right: 4}
	if r := sendMessage(lc.hwnd, bcmSetImgLst, 0, uintptr(unsafe.Pointer(&bil))); r == 0 {
		procImageListDestroy.Call(himl)
		return false
	}
	lc.himl = himl
	return true
}

// dropImage takes the picture off the button and frees what held it.
func (lc *liveControl) dropImage() {
	switch lc.imageMode {
	case imageBitmap1:
		sendMessage(lc.hwnd, bmSetImage, imageTypeBitmap, 0)
		style, _, _ := procGetWindowLongPtrW.Call(lc.hwnd, gwlStyleIdx)
		procSetWindowLongPtrW.Call(lc.hwnd, gwlStyleIdx, style&^(bsTypeMask|bsBitmap))
		procInvalidateRect.Call(lc.hwnd, 0, 1)
	case imageBesideTx:
		bil := buttonImageList{} // himl 0: no list
		sendMessage(lc.hwnd, bcmSetImgLst, 0, uintptr(unsafe.Pointer(&bil)))
	}
	lc.imageMode = imageNone
	lc.freeImage()
}

// freeImage deletes the GDI objects behind the picture. Only after the control
// stopped using them: replaced, or its window destroyed.
func (lc *liveControl) freeImage() {
	if lc.himl != 0 {
		procImageListDestroy.Call(lc.himl)
		lc.himl = 0
	}
	if lc.hbmp != 0 {
		procGdiDeleteObjectRaw.Call(lc.hbmp)
		lc.hbmp = 0
	}
}

// bitmapInfoHeader is BITMAPINFOHEADER.
type bitmapInfoHeader struct {
	size          uint32
	width, height int32
	planes, bits  uint16
	compression   uint32
	sizeImage     uint32
	xppm, yppm    int32
	clrUsed       uint32
	clrImportant  uint32
}

// dibSection makes a top-down 32-bit bitmap from w*h BGRA pixels, or 0.
func dibSection(w, h int, bgra []byte) uintptr {
	if w <= 0 || h <= 0 || len(bgra) != w*h*4 {
		return 0
	}
	hdr := bitmapInfoHeader{size: uint32(unsafe.Sizeof(bitmapInfoHeader{})), width: int32(w), height: -int32(h), planes: 1, bits: 32}
	hdc, _, _ := procGetDC.Call(0)
	var bits unsafe.Pointer
	hbmp, _, _ := procCreateDIBSection.Call(hdc, uintptr(unsafe.Pointer(&hdr)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	procReleaseDC.Call(0, hdc)
	if hbmp == 0 || bits == nil {
		return 0
	}
	copy(unsafe.Slice((*byte)(bits), len(bgra)), bgra)
	return hbmp
}
