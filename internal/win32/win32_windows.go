// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

// This file is the Win32/GDI glue that binds the sovereign, OS-independent codec
// (mapping.go) to a live Windows window. It registers a window class and creates
// a real top-level HWND with a titled, resizable frame, presents the toolkit
// RGBA framebuffer into it by packing it BGRA and blitting through StretchDIBits
// over a top-down 32bpp BITMAPINFO in WM_PAINT, and decodes native WM_MOUSEMOVE/
// WM_LBUTTONDOWN/WM_RBUTTONDOWN/WM_*BUTTONUP/WM_MOUSEWHEEL/WM_KEYDOWN/WM_KEYUP/
// WM_CHAR messages through mapping.go into toolkit.Event. The whole path reaches
// the Win32 API through the process' own user32/gdi32/kernel32 DLLs via
// syscall.NewLazyDLL + syscall.SyscallN and a syscall.NewCallback WNDPROC — no
// cgo — so it links with CGO_ENABLED=0.
//
// DPI model. The toolkit lays out and paints in the framebuffer's coordinate
// space, which is kept at the window's LOGICAL point size (DIPs) so the UI
// appears at a readable size. The process declares Per-Monitor-V2 DPI awareness,
// the window's physical client area is sized logical×scale (scale =
// GetDpiForWindow/96), and StretchDIBits up-samples the logical framebuffer to
// fill that physical client area — readable at 150%/200%, exactly 1:1 at 100%.
// This is the same "render logical, let the OS scale" model the Cocoa backend
// adopted to stay legible on Retina, rather than rendering at device pixels and
// presenting into a smaller logical area (which reads as tiny).
//
// Incremental (damage-region) present is honoured: a root that implements
// RenderDamaged has only its damaged rectangles re-packed and invalidated
// (InvalidateRect, in physical client pixels), so WM_PAINT's update region — and
// thus the StretchDIBits blit — touches only the damaged pixels; a plain root
// re-presents the whole surface.
//
// There is one window per process (a native GUI app owns the single main-thread
// message loop), so the HWND and present state are held on the single active
// *Window that the WNDPROC callback consults — the same single-window model the
// Cocoa backend uses.
package win32

import (
	"fmt"
	"sync"
	"syscall"
	"unsafe"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/window/internal/dnd"
)

// Win32 DLLs and the procedures the backend calls, resolved lazily on first use.
var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procRegisterClassExW           = user32.NewProc("RegisterClassExW")
	procCreateWindowExW            = user32.NewProc("CreateWindowExW")
	procDefWindowProcW             = user32.NewProc("DefWindowProcW")
	procGetMessageW                = user32.NewProc("GetMessageW")
	procTranslateMessage           = user32.NewProc("TranslateMessage")
	procDispatchMessageW           = user32.NewProc("DispatchMessageW")
	procPostQuitMessage            = user32.NewProc("PostQuitMessage")
	procDestroyWindow              = user32.NewProc("DestroyWindow")
	procShowWindow                 = user32.NewProc("ShowWindow")
	procUpdateWindow               = user32.NewProc("UpdateWindow")
	procLoadCursorW                = user32.NewProc("LoadCursorW")
	procBeginPaint                 = user32.NewProc("BeginPaint")
	procEndPaint                   = user32.NewProc("EndPaint")
	procInvalidateRect             = user32.NewProc("InvalidateRect")
	procGetClientRect              = user32.NewProc("GetClientRect")
	procSetWindowPos               = user32.NewProc("SetWindowPos")
	procAdjustWindowRectExForDpi   = user32.NewProc("AdjustWindowRectExForDpi")
	procGetDpiForWindow            = user32.NewProc("GetDpiForWindow")
	procSetProcessDpiAwarenessCtx  = user32.NewProc("SetProcessDpiAwarenessContext")
	procSystemParametersInfoForDpi = user32.NewProc("SystemParametersInfoForDpi")
	procMapVirtualKeyW             = user32.NewProc("MapVirtualKeyW")
	procGetKeyState                = user32.NewProc("GetKeyState")

	procStretchDIBits = gdi32.NewProc("StretchDIBits")

	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
)

// Win32 message and style constants (winuser.h).
const (
	wmDestroy     = 0x0002
	wmSize        = 0x0005
	wmClose       = 0x0010
	wmPaint       = 0x000F
	wmEraseBkgnd  = 0x0014
	wmKeyDown     = 0x0100
	wmKeyUp       = 0x0101
	wmChar        = 0x0102
	wmMouseMove   = 0x0200
	wmLButtonDown = 0x0201
	wmLButtonUp   = 0x0202
	wmRButtonDown = 0x0204
	wmRButtonUp   = 0x0205
	wmMouseWheel  = 0x020A
	wmDpiChanged  = 0x02E0

	wsOverlappedWindow = 0x00CF0000                        // WS_OVERLAPPEDWINDOW (titled, resizable, min/max)
	swShow             = 5                                 // SW_SHOW
	cwUseDefault       = ^uintptr(0) &^ (^uintptr(0) >> 1) // 0x80000000 (CW_USEDEFAULT), unused after sizing

	idcArrow = 32512 // IDC_ARROW

	// SetWindowPos flags: don't touch Z-order or activation while (re)sizing.
	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010

	// GDI StretchDIBits: BI_RGB compression, DIB_RGB_COLORS, SRCCOPY raster op.
	biRGB        = 0
	dibRGBColors = 0
	srcCopy      = 0x00CC0020

	// SystemParametersInfoForDpi: SPI_GETWORKAREA returns the usable monitor
	// rectangle (excluding the taskbar) in physical pixels.
	spiGetWorkArea = 0x0030

	// Per-Monitor-V2 DPI awareness context handle (winuser.h): (HANDLE)-4.
	dpiAwarenessPerMonitorV2 = ^uintptr(3) // -4

	mapvkVKToChar = 2 // MAPVK_VK_TO_CHAR

	vkShift   = 0x10
	vkControl = 0x11
	vkMenu    = 0x12 // Alt
	vkLWin    = 0x5B // left  ⊞ Windows/logo key
	vkRWin    = 0x5C // right ⊞ Windows/logo key
)

// rect mirrors Win32 RECT (left, top, right, bottom).
type rect struct{ left, top, right, bottom int32 }

// point mirrors Win32 POINT.
type point struct{ x, y int32 }

// msg mirrors Win32 MSG.
type msg struct {
	hwnd     uintptr
	message  uint32
	wParam   uintptr
	lParam   uintptr
	time     uint32
	pt       point
	lPrivate uint32
}

// paintStruct mirrors Win32 PAINTSTRUCT; only hdc and rcPaint are read.
type paintStruct struct {
	hdc         uintptr
	fErase      int32
	rcPaint     rect
	fRestore    int32
	fIncUpdate  int32
	rgbReserved [32]byte
}

// wndClassExW mirrors Win32 WNDCLASSEXW.
type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

// bitmapInfoHeader mirrors Win32 BITMAPINFOHEADER. A negative biHeight requests
// a top-down DIB (row 0 at the top), matching the toolkit framebuffer.
type bitmapInfoHeader struct {
	biSize          uint32
	biWidth         int32
	biHeight        int32
	biPlanes        uint16
	biBitCount      uint16
	biCompression   uint32
	biSizeImage     uint32
	biXPelsPerMeter int32
	biYPelsPerMeter int32
	biClrUsed       uint32
	biClrImportant  uint32
}

// damageRenderer is the OPT-IN incremental-present capability, declared
// structurally so this backend needs no import of the parent window package
// (which imports this one). Mirrors window.DamageRenderer exactly.
type damageRenderer interface {
	RenderDamaged(p painter.Painter, th *toolkit.Theme) []toolkit.Rect
}

// Window is an open Windows window bound to a go-widgets scene. It owns the
// backing RGBA framebuffer (in LOGICAL points) and a mirrored BGRA DIB buffer,
// presents the DIB to the client area with StretchDIBits and drives the toolkit
// widget tree from WM_* input. It satisfies window.Backend (Run/Close/Size/String).
type Window struct {
	title string
	theme *toolkit.Theme

	hwnd      uintptr
	hInstance uintptr
	className *uint16

	mu   sync.Mutex // guards buf/dib/w/h against the WM_PAINT reader
	buf  []byte     // RGBA framebuffer, 4*w*h bytes (logical points)
	dib  []byte     // BGRA mirror for StretchDIBits, same size
	w, h int        // logical-point framebuffer size

	scale      float64 // physical device pixels per logical point (dpi/96)
	physW      int     // physical client width  (= w*scale)
	physH      int     // physical client height (= h*scale)
	buttonHeld bool

	root   toolkit.Widget
	dmg    damageRenderer
	dnd    *dnd.Controller
	closed bool
}

// active is the single live window the WNDPROC callback routes to. A native GUI
// app owns one message loop and one window here.
var active *Window

// wndProcOnce installs the WNDPROC callback exactly once (syscall.NewCallback
// allocates a non-collectable trampoline, so it must not be created per window).
var (
	wndProcOnce sync.Once
	wndProcCB   uintptr
)

// New declares Per-Monitor-V2 DPI awareness, registers the window class and
// creates a titled, resizable HWND sized to a readable default (or the caller's
// logical Config size), centred in the monitor work area, then shows it. It must
// be called on the goroutine that will run the message loop; the parent Open
// pins that goroutine with runtime.LockOSThread.
func New(title string, width, height int, theme *toolkit.Theme) (*Window, error) {
	// Per-Monitor-V2 DPI awareness: the window is notified of per-monitor DPI and
	// its non-client frame scales, so GetDpiForWindow reports the true monitor
	// DPI and the content stays readable. Best-effort: ignored on the rare build
	// without the context (the render still works at scale 1).
	procSetProcessDpiAwarenessCtx.Call(dpiAwarenessPerMonitorV2)

	if theme == nil {
		theme = toolkit.DefaultDark()
	}

	hInstance, _, _ := procGetModuleHandleW.Call(0)
	className, err := syscall.UTF16PtrFromString("GoWidgetsWindowClass")
	if err != nil {
		return nil, err
	}
	cursor, _, _ := procLoadCursorW.Call(0, uintptr(idcArrow))

	wndProcOnce.Do(func() { wndProcCB = syscall.NewCallback(wndProc) })

	wc := wndClassExW{
		cbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		style:         0x0003, // CS_HREDRAW|CS_VREDRAW: repaint on resize
		lpfnWndProc:   wndProcCB,
		hInstance:     hInstance,
		hCursor:       cursor,
		lpszClassName: className,
	}
	if r, _, e := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		return nil, fmt.Errorf("win32: RegisterClassExW: %w", e)
	}

	w := &Window{
		title:     title,
		theme:     theme,
		hInstance: hInstance,
		className: className,
		scale:     1,
	}
	active = w

	titlePtr, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return nil, err
	}
	// Create the window first (unsized): its DPI is not known until it exists on
	// a monitor. The client is sized to the logical/physical target immediately
	// afterwards via SetWindowPos.
	hwnd, _, e := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(titlePtr)),
		uintptr(wsOverlappedWindow),
		cwUseDefault, cwUseDefault, 640, 480,
		0, 0, hInstance, 0,
	)
	if hwnd == 0 {
		return nil, fmt.Errorf("win32: CreateWindowExW: %w", e)
	}
	w.hwnd = hwnd

	// Now the DPI is known: pick a readable logical size (default from the work
	// area if the caller gave none) and size the physical client to match.
	w.scale = w.windowScale()
	if width <= 0 || height <= 0 {
		wkW, wkH := w.workAreaLogical()
		width, height = DefaultContentSize(wkW, wkH)
	}
	w.applySize(width, height, w.scale)
	w.sizeAndCenter()

	procShowWindow.Call(hwnd, uintptr(swShow))
	procUpdateWindow.Call(hwnd)
	return w, nil
}

// windowScale reads the window's current DPI and converts it to the render scale.
func (w *Window) windowScale() float64 {
	dpi, _, _ := procGetDpiForWindow.Call(w.hwnd)
	return ScaleForDpi(uint32(dpi))
}

// workAreaLogical returns the monitor work area (usable desktop, excluding the
// taskbar) in LOGICAL points at the window's current DPI, for DefaultContentSize.
// On any failure it returns (0,0) so DefaultContentSize falls back to a fixed size.
func (w *Window) workAreaLogical() (float64, float64) {
	dpi, _, _ := procGetDpiForWindow.Call(w.hwnd)
	var r rect
	ok, _, _ := procSystemParametersInfoForDpi.Call(
		uintptr(spiGetWorkArea), 0, uintptr(unsafe.Pointer(&r)), 0, dpi)
	if ok == 0 {
		return 0, 0
	}
	physW := int(r.right - r.left)
	physH := int(r.bottom - r.top)
	scale := ScaleForDpi(uint32(dpi))
	return float64(LogicalFromPhysical(physW, scale)), float64(LogicalFromPhysical(physH, scale))
}

// applySize sets the logical framebuffer size and derived physical client size
// at the given scale, allocating the RGBA framebuffer and its BGRA DIB mirror.
func (w *Window) applySize(logicalW, logicalH int, scale float64) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.w, w.h, w.scale = logicalW, logicalH, scale
	w.physW = PhysicalFromLogical(logicalW, scale)
	w.physH = PhysicalFromLogical(logicalH, scale)
	w.buf = make([]byte, 4*logicalW*logicalH)
	w.dib = make([]byte, 4*logicalW*logicalH)
}

// sizeAndCenter resizes the window so its CLIENT area is exactly physW×physH
// device pixels (AdjustWindowRectExForDpi adds the frame at the current DPI) and
// centres it in the monitor work area.
func (w *Window) sizeAndCenter() {
	dpi, _, _ := procGetDpiForWindow.Call(w.hwnd)
	r := rect{left: 0, top: 0, right: int32(w.physW), bottom: int32(w.physH)}
	procAdjustWindowRectExForDpi.Call(
		uintptr(unsafe.Pointer(&r)), uintptr(wsOverlappedWindow), 0, 0, dpi)
	outerW := int(r.right - r.left)
	outerH := int(r.bottom - r.top)

	// Centre in the work area; fall back to (0,0) origin if it is unavailable.
	x, y := 0, 0
	var wa rect
	if ok, _, _ := procSystemParametersInfoForDpi.Call(
		uintptr(spiGetWorkArea), 0, uintptr(unsafe.Pointer(&wa)), 0, dpi); ok != 0 {
		x = int(wa.left) + CenterOffset(int(wa.right-wa.left), outerW)
		y = int(wa.top) + CenterOffset(int(wa.bottom-wa.top), outerH)
	}
	procSetWindowPos.Call(w.hwnd, 0, uintptr(x), uintptr(y),
		uintptr(outerW), uintptr(outerH), uintptr(swpNoZOrder|swpNoActivate))
}

// Run binds root, seeds the initial full frame and pumps the Win32 message loop
// into the widget tree until the window is closed (WM_DESTROY → WM_QUIT). It is
// the Windows analogue of the X11/Cocoa event loops.
func (w *Window) Run(root toolkit.Widget) error {
	w.root = root
	w.dmg, _ = root.(damageRenderer)
	if w.dnd == nil {
		w.dnd = dnd.New()
	}
	w.dnd.Bind(root)
	// Seed the first frame: paint the whole framebuffer (through the incremental
	// renderer when the root opts in, so its whole-surface seed damage is
	// consumed here) and force an immediate full present.
	if w.dmg != nil {
		w.drawIncremental()
	} else {
		w.draw()
	}
	w.invalidateAll()
	procUpdateWindow.Call(w.hwnd)

	var m msg
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(r) <= 0 { // 0 = WM_QUIT, -1 = error
			return nil
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// wndProc is the window procedure. It routes each message the backend cares about
// through the OS-independent mapping.go and defers everything else to
// DefWindowProcW. It runs on the message-loop thread (LockOSThread'd in Open).
func wndProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	w := active
	if w == nil || w.hwnd != hwnd {
		r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
		return r
	}
	switch message {
	case wmGetObject:
		// Only a request for the UI Automation root is ours; every other object
		// id on this message belongs to MSAA and must be answered by the
		// default handling rather than by silence.
		if ret, ok := w.a11yGetObject(wParam, lParam); ok {
			return ret
		}
		r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
		return r
	case wmMove:
		// Only the UI thread may ask the window where it is; the accessibility
		// bridge reads the answer from a cache.
		w.noteWindowOrigin()
		return 0
	case wmEraseBkgnd:
		return 1 // the framebuffer paints every pixel; skip the flicker-y erase
	case wmPaint:
		w.onPaint()
		return 0
	case wmSize:
		w.onSize(int(loWord(uint32(lParam))), int(hiWord(uint32(lParam))))
		return 0
	case wmDpiChanged:
		w.onDpiChanged(uint32(loWord(uint32(wParam))))
		return 0
	case wmMouseMove:
		x, y := w.clientPoint(lParam)
		w.dispatch(MapMouseMove(x, y, AnyButtonDown(wParam), DecodeMouseMods(wParam)))
		return 0
	case wmLButtonDown, wmRButtonDown:
		w.buttonHeld = true
		x, y := w.clientPoint(lParam)
		w.dispatch(MapMouseDown(x, y, DecodeMouseMods(wParam)))
		return 0
	case wmLButtonUp, wmRButtonUp:
		w.buttonHeld = false
		x, y := w.clientPoint(lParam)
		w.dispatch(MapMouseUp(x, y, DecodeMouseMods(wParam)))
		return 0
	case wmMouseWheel:
		// WM_MOUSEWHEEL delivers SCREEN coordinates; convert to client pixels.
		x, y := w.screenPoint(lParam)
		delta := int(int16(hiWord(uint32(wParam))))
		w.dispatch(MapWheel(x, y, delta, DecodeMouseMods(uintptr(loWord(uint32(wParam))))))
		return 0
	case wmKeyDown:
		w.dispatchAll(MapKeyDown(uint32(wParam), keyMods()))
		return 0
	case wmKeyUp:
		m := keyMods()
		if evs := MapKeyUp(uint32(wParam), m); evs != nil {
			w.dispatchAll(evs)
		} else {
			// A printable key's release: translate the virtual key to its rune.
			ch, _, _ := procMapVirtualKeyW.Call(wParam, uintptr(mapvkVKToChar))
			w.dispatchAll(MapCharUp(rune(uint16(ch)), m))
		}
		return 0
	case wmChar:
		w.dispatchAll(MapCharDown(rune(uint16(wParam)), keyMods()))
		return 0
	case wmClose:
		procDestroyWindow.Call(hwnd)
		return 0
	case wmDestroy:
		w.closed = true
		procPostQuitMessage.Call(0)
		return 0
	default:
		r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(message), wParam, lParam)
		return r
	}
}

// clientPoint extracts the (x,y) of a mouse message's lParam (client pixels,
// top-left origin) and converts it to framebuffer (logical) coordinates.
func (w *Window) clientPoint(lParam uintptr) (int, int) {
	px := int(int16(loWord(uint32(lParam))))
	py := int(int16(hiWord(uint32(lParam))))
	return ClientCoords(px, py, w.scale)
}

// screenPoint converts a WM_MOUSEWHEEL screen-coordinate lParam to framebuffer
// coordinates. Without a live window it degrades to treating the coordinates as
// client pixels (the wheel target only needs to be inside the surface).
func (w *Window) screenPoint(lParam uintptr) (int, int) {
	pt := point{x: int32(int16(loWord(uint32(lParam)))), y: int32(int16(hiWord(uint32(lParam))))}
	// ScreenToClient maps the screen point into client pixels in place.
	if proc := user32.NewProc("ScreenToClient"); proc.Find() == nil {
		proc.Call(w.hwnd, uintptr(unsafe.Pointer(&pt)))
	}
	return ClientCoords(int(pt.x), int(pt.y), w.scale)
}

// keyMods reads the current Shift/Ctrl state for a keyboard message (WM_KEYDOWN/
// WM_KEYUP/WM_CHAR carry no key-state mask, unlike the mouse messages). The high
// bit of GetKeyState marks a held key.
func keyMods() Mods {
	held := func(vk int) bool {
		s, _, _ := procGetKeyState.Call(uintptr(vk))
		return int16(s) < 0
	}
	return Mods{
		Shift: held(vkShift),
		Ctrl:  held(vkControl),
		Alt:   held(vkMenu),
		Meta:  held(vkLWin) || held(vkRWin),
	}
}

// onPaint blits the current DIB into the update region with StretchDIBits,
// up-sampling the logical framebuffer to the physical client area.
func (w *Window) onPaint() {
	var ps paintStruct
	hdc, _, _ := procBeginPaint.Call(w.hwnd, uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	w.mu.Lock()
	bw, bh := w.w, w.h
	physW, physH := w.physW, w.physH
	dib := w.dib
	w.mu.Unlock()
	if len(dib) != 0 && bw > 0 && bh > 0 {
		bmi := bitmapInfoHeader{
			biSize:        uint32(unsafe.Sizeof(bitmapInfoHeader{})),
			biWidth:       int32(bw),
			biHeight:      -int32(bh), // negative → top-down (row 0 at top)
			biPlanes:      1,
			biBitCount:    32,
			biCompression: biRGB,
		}
		// Blit the whole logical framebuffer stretched over the whole physical
		// client. The paint DC is clipped to ps.rcPaint (the InvalidateRect'd
		// damage), so only the damaged pixels are actually written to the screen.
		procStretchDIBits.Call(
			hdc,
			0, 0, uintptr(physW), uintptr(physH), // dest rect (physical client)
			0, 0, uintptr(bw), uintptr(bh), // src rect (logical framebuffer)
			uintptr(unsafe.Pointer(&dib[0])),
			uintptr(unsafe.Pointer(&bmi)),
			uintptr(dibRGBColors), uintptr(srcCopy),
		)
	}
	procEndPaint.Call(w.hwnd, uintptr(unsafe.Pointer(&ps)))
}

// onSize re-derives the logical framebuffer size from the new physical client
// size at the current scale, reallocates and re-presents the whole surface.
func (w *Window) onSize(physW, physH int) {
	if physW <= 0 || physH <= 0 {
		return
	}
	lw := LogicalFromPhysical(physW, w.scale)
	lh := LogicalFromPhysical(physH, w.scale)
	w.mu.Lock()
	same := lw == w.w && lh == w.h
	w.mu.Unlock()
	if same {
		return
	}
	w.applySize(lw, lh, w.scale)
	w.paintFrame(true)
}

// onDpiChanged handles WM_DPICHANGED (the window moved to a monitor of a
// different DPI). It adopts the new render scale while KEEPING the logical
// framebuffer size stable — so the toolkit UI stays the same readable apparent
// size — and grows/shrinks the physical window (and its frame) to match via
// sizeAndCenter, which sizes the client from the logical size at the new DPI.
// The suggested rectangle Windows passes in lParam is intentionally not
// dereferenced (avoiding a uintptr→pointer conversion); sizeAndCenter recomputes
// an equivalent frame with AdjustWindowRectExForDpi.
func (w *Window) onDpiChanged(newDPI uint32) {
	scale := ScaleForDpi(newDPI)
	w.mu.Lock()
	lw, lh := w.w, w.h
	w.mu.Unlock()
	w.applySize(lw, lh, scale) // recomputes physW/physH at the new scale
	w.sizeAndCenter()          // resize the physical window/frame to match
	w.paintFrame(true)
}

// dispatch delivers one toolkit event to the root and re-presents the frame it
// dirtied.
func (w *Window) dispatch(ev toolkit.Event) {
	if w.root != nil {
		for _, dev := range w.dnd.Process(ev) {
			w.root.OnEvent(dev)
		}
	}
	w.paintFrame(false)
}

// dispatchAll delivers a batch of events (the key/char pair) then re-presents once.
func (w *Window) dispatchAll(evs []toolkit.Event) {
	if len(evs) == 0 {
		return
	}
	for _, ev := range evs {
		if w.root != nil {
			w.root.OnEvent(ev)
		}
	}
	w.paintFrame(false)
}

// draw repaints the whole framebuffer: background fill then the root laid out to
// fill the client area, and packs the whole DIB mirror.
func (w *Window) draw() {
	w.mu.Lock()
	defer w.mu.Unlock()
	p := painter.NewPixelPainter(w.buf, w.w, w.h)
	full := toolkit.Rect{X: 0, Y: 0, W: w.w, H: w.h}
	p.FillRect(full, w.theme.Background)
	if w.root != nil {
		w.root.SetBounds(full)
		w.root.Draw(p, w.theme)
	}
	PackBGRA(w.dib, w.buf)
}

// drawIncremental lays the root out to the full client area, repaints ONLY the
// damage the root reports, packs those rectangles into the DIB mirror and
// returns them.
func (w *Window) drawIncremental() []toolkit.Rect {
	w.mu.Lock()
	defer w.mu.Unlock()
	p := painter.NewPixelPainter(w.buf, w.w, w.h)
	w.root.SetBounds(toolkit.Rect{X: 0, Y: 0, W: w.w, H: w.h})
	rects := w.dmg.RenderDamaged(p, w.theme)
	for _, r := range rects {
		PackBGRARect(w.dib, w.buf, w.w, w.h, r.X, r.Y, r.W, r.H)
	}
	return rects
}

// paintFrame renders and presents one frame after an event or resize. A plain
// root repaints+re-presents the whole surface; an incremental root re-packs and
// invalidates only its damaged rectangles — except after a resize, where the
// framebuffer was reallocated (whole-surface damage) and the full surface is
// presented.
func (w *Window) paintFrame(resize bool) {
	// The frame about to be shown and the tree a screen reader reads are
	// published from the same place, so the description can never lag the
	// pixels a sighted user already sees.
	w.refreshA11y()
	if w.dmg == nil {
		w.draw()
		w.invalidateAll()
		return
	}
	rects := w.drawIncremental()
	if resize {
		w.invalidateAll()
		return
	}
	w.invalidateRects(rects)
}

// invalidateAll marks the whole client area for repaint (WM_PAINT full blit).
func (w *Window) invalidateAll() {
	procInvalidateRect.Call(w.hwnd, 0, 0)
}

// invalidateRects marks only the damaged rectangles (converted to physical
// client pixels) for repaint, so WM_PAINT's update region blits just those.
func (w *Window) invalidateRects(rects []toolkit.Rect) {
	if len(rects) == 0 {
		return
	}
	for _, dr := range rects {
		x, y, rw, rh := InvalidRect(dr, w.scale)
		r := rect{left: int32(x), top: int32(y), right: int32(x + rw), bottom: int32(y + rh)}
		procInvalidateRect.Call(w.hwnd, uintptr(unsafe.Pointer(&r)), 0)
	}
}

// Size returns the current client size in framebuffer (logical) pixels — equal
// to the window's logical point size, which the toolkit lays out in.
func (w *Window) Size() (int, int) { return w.w, w.h }

// Close destroys the window. Safe to call more than once.
func (w *Window) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	if w.hwnd != 0 {
		procDestroyWindow.Call(w.hwnd)
		w.hwnd = 0
	}
	if active == w {
		active = nil
	}
	return nil
}

// String identifies the window for debugging.
func (w *Window) String() string {
	return fmt.Sprintf("win32.Window(%dx%d %q)", w.w, w.h, w.title)
}

// loWord/hiWord extract the low/high 16 bits of a 32-bit Win32 packed value.
func loWord(v uint32) uint32 { return v & 0xFFFF }
func hiWord(v uint32) uint32 { return (v >> 16) & 0xFFFF }
