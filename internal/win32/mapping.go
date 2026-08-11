// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// Package win32 is the pure-Go (CGO-free) Windows Win32/GDI windowing backend
// for the go-widgets toolkit. It registers a window class and creates a real
// top-level HWND, blits the toolkit's RGBA framebuffer into it through a
// top-down 32bpp DIB with StretchDIBits, and routes native WM_* mouse/wheel/key
// input into toolkit.Event, so a go-widgets widget tree runs on a Windows
// desktop exactly as it does on X11, Wayland, macOS or in the browser/wasm host.
//
// The Win32 API is reached through the process' own kernel32/user32/gdi32 DLLs
// via syscall.NewLazyDLL + syscall.SyscallN and a syscall.NewCallback WNDPROC —
// no cgo — so the whole module builds and links with CGO_ENABLED=0.
//
// This file is the SOVEREIGN, OS-INDEPENDENT half: the WM_*→toolkit.Event
// mapping (virtual-key decode, modifier decode, button/wheel mapping), the
// RGBA→BGRA DIB packing, the Per-Monitor-V2 DPI/size maths and the
// damage-rect→InvalidateRect conversion, all expressed over plain Go values with
// a single toolkit dependency (the event model). It carries NO
// syscall/unsafe/windows dependency, so it builds — and is unit-tested to 100% —
// on every GOOS, mirroring internal/cocoa's mapping.go and internal/wasmbox's
// protocol.go. The thin Win32 glue that actually creates the HWND, presents the
// DIB and pumps the message loop lives in win32_windows.go (//go:build windows)
// and drives everything here.
package win32

import (
	"math"

	"github.com/go-widgets/toolkit"
)

// Win32 mouse-message wParam key-state bits (winuser.h MK_*). They report the
// modifier keys and mouse buttons held when a mouse message was produced.
const (
	mkLButton = 0x0001
	mkRButton = 0x0002
	mkShift   = 0x0004
	mkControl = 0x0008
	mkMButton = 0x0010
)

// Win32 virtual-key codes (winuser.h VK_*) for the named editing/navigation
// keys. Every other key is treated as a printable character, decoded from the
// WM_CHAR message the OS synthesises after TranslateMessage.
const (
	vkBack   = 0x08
	vkTab    = 0x09
	vkReturn = 0x0D
	vkEscape = 0x1B
	vkPrior  = 0x21 // Page Up
	vkNext   = 0x22 // Page Down
	vkEnd    = 0x23
	vkHome   = 0x24
	vkLeft   = 0x25
	vkUp     = 0x26
	vkRight  = 0x27
	vkDown   = 0x28
	vkDelete = 0x2E
)

// wheelDelta is one notch of the mouse wheel (winuser.h WHEEL_DELTA); the OS
// reports scroll amounts in multiples of it.
const wheelDelta = 120

// Default window sizing. When a caller opens a window without an explicit size
// the backend picks a readable default from the monitor's work area: a fraction
// of the usable area, clamped to a comfortable band on each axis and never
// larger than the work area itself. All values are LOGICAL points (DIPs, the
// unit the toolkit lays out and the user reads in), never device pixels — the
// same model the Cocoa backend uses, so a defaulted window is legible on a
// HiDPI display rather than tiny.
const (
	// defaultFallbackW/H is used when the work-area size is unknown (no monitor,
	// e.g. a headless build) — a plainly readable desktop-window size.
	defaultFallbackW = 1280
	defaultFallbackH = 800
	// defaultScreenFraction is the share of the work area a defaulted window
	// occupies on each axis.
	defaultScreenFraction = 0.85
	// The clamp band keeps the default comfortably readable on a small display
	// yet never sprawling on a very large one.
	minContentW = 960
	minContentH = 600
	maxContentW = 1600
	maxContentH = 1000
)

// usdaDefaultDPI is the Win32 reference DPI (winuser.h USER_DEFAULT_SCREEN_DPI):
// 96 dots per inch is a 100% (scale 1.0) display. Every DPI the OS reports is
// interpreted relative to it.
const usdaDefaultDPI = 96

// ScaleForDpi converts a Win32 DPI value (as returned by GetDpiForWindow /
// WM_DPICHANGED) into the framebuffer render scale: physical device pixels per
// logical point. 96 DPI (100%) → 1.0, 144 (150%) → 1.5, 192 (200%) → 2.0. A
// zero or negative DPI (an unavailable monitor) defaults to 1.0 so the maths
// never divides by zero downstream.
func ScaleForDpi(dpi uint32) float64 {
	if dpi == 0 {
		return 1
	}
	return float64(dpi) / usdaDefaultDPI
}

// PhysicalFromLogical converts a logical-point extent to physical device pixels
// at the given render scale, rounded to the nearest whole pixel and never below
// 1 (a visible window always has a positive pixel size). It sizes the window's
// physical client area from the toolkit's logical layout: at 150% a 800-point
// content width becomes a 1200-pixel client area, so the OS shows the logical
// UI at a readable physical size.
func PhysicalFromLogical(logical int, scale float64) int {
	if scale <= 0 {
		scale = 1
	}
	v := int(math.Round(float64(logical) * scale))
	if v < 1 {
		v = 1
	}
	return v
}

// LogicalFromPhysical converts a physical client extent (WM_SIZE reports the
// client area in device pixels) back to the logical-point size the framebuffer
// is rendered at, rounded to the nearest whole point and never below 1. It is
// the inverse of PhysicalFromLogical: at 150% a 1200-pixel client becomes an
// 800-point framebuffer, keeping the UI readable as the window resizes.
func LogicalFromPhysical(phys int, scale float64) int {
	if scale <= 0 {
		scale = 1
	}
	v := int(math.Round(float64(phys) / scale))
	if v < 1 {
		v = 1
	}
	return v
}

// DefaultContentSize picks a readable default window content size, in LOGICAL
// points, from the monitor work area (workW×workH, also in logical points). It
// takes defaultScreenFraction of the work area and clamps each axis to the
// [min,max] readability band, then to the work extent so the window never
// exceeds the usable screen. When the work area is unknown (workW or workH ≤ 0)
// it returns the fixed fallback. The result is always ≥ 1×1 and ≤ the work
// area, so a defaulted window is legible without manual sizing. It mirrors the
// Cocoa backend's DefaultContentSize exactly.
func DefaultContentSize(workW, workH float64) (w, h int) {
	if workW <= 0 || workH <= 0 {
		return defaultFallbackW, defaultFallbackH
	}
	return clampContent(workW*defaultScreenFraction, minContentW, maxContentW, workW),
		clampContent(workH*defaultScreenFraction, minContentH, maxContentH, workH)
}

// clampContent rounds want down to whole points, clamps it into [lo,hi] and then
// caps it at the available extent avail so the window never exceeds the screen.
func clampContent(want float64, lo, hi int, avail float64) int {
	v := int(want)
	if v < lo {
		v = lo
	}
	if v > hi {
		v = hi
	}
	if float64(v) > avail {
		v = int(avail)
	}
	return v
}

// CenterOffset returns the top-left offset at which a window of outer size
// winExtent is centred within an axis of usable extent avail: (avail-winExtent)/2,
// clamped to 0 so a window larger than the work area is pinned to the origin
// rather than pushed off-screen. The caller adds the work-area origin (left/top)
// to place the window absolutely.
func CenterOffset(avail, winExtent int) int {
	off := (avail - winExtent) / 2
	if off < 0 {
		off = 0
	}
	return off
}

// Mods is the decoded modifier state carried on every toolkit event the Win32
// backend emits: Shift, Ctrl, Alt and Meta (the ⊞ Windows/logo key).
type Mods struct{ Shift, Ctrl, Alt, Meta bool }

// apply stamps the four modifier flags onto ev.
func (m Mods) apply(ev toolkit.Event) toolkit.Event {
	ev.Shift, ev.Ctrl, ev.Alt, ev.Meta = m.Shift, m.Ctrl, m.Alt, m.Meta
	return ev
}

// DecodeMouseMods splits a Win32 mouse-message wParam (its low word carries the
// MK_* key-state bits) into the toolkit modifiers, so a Ctrl-click or
// Shift-click reaches a widget with the same flags an X11/Cocoa chord would. The
// mouse wParam carries no Alt or Windows-key bit, so Alt/Meta are left false for
// pointer events (they are read from the keyboard state for key events).
func DecodeMouseMods(wparam uintptr) Mods {
	return Mods{Shift: wparam&mkShift != 0, Ctrl: wparam&mkControl != 0}
}

// acceleratorRune maps a letter/digit virtual key held under a Ctrl or Meta
// chord to the lowercase character the toolkit expects in Code, and reports
// whether vk is such a key. Windows delivers no usable WM_CHAR for Ctrl+letter
// (it synthesises a control code instead), so without this a ⌃C / ⌃V shortcut
// would reach the widget tree as nothing at all — unlike X11/Cocoa/Wayland,
// where the letter is always the Code. Emitting it here from MapKeyDown/Up
// closes that gap, so a file manager's Ctrl+C/X/V works on Windows too.
func acceleratorRune(vk uint32) (string, bool) {
	switch {
	case vk >= 'A' && vk <= 'Z':
		return string(rune(vk - 'A' + 'a')), true
	case vk >= '0' && vk <= '9':
		return string(rune(vk)), true
	default:
		return "", false
	}
}

// AnyButtonDown reports whether any mouse button is held per a WM_MOUSEMOVE
// wParam, which is how the backend distinguishes a drag from a plain hover move
// (the X11/Wayland backends read the same button mask; Cocoa gets it from the
// distinct -mouseDragged: selector).
func AnyButtonDown(wparam uintptr) bool {
	return wparam&(mkLButton|mkRButton|mkMButton) != 0
}

// DecodeVK maps a Win32 virtual-key code to the toolkit's symbolic key NAME
// (DOM-style: "Enter", "ArrowLeft", …, exactly the names the toolkit widgets
// match and the X11/Cocoa/wasmbox backends emit). It returns "" for every key
// that is not one of the named editing/navigation keys — those carry a
// printable rune instead, delivered via the WM_CHAR path (MapCharDown).
func DecodeVK(vk uint32) string {
	switch vk {
	case vkReturn:
		return "Enter"
	case vkTab:
		return "Tab"
	case vkBack:
		return "Backspace"
	case vkDelete:
		return "Delete"
	case vkEscape:
		return "Escape"
	case vkHome:
		return "Home"
	case vkEnd:
		return "End"
	case vkPrior:
		return "PageUp"
	case vkNext:
		return "PageDown"
	case vkLeft:
		return "ArrowLeft"
	case vkRight:
		return "ArrowRight"
	case vkUp:
		return "ArrowUp"
	case vkDown:
		return "ArrowDown"
	default:
		return ""
	}
}

// MapKeyDown turns a WM_KEYDOWN virtual key into the toolkit event(s) it
// produces. A NAMED key (Enter, ArrowLeft, …) yields a single EventKeyDown
// carrying the name in Code. A key that is not named yields nothing here: on
// Windows the printable character is not known until the OS translates the
// keystroke into the following WM_CHAR, so MapCharDown emits the KeyDown+Char
// pair for printables — keeping the toolkit's press/char split identical to the
// X11 and Cocoa backends.
func MapKeyDown(vk uint32, m Mods) []toolkit.Event {
	if name := DecodeVK(vk); name != "" {
		return []toolkit.Event{m.apply(toolkit.Event{Kind: toolkit.EventKeyDown, Code: name})}
	}
	// Under a Ctrl or Meta chord a letter/digit's WM_CHAR is a control code, so
	// emit the accelerator letter here (⌃C → EventKeyDown{"c"}); a bare letter
	// still yields nothing (its printable comes from MapCharDown's WM_CHAR).
	if m.Ctrl || m.Meta {
		if s, ok := acceleratorRune(vk); ok {
			return []toolkit.Event{m.apply(toolkit.Event{Kind: toolkit.EventKeyDown, Code: s})}
		}
	}
	return nil
}

// MapKeyUp turns a WM_KEYUP virtual key into the toolkit event(s) it produces.
// A NAMED key yields a single EventKeyUp carrying the name; a non-named
// (printable) key's release is delivered via MapCharUp (the glue translates the
// virtual key to its rune), so a printable key produces EventKeyUp{rune} exactly
// as the X11 backend does.
func MapKeyUp(vk uint32, m Mods) []toolkit.Event {
	if name := DecodeVK(vk); name != "" {
		return []toolkit.Event{m.apply(toolkit.Event{Kind: toolkit.EventKeyUp, Code: name})}
	}
	if m.Ctrl || m.Meta {
		if s, ok := acceleratorRune(vk); ok {
			return []toolkit.Event{m.apply(toolkit.Event{Kind: toolkit.EventKeyUp, Code: s})}
		}
	}
	return nil
}

// MapCharDown turns a committed printable rune (from WM_CHAR on key press) into
// the EventKeyDown+EventChar pair the toolkit expects for text input — the same
// press/char split the X11 and Cocoa backends perform. A non-printable code
// (a control code such as the ^M/^I/^[ that WM_CHAR also delivers for
// Enter/Tab/Escape, or DEL) yields nothing, because those keys are already
// delivered as named keys through MapKeyDown.
func MapCharDown(r rune, m Mods) []toolkit.Event {
	if !isPrintable(r) {
		return nil
	}
	s := string(r)
	return []toolkit.Event{
		m.apply(toolkit.Event{Kind: toolkit.EventKeyDown, Code: s}),
		m.apply(toolkit.Event{Kind: toolkit.EventChar, Code: s}),
	}
}

// MapCharUp turns a printable rune (the glue translates the WM_KEYUP virtual key
// to its character) into the single EventKeyUp the toolkit expects on release. A
// non-printable rune yields nothing.
func MapCharUp(r rune, m Mods) []toolkit.Event {
	if !isPrintable(r) {
		return nil
	}
	return []toolkit.Event{m.apply(toolkit.Event{Kind: toolkit.EventKeyUp, Code: string(r)})}
}

// isPrintable reports whether r is a genuine committed character rather than a
// control code or DEL. WM_CHAR delivers control codes (0x00–0x1F) for keys such
// as Enter/Tab/Escape, which the backend already handles as named keys, so those
// are filtered out here.
func isPrintable(r rune) bool {
	return r >= 0x20 && r != 0x7f
}

// MapMouseDown turns a WM_LBUTTONDOWN/WM_RBUTTONDOWN at the given framebuffer
// pixel into an EventClick, mirroring the X11 ButtonPress (buttons 1–3 → click)
// and Cocoa mapping. Windows delivers a distinct message per button; the backend
// routes all of them here, button-agnostically, exactly as the toolkit's click
// model expects.
func MapMouseDown(x, y int, m Mods) toolkit.Event {
	return m.apply(toolkit.Event{Kind: toolkit.EventClick, X: x, Y: y})
}

// MapMouseUp turns a WM_LBUTTONUP/WM_RBUTTONUP into an EventMouseUp.
func MapMouseUp(x, y int, m Mods) toolkit.Event {
	return m.apply(toolkit.Event{Kind: toolkit.EventMouseUp, X: x, Y: y})
}

// MapMouseMove turns a WM_MOUSEMOVE into a drag (a button held) or a plain hover
// move (no button), per buttonHeld — the same drag-vs-move split the X11/Wayland
// backends derive from the event's button-state mask.
func MapMouseMove(x, y int, buttonHeld bool, m Mods) toolkit.Event {
	kind := toolkit.EventMouseMove
	if buttonHeld {
		kind = toolkit.EventMouseDrag
	}
	return m.apply(toolkit.Event{Kind: kind, X: x, Y: y})
}

// MapWheel turns a WM_MOUSEWHEEL delta into an EventScroll whose Delta is
// normalised to the toolkit's ±1 row step. Windows reports a POSITIVE delta when
// the wheel is rotated forward/away from the user (a scroll-up gesture); the
// toolkit's Delta is POSITIVE to scroll down/forward, so the sign is inverted —
// matching the browser/wheel convention the X11 and wasmbox backends use. A zero
// delta yields a Delta-0 EventScroll (harmless; scrollable widgets clamp it), so
// the mapping is total.
func MapWheel(x, y, delta int, m Mods) toolkit.Event {
	return m.apply(toolkit.Event{Kind: toolkit.EventScroll, X: x, Y: y, Delta: -signi(delta)})
}

// signi returns the sign of v as -1, 0 or +1.
func signi(v int) int {
	switch {
	case v < 0:
		return -1
	case v > 0:
		return 1
	default:
		return 0
	}
}

// ClientCoords converts a mouse position in physical client pixels (top-left
// origin, the space every WM_MOUSE* message reports) into framebuffer pixels —
// the LOGICAL-point coordinate space the toolkit framebuffer and every
// toolkit.Event uses. At the default render scale of 1 the two spaces coincide;
// at 150% a click at physical (300,150) maps to framebuffer (200,100), so a
// widget is hit where the user sees it. The result is clamped to be
// non-negative.
func ClientCoords(px, py int, scale float64) (int, int) {
	if scale <= 0 {
		scale = 1
	}
	x := int(float64(px) / scale)
	y := int(float64(py) / scale)
	if x < 0 {
		x = 0
	}
	if y < 0 {
		y = 0
	}
	return x, y
}

// InvalidRect converts a damage rectangle in framebuffer (LOGICAL) pixels — the
// space RenderDamaged reports and the framebuffer uses — into a rectangle in
// physical client pixels, ready for InvalidateRect / the WM_PAINT update region.
// Because the framebuffer is logical and the client area is physical, the rect
// is multiplied by the render scale (the inverse of the Cocoa DirtyRect, which
// divides a device-pixel rect back to points). The rectangle is clamped to be
// non-negative and expanded to whole pixels (floor origin, ceil far edge) so a
// sub-pixel damage rect never leaves a seam on screen.
func InvalidRect(r toolkit.Rect, scale float64) (x, y, w, h int) {
	if scale <= 0 {
		scale = 1
	}
	x0 := math.Floor(float64(r.X) * scale)
	y0 := math.Floor(float64(r.Y) * scale)
	x1 := math.Ceil(float64(r.X+r.W) * scale)
	y1 := math.Ceil(float64(r.Y+r.H) * scale)
	if x0 < 0 {
		x0 = 0
	}
	if y0 < 0 {
		y0 = 0
	}
	return int(x0), int(y0), int(x1 - x0), int(y1 - y0)
}

// PackBGRA converts the whole RGBA framebuffer src (the toolkit's byte order:
// R,G,B,A) into the BGRA byte order a Win32 top-down 32bpp BI_RGB DIB requires
// (B,G,R,A), writing into dst. It swaps the R and B channels of every pixel and
// preserves G and A. dst and src must be the same length and a multiple of 4;
// any tail shorter than a whole pixel is left untouched. This is the pure core
// of the StretchDIBits present path.
func PackBGRA(dst, src []byte) {
	n := len(src)
	if len(dst) < n {
		n = len(dst)
	}
	n -= n % 4
	for i := 0; i < n; i += 4 {
		dst[i+0] = src[i+2] // B ← R
		dst[i+1] = src[i+1] // G
		dst[i+2] = src[i+0] // R ← B
		dst[i+3] = src[i+3] // A
	}
}

// PackBGRARect converts only the w0×h0 sub-rectangle at (x,y) of the RGBA
// framebuffer src into the BGRA DIB dst, both laid out as width×height 32bpp
// rows (stride = 4*width bytes). It is the damage-region packer: after an
// incremental repaint only the changed rectangle is re-packed before the
// damaged sub-blit, instead of re-packing the whole surface. The rectangle is
// clamped to the surface, so an out-of-bounds rect packs only its visible part
// (and an empty intersection packs nothing).
func PackBGRARect(dst, src []byte, width, height, x, y, w0, h0 int) {
	// Clamp the rectangle to the surface bounds.
	if x < 0 {
		w0 += x
		x = 0
	}
	if y < 0 {
		h0 += y
		y = 0
	}
	if x+w0 > width {
		w0 = width - x
	}
	if y+h0 > height {
		h0 = height - y
	}
	if w0 <= 0 || h0 <= 0 {
		return
	}
	stride := width * 4
	for row := y; row < y+h0; row++ {
		base := row*stride + x*4
		for i := base; i < base+w0*4; i += 4 {
			if i+3 >= len(src) || i+3 >= len(dst) {
				return
			}
			dst[i+0] = src[i+2] // B ← R
			dst[i+1] = src[i+1] // G
			dst[i+2] = src[i+0] // R ← B
			dst[i+3] = src[i+3] // A
		}
	}
}
