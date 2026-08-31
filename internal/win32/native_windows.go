// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package win32

// This file is the Win32 backing for toolkit's native-control seam, the Windows
// peer of internal/cocoa/native_darwin.go. Each frame the app describes the
// native controls it wants — as flat toolkit.NativeControl descriptors, from a
// Surface's Controls field or from WalkNative over a widget tree — and this
// backend holds the real Win32 child controls, one per Key, parented to the
// framebuffer window, reconciling them to the descriptors.
//
// Where the Cocoa backend lays NSViews over the pixel view, the Windows analogue
// is even simpler: a native control is a CHILD HWND of the window (an "EDIT",
// "BUTTON", "COMBOBOX", …). A child window is composited by the OS above the
// parent's client area — the parent carries WS_CLIPCHILDREN so the StretchDIBits
// blit never paints over it — so there is no hole to punch and no compositing
// trick, just a sub-window that owns its own pixels, focus, caret and selection.
//
// Controls are RECONCILED by Key, not rebuilt: the same descriptor Key finds the
// same live child across frames and only moves or updates it, so a field keeps
// its focus and insertion point. The value binding is immediate-mode-safe by
// construction, exactly as in the Cocoa backend: a descriptor's value is pushed
// into a control ONLY when it differs from what the control last reported, so the
// person's own edits are never fought and only a value the app changed on its own
// is pushed back.
//
// Notifications flow the idiomatic Win32 way: a child sends the PARENT a
// WM_COMMAND (button click, edit change, combo selection) or WM_HSCROLL (the
// trackbar slider); wndProc routes those to onCommand/onHScroll here, which look
// the control up by its HWND and report the new value through the liveControl's
// current app callbacks — the same reporters the Cocoa backend wires to its
// target/action.

import (
	"math"
	"sync"
	"syscall"
	"unsafe"

	"github.com/go-mswin/win32"
	"github.com/go-widgets/toolkit"
)

// Win32 messages, window/control styles and control messages (winuser.h,
// commctrl.h) this backend needs beyond the set win32_windows.go already
// declares. They are plain numeric constants, so they cost nothing on the arches
// that never run them.
const (
	wmCommand = 0x0111 // WM_COMMAND: a child control notifies its parent
	wmHScroll = 0x0114 // WM_HSCROLL: a horizontal trackbar notifies its parent
	wmSetFont = 0x0030 // WM_SETFONT: give a control the GUI font

	// Window styles.
	wsChild        = 0x40000000 // WS_CHILD
	wsVisible      = 0x10000000 // WS_VISIBLE
	wsBorder       = 0x00800000 // WS_BORDER
	wsTabStop      = 0x00010000 // WS_TABSTOP
	wsVScroll      = 0x00200000 // WS_VSCROLL (combo drop-down list scrollbar)
	wsClipChildren = 0x02000000 // WS_CLIPCHILDREN: keep the blit off child HWNDs

	// EDIT styles.
	esAutoHScroll = 0x0080 // ES_AUTOHSCROLL
	esPassword    = 0x0020 // ES_PASSWORD

	// BUTTON styles.
	bsPushButton      = 0x00000000 // BS_PUSHBUTTON
	bsAutoCheckbox    = 0x00000003 // BS_AUTOCHECKBOX
	bsAutoRadioButton = 0x00000009 // BS_AUTORADIOBUTTON

	// STATIC styles.
	ssLeft = 0x00000000 // SS_LEFT

	// COMBOBOX styles.
	cbsDropDownList = 0x0003 // CBS_DROPDOWNLIST
	cbsHasStrings   = 0x0200 // CBS_HASSTRINGS

	// Trackbar (msctls_trackbar32) style.
	tbsHorz = 0x0000 // TBS_HORZ

	// BUTTON check state.
	bmGetCheck   = 0x00F0 // BM_GETCHECK
	bmSetCheck   = 0x00F1 // BM_SETCHECK
	bstUnchecked = 0      // BST_UNCHECKED
	bstChecked   = 1      // BST_CHECKED

	// COMBOBOX messages.
	cbAddString    = 0x0143 // CB_ADDSTRING
	cbSelectString = 0x014D // CB_SELECTSTRING

	// Trackbar messages (WM_USER + n).
	wmUser      = 0x0400
	tbmGetPos   = wmUser + 0 // TBM_GETPOS
	tbmSetPos   = wmUser + 5 // TBM_SETPOS
	tbmSetRange = wmUser + 6 // TBM_SETRANGE

	// ShowWindow commands.
	swHide   = 0 // SW_HIDE
	swShowNA = 8 // SW_SHOWNA: show without stealing activation/focus

	// WM_COMMAND notification codes (HIWORD of wParam).
	bnClicked    = 0      // BN_CLICKED
	enChange     = 0x0300 // EN_CHANGE
	cbnSelChange = 1      // CBN_SELCHANGE

	// GetStockObject index.
	defaultGUIFont = 17 // DEFAULT_GUI_FONT

	// InitCommonControlsEx flag for the trackbar/slider class.
	iccBarClasses = 0x00000004 // ICC_BAR_CLASSES

	// nativeIDBase keeps synthesised control ids clear of the standard button
	// ids (IDOK=1, IDCANCEL=2, …) a dialog manager would use.
	nativeIDBase = 100

	// sliderSteps is the integer resolution the trackbar carries. The toolkit
	// slider is a float over [Min,Max]; the trackbar is an integer control, so
	// the value is mapped onto [0,sliderSteps] and back.
	sliderSteps = 1000
)

// Procedures beyond win32_windows.go's set. SendMessageW and SetWindowTextW push
// values into controls; GetStockObject fetches the GUI font; InitCommonControlsEx
// registers the trackbar class before the first slider is created.
var (
	procSendMessageW   = user32.NewProc("SendMessageW")
	procSetWindowTextW = user32.NewProc("SetWindowTextW")
	procGetStockObject = win32.Gdi32.NewProc("GetStockObject")

	comctl32                 = syscall.NewLazyDLL("comctl32.dll")
	procInitCommonControlsEx = comctl32.NewProc("InitCommonControlsEx")
)

// Control class names, interned once as UTF-16.
var (
	classEDIT     = mustUTF16("EDIT")
	classBUTTON   = mustUTF16("BUTTON")
	classSTATIC   = mustUTF16("STATIC")
	classCOMBOBOX = mustUTF16("COMBOBOX")
	classTrackbar = mustUTF16("msctls_trackbar32")
)

func mustUTF16(s string) *uint16 {
	p, _ := win32.UTF16PtrFromString(s)
	return p
}

// liveControl is one embedded Win32 child control, the app callbacks for the
// frame, and the last value it reported — the baseline the next descriptor is
// compared against.
type liveControl struct {
	hwnd uintptr
	kind toolkit.NativeKind
	id   uint16

	onText     func(string)
	onBool     func(bool)
	onNumber   func(float64)
	onActivate func()

	lastText string
	lastBool bool
	lastNum  float64
	shown    bool

	sliderMin, sliderMax float64 // the trackbar's float range, for pos<->value
}

// nativeControlSource is the optional capability a root exposes to supply native
// controls directly — a self-rendering toolkit.Surface implements it. A root that
// does not is walked as a widget tree instead.
type nativeControlSource interface {
	NativeControls() []toolkit.NativeControl
}

// gatherNative collects this frame's control descriptors: from the root's own
// provider when it has one (a Surface), else by walking it as a widget tree.
func gatherNative(root toolkit.Widget) []toolkit.NativeControl {
	if p, ok := root.(nativeControlSource); ok {
		return p.NativeControls()
	}
	return toolkit.WalkNative(root)
}

// syncNative reconciles the window's embedded Win32 child controls with the
// descriptors for the current frame. It runs after layout, so a control tracks
// its descriptor through scrolling and interaction. With no controls ever present
// it does nothing, leaving the ordinary path untouched.
func (w *Window) syncNative(root toolkit.Widget) {
	specs := gatherNative(root)
	if len(specs) == 0 && w.nativeControls == nil {
		return
	}
	if w.nativeControls == nil {
		w.nativeControls = make(map[string]*liveControl)
		w.nativeByHWND = make(map[uintptr]*liveControl)
	}

	seen := make(map[string]bool, len(specs))
	for _, spec := range specs {
		if spec.Key == "" {
			// A descriptor with no identity cannot be reconciled across frames;
			// a producer must key every control (WalkNative synthesises one).
			continue
		}
		seen[spec.Key] = true
		lc := w.nativeControls[spec.Key]
		if lc == nil {
			lc = w.makeControl(spec)
			if lc == nil {
				continue
			}
			w.nativeControls[spec.Key] = lc
			if spec.OnClaim != nil {
				spec.OnClaim(true)
			}
		}
		w.applySpec(lc, spec)
	}
	for key, lc := range w.nativeControls {
		if !seen[key] {
			w.closeControl(lc)
			delete(w.nativeControls, key)
		}
	}
}

// applySpec updates a live control from this frame's descriptor: refresh the
// callbacks (their closures may differ each frame), push a value only when it
// changed away from what the control last reported, and reposition it.
func (w *Window) applySpec(lc *liveControl, spec toolkit.NativeControl) {
	lc.onText = spec.OnText
	lc.onBool = spec.OnBool
	lc.onNumber = spec.OnNumber
	lc.onActivate = spec.OnActivate

	switch spec.Kind {
	case toolkit.NativeLabel, toolkit.NativeEntry, toolkit.NativeSecureEntry:
		if spec.Text != lc.lastText {
			setWindowText(lc.hwnd, spec.Text)
			lc.lastText = spec.Text
		}
	case toolkit.NativePopUp:
		if spec.Text != lc.lastText {
			comboSelect(lc.hwnd, spec.Text)
			lc.lastText = spec.Text
		}
	case toolkit.NativeCheckbox, toolkit.NativeRadio, toolkit.NativeSwitch:
		if spec.On != lc.lastBool {
			setCheck(lc.hwnd, spec.On)
			lc.lastBool = spec.On
		}
	case toolkit.NativeSlider:
		if spec.Number != lc.lastNum {
			setTrackPos(lc.hwnd, spec.Min, spec.Max, spec.Number)
			lc.lastNum = spec.Number
		}
	}
	// A Button's title is fixed at creation, mirroring the Cocoa backend, so it
	// is not pushed here.

	x, y, cw, ch := w.controlRect(spec.Rect)
	_ = win32.SetWindowPos(win32.HWND(lc.hwnd), 0, x, y, cw, ch, swpNoZOrder|swpNoActivate)

	if spec.Visible != lc.shown {
		cmd := int32(swHide)
		if spec.Visible {
			cmd = swShowNA // never SW_SHOW here: it would steal focus every frame
		}
		procShowWindow.Call(lc.hwnd, uintptr(cmd))
		lc.shown = spec.Visible
	}
}

// makeControl builds the Win32 child control for a descriptor, parents it to the
// framebuffer window, wires it to receive WM_COMMAND/WM_HSCROLL under a unique
// id, and seeds its initial value.
func (w *Window) makeControl(spec toolkit.NativeControl) *liveControl {
	if w.hwnd == 0 {
		return nil
	}
	var (
		class   *uint16
		style   = uint32(wsChild | wsVisible)
		caption *uint16
	)
	switch spec.Kind {
	case toolkit.NativeButton:
		class = classBUTTON
		style |= wsTabStop | bsPushButton
		caption = mustUTF16(spec.Text)
	case toolkit.NativeLabel:
		class = classSTATIC
		style |= ssLeft
		caption = mustUTF16(spec.Text)
	case toolkit.NativeEntry:
		class = classEDIT
		style |= wsBorder | wsTabStop | esAutoHScroll
		caption = mustUTF16(spec.Text)
	case toolkit.NativeSecureEntry:
		class = classEDIT
		style |= wsBorder | wsTabStop | esAutoHScroll | esPassword
		caption = mustUTF16(spec.Text)
	case toolkit.NativeCheckbox, toolkit.NativeSwitch:
		// Win32 has no toggle switch before WinUI; a checkbox is the honest
		// native equivalent of a NativeSwitch.
		class = classBUTTON
		style |= wsTabStop | bsAutoCheckbox
		caption = mustUTF16(spec.Text)
	case toolkit.NativeRadio:
		class = classBUTTON
		style |= wsTabStop | bsAutoRadioButton
		caption = mustUTF16(spec.Text)
	case toolkit.NativeSlider:
		ensureCommonControls()
		class = classTrackbar
		style |= wsTabStop | tbsHorz
	case toolkit.NativePopUp:
		class = classCOMBOBOX
		style |= wsTabStop | wsVScroll | cbsDropDownList | cbsHasStrings
	default:
		return nil
	}

	id := w.nextNativeID()
	x, y, cw, ch := w.controlRect(spec.Rect)
	hwnd, err := win32.CreateWindowEx(
		0, class, caption, style, x, y, cw, ch,
		win32.HWND(w.hwnd), win32.HMENU(uintptr(id)), win32.HINSTANCE(w.hInstance), nil,
	)
	if err != nil || hwnd == 0 {
		return nil
	}
	lc := &liveControl{
		hwnd:      uintptr(hwnd),
		kind:      spec.Kind,
		id:        id,
		lastText:  spec.Text,
		lastBool:  spec.On,
		lastNum:   spec.Number,
		shown:     true, // created WS_VISIBLE
		sliderMin: spec.Min,
		sliderMax: spec.Max,
	}
	setControlFont(lc.hwnd)

	switch spec.Kind {
	case toolkit.NativeCheckbox, toolkit.NativeRadio, toolkit.NativeSwitch:
		setCheck(lc.hwnd, spec.On)
	case toolkit.NativeSlider:
		initTrackbar(lc.hwnd, spec.Min, spec.Max, spec.Number)
	case toolkit.NativePopUp:
		for _, item := range spec.Items {
			comboAdd(lc.hwnd, item)
		}
		if spec.Text != "" {
			comboSelect(lc.hwnd, spec.Text)
		}
	}

	w.nativeByHWND[lc.hwnd] = lc
	return lc
}

// closeControl destroys a control's child window and forgets it. The reconcile
// map entry is deleted by the caller.
func (w *Window) closeControl(lc *liveControl) {
	win32.DestroyWindow(win32.HWND(lc.hwnd))
	delete(w.nativeByHWND, lc.hwnd)
}

// nextNativeID hands out a unique control id (the WM_COMMAND LOWORD identity),
// kept clear of the standard dialog ids.
func (w *Window) nextNativeID() uint16 {
	if w.nativeIDSeq < nativeIDBase {
		w.nativeIDSeq = nativeIDBase
	}
	w.nativeIDSeq++
	return w.nativeIDSeq
}

// controlRect converts a descriptor Rect (framebuffer coordinates, top-left) to
// the physical client pixels a child HWND lives in — the same space the backend
// blits into. In the default (logical framebuffer) model that scales by the DPI;
// in the native (physical framebuffer) model frameScale is 1, so the rect passes
// through unchanged. Extents never fall below one pixel.
func (w *Window) controlRect(r toolkit.Rect) (x, y, cw, ch int32) {
	fs := w.frameScale()
	x = int32(math.Round(float64(r.X) * fs))
	y = int32(math.Round(float64(r.Y) * fs))
	cw = int32(math.Round(float64(r.W) * fs))
	ch = int32(math.Round(float64(r.H) * fs))
	if cw < 1 {
		cw = 1
	}
	if ch < 1 {
		ch = 1
	}
	return x, y, cw, ch
}

// onCommand routes a WM_COMMAND from a child control to its liveControl and
// reports the person's change. It returns false for a command that is not one of
// ours (a menu or accelerator, whose lParam is 0), so wndProc defers it.
func (w *Window) onCommand(wParam, lParam uintptr) bool {
	if lParam == 0 {
		return false
	}
	lc := w.nativeByHWND[lParam]
	if lc == nil {
		return false
	}
	code := hiWord(uint32(wParam))
	switch lc.kind {
	case toolkit.NativeEntry, toolkit.NativeSecureEntry:
		if code == enChange {
			lc.reportText()
		}
	case toolkit.NativeButton:
		if code == bnClicked {
			lc.activate()
		}
	case toolkit.NativeCheckbox, toolkit.NativeRadio, toolkit.NativeSwitch:
		if code == bnClicked {
			lc.reportBool()
			lc.activate()
		}
	case toolkit.NativePopUp:
		if code == cbnSelChange {
			lc.reportText()
			lc.activate()
		}
	default:
		return true
	}
	// The app's model may have changed; repaint so the next frame's descriptor —
	// and any drawn widget bound to the same value — reflects it. The value-diff
	// binding keeps that repaint from pushing the value back into the control.
	w.paintFrame(false)
	return true
}

// onHScroll routes a WM_HSCROLL from a trackbar slider to its liveControl and
// reports the new position. It returns false when the scroll did not come from a
// control we own.
func (w *Window) onHScroll(lParam uintptr) bool {
	if lParam == 0 {
		return false
	}
	lc := w.nativeByHWND[lParam]
	if lc == nil || lc.kind != toolkit.NativeSlider {
		return false
	}
	lc.reportNumber()
	w.paintFrame(false)
	return true
}

// reportText/reportBool/reportNumber record the control's new value as the
// baseline (so the next descriptor comparing equal does not push it back) and
// forward it to the app's current callback.
func (lc *liveControl) reportText() {
	lc.lastText = win32.GetWindowText(win32.HWND(lc.hwnd))
	if lc.onText != nil {
		lc.onText(lc.lastText)
	}
}

func (lc *liveControl) reportBool() {
	lc.lastBool = getCheck(lc.hwnd)
	if lc.onBool != nil {
		lc.onBool(lc.lastBool)
	}
}

func (lc *liveControl) reportNumber() {
	pos := int(sendMessage(lc.hwnd, tbmGetPos, 0, 0))
	lc.lastNum = valFromPos(lc.sliderMin, lc.sliderMax, pos)
	if lc.onNumber != nil {
		lc.onNumber(lc.lastNum)
	}
}

func (lc *liveControl) activate() {
	if lc.onActivate != nil {
		lc.onActivate()
	}
}

// --- thin Win32 helpers ------------------------------------------------------

func sendMessage(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	r, _, _ := procSendMessageW.Call(hwnd, uintptr(msg), wParam, lParam)
	return r
}

func setWindowText(hwnd uintptr, s string) {
	procSetWindowTextW.Call(hwnd, uintptr(unsafe.Pointer(mustUTF16(s))))
}

func setCheck(hwnd uintptr, on bool) {
	state := uintptr(bstUnchecked)
	if on {
		state = bstChecked
	}
	sendMessage(hwnd, bmSetCheck, state, 0)
}

func getCheck(hwnd uintptr) bool {
	return sendMessage(hwnd, bmGetCheck, 0, 0) == bstChecked
}

func comboAdd(hwnd uintptr, item string) {
	sendMessage(hwnd, cbAddString, 0, uintptr(unsafe.Pointer(mustUTF16(item))))
}

func comboSelect(hwnd uintptr, item string) {
	// -1 as the start index searches the whole list from the beginning.
	sendMessage(hwnd, cbSelectString, ^uintptr(0), uintptr(unsafe.Pointer(mustUTF16(item))))
}

// initTrackbar sets the trackbar's integer range to [0,sliderSteps] and its
// initial thumb position from the descriptor's float value.
func initTrackbar(hwnd uintptr, min, max, value float64) {
	sendMessage(hwnd, tbmSetRange, 1, makeLong(0, sliderSteps))
	setTrackPos(hwnd, min, max, value)
}

func setTrackPos(hwnd uintptr, min, max, value float64) {
	sendMessage(hwnd, tbmSetPos, 1, uintptr(posFromVal(min, max, value)))
}

// posFromVal maps a float value in [min,max] onto the integer trackbar range
// [0,sliderSteps], clamped. A degenerate range pins the thumb at 0.
func posFromVal(min, max, value float64) int {
	if max <= min {
		return 0
	}
	p := int(math.Round((value - min) / (max - min) * float64(sliderSteps)))
	if p < 0 {
		return 0
	}
	if p > sliderSteps {
		return sliderSteps
	}
	return p
}

// valFromPos is the inverse of posFromVal: an integer thumb position back to the
// float value the descriptor speaks.
func valFromPos(min, max float64, pos int) float64 {
	if max <= min {
		return min
	}
	return min + float64(pos)/float64(sliderSteps)*(max-min)
}

func makeLong(lo, hi uint16) uintptr {
	return uintptr(uint32(lo) | uint32(hi)<<16)
}

// setControlFont gives a freshly created control the standard GUI font instead
// of the archaic system bitmap font child controls default to.
func setControlFont(hwnd uintptr) {
	if f := guiFont(); f != 0 {
		sendMessage(hwnd, wmSetFont, f, 1)
	}
}

var (
	guiFontOnce sync.Once
	guiFontH    uintptr
)

func guiFont() uintptr {
	guiFontOnce.Do(func() {
		r, _, _ := procGetStockObject.Call(uintptr(defaultGUIFont))
		guiFontH = r
	})
	return guiFontH
}

// initCommonControlsEx mirrors the Win32 INITCOMMONCONTROLSEX structure.
type initCommonControlsExStruct struct {
	dwSize uint32
	dwICC  uint32
}

var commonControlsOnce sync.Once

// ensureCommonControls registers the common-control window classes (the trackbar
// among them) exactly once, before the first slider is created.
func ensureCommonControls() {
	commonControlsOnce.Do(func() {
		icc := initCommonControlsExStruct{
			dwSize: uint32(unsafe.Sizeof(initCommonControlsExStruct{})),
			dwICC:  iccBarClasses,
		}
		procInitCommonControlsEx.Call(uintptr(unsafe.Pointer(&icc)))
	})
}
