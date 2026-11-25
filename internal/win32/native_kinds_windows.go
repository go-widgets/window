// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package win32

// The Win32 hosting of the five native kinds that follow Progress, Spinner and
// Stepper: List, Link, Date, Color and Segmented. Each is one child HWND, so the
// descriptor reconcile in native_windows.go (syncNative / applySpec / onCommand)
// treats them like every other kind; this file holds the constants, the window
// class the colour well needs, and the notification routing (WM_NOTIFY).
//
// Controls, and the documentation each was written from (Microsoft Learn,
// learn.microsoft.com/en-us/windows/win32/):
//
//	List      "LISTBOX"            controls/list-boxes  (LBS_NOTIFY, LB_ADDSTRING,
//	                                LB_SETCURSEL, LB_GETCURSEL, LBN_SELCHANGE)
//	Link      "SysLink"            controls/syslink-control-overview (needs
//	                                comctl32 v6; NM_CLICK / NM_RETURN notify)
//	Date      "SysDateTimePick32"  controls/date-and-time-picker-controls
//	                                (DTM_SETSYSTEMTIME, DTN_DATETIMECHANGE)
//	Color     "GoWidgetsColorWell" a window class of ours that paints the colour
//	                                and opens ChooseColor (dlgbox/choosecolor-str)
//	Segmented "SysTabControl32"    controls/tab-controls (TCS_BUTTONS gives the
//	                                row of mutually exclusive buttons; TCN_SELCHANGE)
//
// A colour well is our own class, not a BUTTON, so that a child-window
// enumeration (and a screen reader) can tell it from a push button.

import (
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"unsafe"

	"github.com/go-mswin/win32"
	"github.com/go-widgets/toolkit"
)

const (
	wmNotify      = 0x004E // WM_NOTIFY
	wmLButtonUpCW = 0x0202 // WM_LBUTTONUP
	wmLButtonDnCW = 0x0201 // WM_LBUTTONDOWN
	wmKeyDownCW   = 0x0100 // WM_KEYDOWN
	wmPaintCW     = 0x000F // WM_PAINT
	wmEraseBkCW   = 0x0014 // WM_ERASEBKGND
	wmDestroyCW   = 0x0002 // WM_DESTROY
	vkSpace       = 0x20

	// LISTBOX.
	lbsNotify     = 0x0001 // LBS_NOTIFY: send LBN_ notifications to the parent
	lbAddString   = 0x0180 // LB_ADDSTRING
	lbSetCurSel   = 0x0186 // LB_SETCURSEL
	lbGetCurSel   = 0x0188 // LB_GETCURSEL
	lbnSelChange  = 1      // LBN_SELCHANGE
	lbsNoIntegral = 0x0100 // LBS_NOINTEGRALHEIGHT: fill the rect, no whole-row snap

	// SysDateTimePick32.
	dtsShortDateCentury = 0x000C // DTS_SHORTDATECENTURYFORMAT
	dtmGetSystemTime    = 0x1001 // DTM_GETSYSTEMTIME
	dtmSetSystemTime    = 0x1002 // DTM_SETSYSTEMTIME
	gdtValid            = 0      // GDT_VALID

	// SysTabControl32.
	tcsButtons   = 0x0100 // TCS_BUTTONS
	tcifText     = 0x0001 // TCIF_TEXT
	tcmGetCurSel = 0x130B // TCM_GETCURSEL
	tcmSetCurSel = 0x130C // TCM_SETCURSEL
	tcmInsertW   = 0x133E // TCM_INSERTITEMW

	// WM_NOTIFY codes. NMHDR.code is a UINT whose values are small NEGATIVE
	// numbers (NM_FIRST counts down from 0), compared here as int32.
	nmClick           = -2
	nmReturn          = -4
	dtnDateTimeChange = -739 // DTN_FIRST (-740) + 1
	tcnSelChange      = -551 // TCN_FIRST (-550) - 1

	// InitCommonControlsEx classes.
	iccTabClasses  = 0x00000008 // ICC_TAB_CLASSES
	iccDateClasses = 0x00000100 // ICC_DATE_CLASSES
	iccLinkClass   = 0x00008000 // ICC_LINK_CLASS

	// ChooseColor.
	ccRGBInit  = 0x00000001 // CC_RGBINIT
	ccFullOpen = 0x00000002 // CC_FULLOPEN
)

var (
	classListBox   = mustUTF16("LISTBOX")
	classSysLink   = mustUTF16("SysLink")
	classDatePick  = mustUTF16("SysDateTimePick32")
	classTab       = mustUTF16("SysTabControl32")
	classColorWell = mustUTF16("GoWidgetsColorWell")

	comdlg32           = syscall.NewLazyDLL("comdlg32.dll")
	procChooseColorW   = comdlg32.NewProc("ChooseColorW")
	procCreateActCtxW  = kernel32.NewProc("CreateActCtxW")
	procActivateActCtx = kernel32.NewProc("ActivateActCtx")
	procDeactivateAct  = kernel32.NewProc("DeactivateActCtx")
	procGetParent      = user32.NewProc("GetParent")
	procFillRect       = user32.NewProc("FillRect")
	procSetFocus       = user32.NewProc("SetFocus")
	procCreateBrush    = win32.Gdi32.NewProc("CreateSolidBrush")
)

// ptrOf turns a pointer-sized integer that Windows handed us (a WM_NOTIFY
// lParam) into a pointer without the vet "possible misuse" the direct
// conversion draws: the value is a live address owned by the sender for the
// duration of the message, not memory the garbage collector tracks.
func ptrOf(u uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&u)) }

// --- structures ---------------------------------------------------------------

type nmHdr struct {
	hwndFrom uintptr
	idFrom   uintptr
	code     uint32
}

type systemTime struct {
	year, month, dayOfWeek, day, hour, minute, second, millis uint16
}

type nmDateTimeChange struct {
	hdr     nmHdr
	dwFlags uint32
	st      systemTime
}

type tcItemW struct {
	mask        uint32
	dwState     uint32
	dwStateMask uint32
	pszText     *uint16
	cchTextMax  int32
	iImage      int32
	lParam      uintptr
}

type chooseColorW struct {
	lStructSize    uint32
	hwndOwner      uintptr
	hInstance      uintptr
	rgbResult      uint32
	lpCustColors   *[16]uint32
	flags          uint32
	lCustData      uintptr
	lpfnHook       uintptr
	lpTemplateName *uint16
}

type actCtxW struct {
	cbSize                 uint32
	dwFlags                uint32
	lpSource               *uint16
	wProcessorArchitecture uint16
	wLangID                uint16
	lpAssemblyDirectory    *uint16
	lpResourceName         *uint16
	lpApplicationName      *uint16
	hModule                uintptr
}

type paintStruct struct {
	hdc        uintptr
	fErase     int32
	rcPaint    [4]int32
	fRestore   int32
	fIncUpdate int32
	reserved   [32]byte
}

// --- SysLink needs comctl32 v6 ------------------------------------------------

// comctl32V6 is the side-by-side manifest that selects the visual-styles common
// controls. SysLink exists only there: an application that ships no manifest of
// its own (most Go programs) gets comctl32 v5, where creating a SysLink simply
// fails. So the class is created under an activation context built from this.
const comctl32V6 = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
<dependency><dependentAssembly>
<assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls" version="6.0.0.0" processorArchitecture="*" publicKeyToken="6595b64144ccf1df" language="*"/>
</dependentAssembly></dependency>
</assembly>
`

var (
	v6Once sync.Once
	v6Ctx  uintptr // an activation context handle, 0 when unavailable
)

// enterLinkV6 activates the comctl32 v6 context on the calling thread and makes
// sure the SysLink class is registered under it. It returns the function that
// leaves the context, or nil when v6 cannot be had (the caller then hosts the
// link as a flat push button, so the kind still works).
func enterLinkV6() func() {
	v6Once.Do(func() {
		dir, err := os.UserCacheDir()
		if err != nil {
			return
		}
		dir = filepath.Join(dir, "go-widgets-window")
		if os.MkdirAll(dir, 0o755) != nil {
			return
		}
		path := filepath.Join(dir, "comctl32-v6.manifest")
		if os.WriteFile(path, []byte(comctl32V6), 0o644) != nil {
			return
		}
		src, err := syscall.UTF16PtrFromString(path)
		if err != nil {
			return
		}
		ctx := actCtxW{cbSize: uint32(unsafe.Sizeof(actCtxW{})), lpSource: src}
		h, _, _ := procCreateActCtxW.Call(uintptr(unsafe.Pointer(&ctx)))
		if h == ^uintptr(0) {
			return
		}
		v6Ctx = h
	})
	if v6Ctx == 0 {
		return nil
	}
	var cookie uintptr
	if r, _, _ := procActivateActCtx.Call(v6Ctx, uintptr(unsafe.Pointer(&cookie))); r == 0 {
		return nil
	}
	icc := initCommonControlsExStruct{
		dwSize: uint32(unsafe.Sizeof(initCommonControlsExStruct{})),
		dwICC:  iccLinkClass,
	}
	procInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))
	return func() { procDeactivateAct.Call(0, cookie) }
}

// linkCaption is the text to push into a link: SysLink markup for a real
// SysLink, the plain title for the push-button fallback.
func linkCaption(sysLink bool, text string) string {
	if sysLink {
		return linkMarkup(text)
	}
	return text
}

// --- list ---------------------------------------------------------------------

func listAdd(hwnd uintptr, item string) {
	sendMessage(hwnd, lbAddString, 0, uintptr(unsafe.Pointer(mustUTF16(item))))
}

// listSelect selects row i, or clears the selection for -1 (LB_SETCURSEL takes
// -1 for "none").
func listSelect(hwnd uintptr, i int) {
	sendMessage(hwnd, lbSetCurSel, uintptr(i), 0)
}

func listSelected(hwnd uintptr) int {
	return int(int32(sendMessage(hwnd, lbGetCurSel, 0, 0)))
}

// --- date ---------------------------------------------------------------------

// dateSet puts an ISO date into the picker; text that is not a date is ignored,
// leaving the picker on the day it shows.
func dateSet(hwnd uintptr, iso string) {
	y, m, d, ok := parseISODate(iso)
	if !ok {
		return
	}
	st := systemTime{year: uint16(y), month: uint16(m), day: uint16(d)}
	sendMessage(hwnd, dtmSetSystemTime, gdtValid, uintptr(unsafe.Pointer(&st)))
}

func dateGet(hwnd uintptr) (string, bool) {
	var st systemTime
	if sendMessage(hwnd, dtmGetSystemTime, 0, uintptr(unsafe.Pointer(&st))) != gdtValid {
		return "", false
	}
	return formatISODate(int(st.year), int(st.month), int(st.day)), true
}

// --- segmented (tab control) ----------------------------------------------------

func tabFill(hwnd uintptr, items []string, selected string) {
	for i, it := range items {
		ti := tcItemW{mask: tcifText, pszText: mustUTF16(it)}
		sendMessage(hwnd, tcmInsertW, uintptr(i), uintptr(unsafe.Pointer(&ti)))
	}
	tabSelect(hwnd, itemIndex(items, selected))
}

func tabSelect(hwnd uintptr, i int) { sendMessage(hwnd, tcmSetCurSel, uintptr(i), 0) }

func tabSelected(hwnd uintptr) int { return int(int32(sendMessage(hwnd, tcmGetCurSel, 0, 0))) }

// --- colour well ----------------------------------------------------------------

var (
	wellMu     sync.Mutex
	wellColors = map[uintptr]uint32{} // hwnd -> COLORREF
	wellCB     uintptr
	wellOnce   sync.Once
	wellCustom [16]uint32
)

func wellColor(hwnd uintptr) uint32 {
	wellMu.Lock()
	defer wellMu.Unlock()
	return wellColors[hwnd]
}

func wellSet(hwnd uintptr, ref uint32) {
	wellMu.Lock()
	wellColors[hwnd] = ref
	wellMu.Unlock()
	procInvalidateRect.Call(hwnd, 0, 1)
}

// registerColorWell registers the well's window class once per process.
func registerColorWell(hInstance uintptr) {
	wellOnce.Do(func() {
		wellCB = win32.NewCallback(colorWellProc)
		wc := win32.WndClassExW{
			CbSize:        uint32(unsafe.Sizeof(win32.WndClassExW{})),
			LpfnWndProc:   wellCB,
			HInstance:     win32.HINSTANCE(hInstance),
			HCursor:       win32.LoadCursor(0, uintptr(idcArrow)),
			LpszClassName: classColorWell,
		}
		_, _ = win32.RegisterClassEx(&wc)
	})
}

// colorWellProc paints the colour as a bordered swatch and, on a click or the
// space/return key, opens the system colour dialog. A chosen colour is reported
// to the parent as a WM_COMMAND with the well as lParam, the same route a button
// uses, so onCommand needs no second mechanism.
func colorWellProc(hwnd, msg, wParam, lParam uintptr) uintptr {
	switch uint32(msg) {
	case wmEraseBkCW:
		return 1
	case wmPaintCW:
		var ps paintStruct
		procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		var rc win32.Rect
		procGetClientRect.Call(hwnd, uintptr(unsafe.Pointer(&rc)))
		brush, _, _ := procCreateBrush.Call(uintptr(wellColor(hwnd)))
		procFillRect.Call(ps.hdc, uintptr(unsafe.Pointer(&rc)), brush)
		win32.DeleteObject(win32.HANDLE(brush))
		procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	case wmLButtonDnCW:
		procSetFocus.Call(hwnd)
		return 0
	case wmLButtonUpCW:
		pickColor(hwnd)
		return 0
	case wmKeyDownCW:
		if wParam == vkSpace || wParam == vkReturn {
			pickColor(hwnd)
			return 0
		}
	case wmDestroyCW:
		wellMu.Lock()
		delete(wellColors, hwnd)
		wellMu.Unlock()
	}
	return uintptr(win32.DefWindowProc(win32.HWND(hwnd), uint32(msg), win32.WPARAM(wParam), win32.LPARAM(lParam)))
}

// pickColor runs ChooseColor starting from the well's colour and, when the
// person confirms, stores and announces the new one.
func pickColor(hwnd uintptr) {
	parent, _, _ := procGetParent.Call(hwnd)
	cc := chooseColorW{
		hwndOwner:    parent,
		rgbResult:    wellColor(hwnd),
		lpCustColors: &wellCustom,
		flags:        ccRGBInit | ccFullOpen,
	}
	cc.lStructSize = uint32(unsafe.Sizeof(cc))
	if ok, _, _ := procChooseColorW.Call(uintptr(unsafe.Pointer(&cc))); ok == 0 {
		return // cancelled
	}
	wellSet(hwnd, cc.rgbResult&0x00FFFFFF)
	sendMessage(parent, wmCommand, 0, hwnd)
}

// --- WM_NOTIFY ----------------------------------------------------------------

// onNotify routes a WM_NOTIFY from a SysLink, date picker or tab control to its
// liveControl. It returns false when the notification is not from a control we
// own, leaving it to default handling.
func (w *Window) onNotify(lParam uintptr) bool {
	if lParam == 0 {
		return false
	}
	h := (*nmHdr)(ptrOf(lParam))
	lc := w.nativeByHWND[h.hwndFrom]
	if lc == nil {
		return false
	}
	switch lc.kind {
	case toolkit.NativeLink:
		if c := int32(h.code); c == nmClick || c == nmReturn {
			lc.activate()
		}
	case toolkit.NativeDate:
		if int32(h.code) == dtnDateTimeChange {
			if iso, ok := dateGet(lc.hwnd); ok && iso != lc.lastText {
				lc.lastText = iso
				if lc.onText != nil {
					lc.onText(iso)
				}
			}
		}
	case toolkit.NativeSegmented:
		if int32(h.code) == tcnSelChange {
			if i := tabSelected(lc.hwnd); i >= 0 && i < len(lc.items) {
				lc.lastText = lc.items[i]
				if lc.onText != nil {
					lc.onText(lc.lastText)
				}
			}
		}
	case toolkit.NativeTable, toolkit.NativeTree, toolkit.NativeTabs:
		handled, repaint := w.collectionNotify(lc, h, lParam)
		if repaint {
			w.paintFrame(false)
		}
		return handled
	default:
		return false
	}
	w.paintFrame(false)
	return true
}
