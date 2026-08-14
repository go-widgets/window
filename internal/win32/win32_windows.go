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
// the Win32 API through github.com/go-mswin/win32 (the shared, pure-Go CGO=0
// bindings on golang.org/x/sys/windows) — its lazy DLL handles, the
// class/window/pump procedures, the WNDCLASSEXW/MSG/RECT/POINT types and the
// BGRA StretchDIBits blit — plus a win32.NewCallback WNDPROC, so it links with
// CGO_ENABLED=0.
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
	"unsafe"

	"github.com/go-mswin/win32"
	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/window/internal/dnd"
)

// The shared Win32 surface — the user32/gdi32/kernel32 lazy DLLs, the nine
// window/class/pump procedures (RegisterClassExW, CreateWindowExW,
// DefWindowProcW, GetMessageW, TranslateMessage, DispatchMessageW,
// PostQuitMessage, LoadCursorW, GetModuleHandleW), the WNDCLASSEXW/MSG/RECT/POINT
// types and the top-down BGRA StretchDIBits blit — now comes from
// github.com/go-mswin/win32 (the Windows peer of go-macos/objc) instead of being
// hand-rolled here. user32 and kernel32 alias win32's shared lazy DLL handles so
// the accessibility and clipboard siblings in this package keep binding their
// own procedures off them unchanged.
var (
	user32   = win32.User32
	kernel32 = win32.Kernel32

	// Window-specific procedures, bound off the shared user32 handle.
	procShowWindow                 = user32.NewProc("ShowWindow")
	procUpdateWindow               = user32.NewProc("UpdateWindow")
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
)

// kernel32 aliases win32.Kernel32 for clipboard_windows.go in this package.
var _ = kernel32

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

	wsOverlappedWindow = 0x00CF0000  // WS_OVERLAPPEDWINDOW (titled, resizable, min/max)
	swShow             = 5           // SW_SHOW
	cwUseDefault       = -0x80000000 // CW_USEDEFAULT ((int)0x80000000); the window is repositioned by sizeAndCenter regardless

	idcArrow = 32512 // IDC_ARROW

	// SetWindowPos flags: don't touch Z-order or activation while (re)sizing.
	swpNoZOrder   = 0x0004
	swpNoActivate = 0x0010

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

// The RECT/POINT/MSG/WNDCLASSEXW/PAINTSTRUCT/BITMAPINFOHEADER structs this
// backend used to declare now come from github.com/go-mswin/win32 (win32.Rect,
// win32.Point, win32.WndClassExW, win32.PaintStruct); the message loop uses
// win32.Pump and the blit uses win32.StretchDIBitsBGRA, which builds the DIB
// header internally.

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

	// The Repainter capability: at most one posted wakeup in flight. See
	// repaint.go for why the flag is ours and not the message queue's.
	repaint repaintFlag
}

// Repaint asks the message loop for a frame. Implements the window.Repainter
// capability; safe to call from any goroutine, returns without waiting.
//
// PostMessage is the one sanctioned way into another thread's message queue,
// and it is why this needs no lock: the pump picks the message up on the UI
// thread, which is the only thread allowed to touch a window.
func (w *Window) Repaint() {
	if w.hwnd == 0 || !w.repaint.arm() {
		return // no window yet, or a wakeup is already on its way
	}
	if !win32.PostMessage(win32.HWND(w.hwnd), WMAppRepaint, 0, 0) {
		// The queue is gone, which means the window is going away and the pump
		// is about to stop. A flag left armed would silence a later Repaint if
		// it were not.
		w.repaint.disarm()
	}
}

// active is the single live window the WNDPROC callback routes to. A native GUI
// app owns one message loop and one window here.
var active *Window

// wndProcOnce installs the WNDPROC callback exactly once (win32.NewCallback
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

	hInstance := uintptr(win32.GetModuleHandle(nil))
	className, err := win32.UTF16PtrFromString("GoWidgetsWindowClass")
	if err != nil {
		return nil, err
	}
	cursor := win32.LoadCursor(0, uintptr(idcArrow))

	wndProcOnce.Do(func() { wndProcCB = win32.NewCallback(wndProc) })

	wc := win32.WndClassExW{
		CbSize:        uint32(unsafe.Sizeof(win32.WndClassExW{})),
		Style:         0x0003, // CS_HREDRAW|CS_VREDRAW: repaint on resize
		LpfnWndProc:   wndProcCB,
		HInstance:     win32.HINSTANCE(hInstance),
		HCursor:       cursor,
		LpszClassName: className,
	}
	if _, err := win32.RegisterClassEx(&wc); err != nil {
		return nil, err
	}

	w := &Window{
		title:     title,
		theme:     theme,
		hInstance: hInstance,
		className: className,
		scale:     1,
	}
	active = w

	titlePtr, err := win32.UTF16PtrFromString(title)
	if err != nil {
		return nil, err
	}
	// Create the window first (unsized): its DPI is not known until it exists on
	// a monitor. The client is sized to the logical/physical target immediately
	// afterwards via SetWindowPos.
	hwnd, err := win32.CreateWindowEx(
		0, className, titlePtr, uint32(wsOverlappedWindow),
		int32(cwUseDefault), int32(cwUseDefault), 640, 480,
		0, 0, win32.HINSTANCE(hInstance), nil,
	)
	if err != nil {
		return nil, err
	}
	w.hwnd = uintptr(hwnd)

	// Now the DPI is known: pick a readable logical size (default from the work
	// area if the caller gave none) and size the physical client to match.
	w.scale = w.windowScale()
	if width <= 0 || height <= 0 {
		wkW, wkH := w.workAreaLogical()
		width, height = DefaultContentSize(wkW, wkH)
	}
	w.applySize(width, height, w.scale)
	w.sizeAndCenter()

	procShowWindow.Call(w.hwnd, uintptr(swShow))
	procUpdateWindow.Call(w.hwnd)
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
	var r win32.Rect
	ok, _, _ := procSystemParametersInfoForDpi.Call(
		uintptr(spiGetWorkArea), 0, uintptr(unsafe.Pointer(&r)), 0, dpi)
	if ok == 0 {
		return 0, 0
	}
	physW := int(r.Right - r.Left)
	physH := int(r.Bottom - r.Top)
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
	r := win32.Rect{Left: 0, Top: 0, Right: int32(w.physW), Bottom: int32(w.physH)}
	procAdjustWindowRectExForDpi.Call(
		uintptr(unsafe.Pointer(&r)), uintptr(wsOverlappedWindow), 0, 0, dpi)
	outerW := int(r.Right - r.Left)
	outerH := int(r.Bottom - r.Top)

	// Centre in the work area; fall back to (0,0) origin if it is unavailable.
	x, y := 0, 0
	var wa win32.Rect
	if ok, _, _ := procSystemParametersInfoForDpi.Call(
		uintptr(spiGetWorkArea), 0, uintptr(unsafe.Pointer(&wa)), 0, dpi); ok != 0 {
		x = int(wa.Left) + CenterOffset(int(wa.Right-wa.Left), outerW)
		y = int(wa.Top) + CenterOffset(int(wa.Bottom-wa.Top), outerH)
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

	// The GetMessage/TranslateMessage/DispatchMessage loop lives in win32.Pump;
	// it returns nil on WM_QUIT (WM_DESTROY -> PostQuitMessage).
	return win32.Pump()
}

// wndProc is the window procedure. It routes each message the backend cares about
// through the OS-independent mapping.go and defers everything else to
// DefWindowProcW. It runs on the message-loop thread (LockOSThread'd in Open).
func wndProc(hwnd uintptr, message uint32, wParam, lParam uintptr) uintptr {
	w := active
	if w == nil || w.hwnd != hwnd {
		return uintptr(win32.DefWindowProc(win32.HWND(hwnd), message, win32.WPARAM(wParam), win32.LPARAM(lParam)))
	}
	switch message {
	case wmGetObject:
		// Only a request for the UI Automation root is ours; every other object
		// id on this message belongs to MSAA and must be answered by the
		// default handling rather than by silence.
		if ret, ok := w.a11yGetObject(wParam, lParam); ok {
			return ret
		}
		return uintptr(win32.DefWindowProc(win32.HWND(hwnd), message, win32.WPARAM(wParam), win32.LPARAM(lParam)))
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
	case WMAppRepaint:
		// Somebody outside the message loop asked for a frame. It carries no
		// input and reaches no widget: the whole content of the message is that
		// the tree may have changed under us.
		w.repaint.take()
		w.paintFrame(false)
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
		win32.DestroyWindow(win32.HWND(hwnd))
		return 0
	case wmDestroy:
		w.closed = true
		win32.PostQuitMessage(0)
		return 0
	default:
		return uintptr(win32.DefWindowProc(win32.HWND(hwnd), message, win32.WPARAM(wParam), win32.LPARAM(lParam)))
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
	pt := win32.Point{X: int32(int16(loWord(uint32(lParam)))), Y: int32(int16(hiWord(uint32(lParam))))}
	// ScreenToClient maps the screen point into client pixels in place.
	if proc := user32.NewProc("ScreenToClient"); proc.Find() == nil {
		proc.Call(w.hwnd, uintptr(unsafe.Pointer(&pt)))
	}
	return ClientCoords(int(pt.X), int(pt.Y), w.scale)
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
	var ps win32.PaintStruct
	hdc, _, _ := procBeginPaint.Call(w.hwnd, uintptr(unsafe.Pointer(&ps)))
	if hdc == 0 {
		return
	}
	w.mu.Lock()
	bw, bh := w.w, w.h
	physW, physH := w.physW, w.physH
	dib := w.dib
	w.mu.Unlock()
	// Blit the whole top-down 32-bpp BGRA framebuffer stretched over the whole
	// physical client. The paint DC is clipped to ps.RcPaint (the InvalidateRect'd
	// damage), so only the damaged pixels are actually written to the screen.
	win32.StretchDIBitsBGRA(win32.HDC(hdc), 0, 0, int32(physW), int32(physH), int32(bw), int32(bh), dib)
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
		r := win32.Rect{Left: int32(x), Top: int32(y), Right: int32(x + rw), Bottom: int32(y + rh)}
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
		win32.DestroyWindow(win32.HWND(w.hwnd))
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
