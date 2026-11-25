// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package win32

import (
	"syscall"
	"unsafe"

	"github.com/go-widgets/toolkit"
)

// Input-method messages and flags (winuser.h, imm.h).
const (
	wmImeStartComposition = 0x010D
	wmImeEndComposition   = 0x010E
	wmImeComposition      = 0x010F
	wmImeSetContext       = 0x0281

	gcsCompStr   = 0x0008 // GCS_COMPSTR: the composition string
	gcsCompAttr  = 0x0010 // GCS_COMPATTR: one attribute per unit
	gcsCursorPos = 0x0080 // GCS_CURSORPOS: the caret, in units
	gcsResultStr = 0x0800 // GCS_RESULTSTR: the finished text

	iscShowUICompositionWindow = 0x80000000 // ISC_SHOWUICOMPOSITIONWINDOW

	cfsPoint        = 0x0002 // CFS_POINT
	cfsCandidatePos = 0x0040 // CFS_CANDIDATEPOS

	// maxImeBytes bounds one ImmGetCompositionStringW read: a composition is a few
	// dozen characters, and a misbehaving input method must not make this a large
	// allocation.
	maxImeBytes = maxImeUnits * 2 * 8
)

var (
	imm32 = syscall.NewLazyDLL("imm32.dll")

	procImmGetContext            = imm32.NewProc("ImmGetContext")
	procImmReleaseContext        = imm32.NewProc("ImmReleaseContext")
	procImmGetCompositionStringW = imm32.NewProc("ImmGetCompositionStringW")
	procImmSetCandidateWindow    = imm32.NewProc("ImmSetCandidateWindow")
	procImmSetCompositionWindow  = imm32.NewProc("ImmSetCompositionWindow")
)

// candidateForm is CANDIDATEFORM and compositionForm is COMPOSITIONFORM, with the
// POINT and RECT spelled out as int32s.
type candidateForm struct {
	Index, Style uint32
	X, Y         int32
	Area         [4]int32
}

type compositionForm struct {
	Style uint32
	X, Y  int32
	Area  [4]int32
}

// imeComposer returns the focused painted field that composes, if any. Without
// one the input-method messages are left to DefWindowProc, whose default handling
// (the system composition window, then WM_IME_CHAR turned into WM_CHAR) is what
// the window did before it knew about composition.
func (w *Window) imeComposer() (toolkit.Composer, bool) {
	if w.root == nil {
		return nil, false
	}
	c, _, ok := toolkit.FocusedComposition(w.root)
	return c, ok
}

// onImeMessage handles the four input-method messages. It reports handled=false
// when the message belongs to DefWindowProc: no field that composes has the
// focus.
func (w *Window) onImeMessage(message uint32, wParam, lParam uintptr) (ret uintptr, handled bool) {
	c, ok := w.imeComposer()
	if !ok {
		return 0, false
	}
	switch message {
	case wmImeSetContext:
		// The window draws the composition itself, at the caret: tell the system not
		// to show its own composition window. The candidate list stays the IME's.
		lParam &^= iscShowUICompositionWindow
		return uintptr(defWindowProc(w.hwnd, message, wParam, lParam)), true
	case wmImeStartComposition:
		w.placeImeWindows()
		return 0, true
	case wmImeEndComposition:
		c.CancelPreedit()
		w.paintFrame(false)
		return 0, true
	case wmImeComposition:
		w.onImeComposition(c, uint32(lParam))
		return 0, true
	}
	return 0, false
}

// onImeComposition reads what the input method reports and feeds the field: the
// finished text (GCS_RESULTSTR) is committed, and the composition string with its
// caret and attributes becomes the preedit.
func (w *Window) onImeComposition(c toolkit.Composer, flags uint32) {
	himc := immGetContext(w.hwnd)
	if himc == 0 {
		return
	}
	defer immReleaseContext(w.hwnd, himc)

	if flags&gcsResultStr != 0 {
		c.CancelPreedit()
		c.CommitText(commitFrom(bytesToUnits(immString(himc, gcsResultStr))))
	}
	if flags&gcsCompStr != 0 {
		units := bytesToUnits(immString(himc, gcsCompStr))
		cur := int(immLong(himc, gcsCursorPos))
		attrs := immString(himc, gcsCompAttr)
		if p := buildPreedit(units, cur, attrs); p.Text != "" {
			c.SetPreedit(p)
		} else {
			c.CancelPreedit()
		}
	} else if flags&gcsResultStr == 0 {
		c.CancelPreedit() // the composition was cancelled
	}
	w.placeImeWindows()
	w.paintFrame(false)
}

// placeImeWindows tells the input method where the caret is, in client pixels, so
// the candidate window opens under it.
func (w *Window) placeImeWindows() {
	if w.root == nil {
		return
	}
	_, r, ok := toolkit.FocusedComposition(w.root)
	if !ok {
		return
	}
	himc := immGetContext(w.hwnd)
	if himc == 0 {
		return
	}
	defer immReleaseContext(w.hwnd, himc)
	s := w.frameScale()
	x, y := int32(float64(r.X)*s), int32(float64(r.Y)*s)
	bottom := int32(float64(r.Y+r.H) * s)
	cand := candidateForm{Style: cfsCandidatePos, X: x, Y: bottom}
	procImmSetCandidateWindow.Call(himc, uintptr(unsafe.Pointer(&cand)))
	comp := compositionForm{Style: cfsPoint, X: x, Y: y}
	procImmSetCompositionWindow.Call(himc, uintptr(unsafe.Pointer(&comp)))
}

func immGetContext(hwnd uintptr) uintptr {
	h, _, _ := procImmGetContext.Call(hwnd)
	return h
}

func immReleaseContext(hwnd, himc uintptr) { procImmReleaseContext.Call(hwnd, himc) }

// immLong is ImmGetCompositionStringW for an index whose answer is a number (the
// caret).
func immLong(himc uintptr, index uint32) int32 {
	n, _, _ := procImmGetCompositionStringW.Call(himc, uintptr(index), 0, 0)
	return int32(n)
}

// immString reads one composition string or attribute array. The call returns the
// size in bytes (negative on error); the read is bounded by maxImeBytes.
func immString(himc uintptr, index uint32) []byte {
	n := immLong(himc, index)
	if n <= 0 {
		return nil
	}
	if n > maxImeBytes {
		n = maxImeBytes
	}
	buf := make([]byte, n)
	got, _, _ := procImmGetCompositionStringW.Call(himc, uintptr(index), uintptr(unsafe.Pointer(&buf[0])), uintptr(n))
	if g := int32(got); g > 0 && g <= n {
		return buf[:g]
	}
	return nil
}

// bytesToUnits reinterprets a UTF-16LE byte string as units, ignoring a trailing
// odd byte.
func bytesToUnits(b []byte) []uint16 {
	u := make([]uint16, len(b)/2)
	for i := range u {
		u[i] = uint16(b[2*i]) | uint16(b[2*i+1])<<8
	}
	return u
}

func defWindowProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	r, _, _ := user32.NewProc("DefWindowProcW").Call(hwnd, uintptr(message), wParam, lParam)
	return r
}
