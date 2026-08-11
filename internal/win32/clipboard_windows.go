// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package win32

import (
	"syscall"
	"unsafe"
)

// The Win32 half of the Clipboard capability.
//
// Windows has an actual clipboard: a system-owned store, not an owner window
// answering questions the way X11 does. Copying hands the system a block of
// memory and stops thinking about it, and text survives the application that
// copied it. That is the whole difference in one sentence, and it is why this
// file is short and the X11 one is not.
//
// The one rule that matters: between OpenClipboard and CloseClipboard nobody
// else can touch it, so every path out of here must close. A function that
// returns early while holding the clipboard freezes copy and paste for the
// entire desktop until the process exits.

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
)

var (
	procOpenClipboard             = user32.NewProc("OpenClipboard")
	procCloseClipboard            = user32.NewProc("CloseClipboard")
	procEmptyClipboard            = user32.NewProc("EmptyClipboard")
	procGetClipboardData          = user32.NewProc("GetClipboardData")
	procSetClipboardData          = user32.NewProc("SetClipboardData")
	procIsClipboardFormatAvailabl = user32.NewProc("IsClipboardFormatAvailable")

	procGlobalAlloc  = kernel32.NewProc("GlobalAlloc")
	procGlobalFree   = kernel32.NewProc("GlobalFree")
	procGlobalLock   = kernel32.NewProc("GlobalLock")
	procGlobalUnlock = kernel32.NewProc("GlobalUnlock")
	procGlobalSize   = kernel32.NewProc("GlobalSize")
	procMoveMemory   = kernel32.NewProc("RtlMoveMemory")
)

// SetClipboardText replaces the clipboard's contents with text.
//
// The memory handed to SetClipboardData becomes the SYSTEM's: freeing it after
// a successful call is a use-after-free that shows up as a corrupted paste in
// some other application, minutes later. It is freed only when the call fails,
// where ownership never transferred.
//
// Implements the window.Clipboard capability.
func (w *Window) SetClipboardText(text string) {
	utf16, err := syscall.UTF16FromString(text)
	if err != nil {
		return // a NUL inside the string; Windows cannot carry it either
	}
	if r, _, _ := procOpenClipboard.Call(w.hwnd); r == 0 {
		return // somebody else holds it; theirs, not ours to take
	}
	defer procCloseClipboard.Call()

	procEmptyClipboard.Call()
	size := uintptr(len(utf16) * 2)
	h, _, _ := procGlobalAlloc.Call(gmemMoveable, size)
	if h == 0 {
		return
	}
	dst, _, _ := procGlobalLock.Call(h)
	if dst == 0 {
		procGlobalFree.Call(h)
		return
	}
	// The block is copied INTO with RtlMoveMemory rather than through a Go
	// slice over it. Turning the locked address into an unsafe.Pointer is what
	// go vet's unsafeptr check objects to, and it is right to: a uintptr is not
	// a reference the collector knows about. Keeping the system's address a
	// uintptr and letting the system do the copy sidesteps the whole question.
	procMoveMemory.Call(dst, uintptr(unsafe.Pointer(&utf16[0])), size)
	procGlobalUnlock.Call(h)

	if r, _, _ := procSetClipboardData.Call(cfUnicodeText, h); r == 0 {
		procGlobalFree.Call(h) // the handover failed, so the block is still ours
	}
}

// ClipboardText returns the clipboard's text, or "" when it holds none.
//
// A clipboard holding an image or a file list is not an error and not empty
// text with a complaint: it is simply nothing this can offer, which is what ""
// says.
//
// Implements the window.Clipboard capability.
func (w *Window) ClipboardText() string {
	if r, _, _ := procIsClipboardFormatAvailabl.Call(cfUnicodeText); r == 0 {
		return ""
	}
	if r, _, _ := procOpenClipboard.Call(w.hwnd); r == 0 {
		return ""
	}
	defer procCloseClipboard.Call()

	h, _, _ := procGetClipboardData.Call(cfUnicodeText)
	if h == 0 {
		return ""
	}
	p, _, _ := procGlobalLock.Call(h)
	if p == 0 {
		return ""
	}
	defer procGlobalUnlock.Call(h)

	size, _, _ := procGlobalSize.Call(h)
	if size < 2 {
		return ""
	}
	// Copied OUT the same way and for the same reason as the write above.
	buf := make([]uint16, size/2)
	procMoveMemory.Call(uintptr(unsafe.Pointer(&buf[0])), p, size)
	return utf16Slice(buf)
}

// utf16Slice decodes up to the first NUL.
//
// GlobalSize is the BLOCK's size, not the string's: it is rounded up by the
// allocator, so decoding all of it appends whatever was left in the tail.
func utf16Slice(u []uint16) string {
	for i, c := range u {
		if c == 0 {
			return syscall.UTF16ToString(u[:i])
		}
	}
	return syscall.UTF16ToString(u)
}
