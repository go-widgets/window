// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

// This file is the AppKit glue that binds the sovereign, OS-independent codec
// (mapping.go) to a live macOS window. It creates an NSWindow with a flipped
// content NSView subclass, presents the toolkit RGBA framebuffer into it by
// wrapping the bytes in an NSBitmapImageRep and drawing it in -drawRect:
// (exactly the reader/"macOS appearance harvest" method), and decodes native
// -mouseDown:/-mouseDragged:/-mouseUp:/-mouseMoved:/-scrollWheel:/-keyDown:/
// -keyUp:/-flagsChanged: NSEvents through mapping.go into toolkit.Event. The
// whole path runs through github.com/go-macos/objc (the fleet's shared purego
// Objective-C bridge) — no cgo — so it links with CGO_ENABLED=0.
//
// Incremental (damage-region) present is honoured: a root that implements
// RenderDamaged has only its damaged rectangles invalidated
// (-setNeedsDisplayInRect:) and re-blitted; a plain root re-presents the whole
// surface. AppKit confines each -drawRect: to the accumulated invalid region, so
// the bitmap draw only touches the damaged pixels on screen.
//
// There is one window per process (a native GUI app owns the single main-thread
// run loop), so the AppKit objects and present state are held in package-level
// vars keyed off the single active *Window that the class callbacks consult —
// the same single-window model the reader precedent uses.
package cocoa

import (
	"fmt"
	"math"
	"runtime"
	"sync"
	"unsafe"

	objc "github.com/go-macos/objc"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// nsPoint / nsSize / nsRect mirror the CoreGraphics geometry structs; purego
// marshals them by value across the amd64/arm64 calling conventions (two/four
// float64s), the same way the reader passes CGRect.
type nsPoint struct{ X, Y float64 }
type nsSize struct{ W, H float64 }
type nsRect struct {
	Origin nsPoint
	Size   nsSize
}

// NSWindowStyleMask bits.
const (
	styleTitled         = 1 << 0
	styleClosable       = 1 << 1
	styleMiniaturizable = 1 << 2
	styleResizable      = 1 << 3
)

const (
	backingStoreBuffered   = 2
	activationPolicyReg    = 0 // NSApplicationActivationPolicyRegular
	nsCompositingCopy      = 1 // NSCompositingOperationCopy
	eventMaskAny           = math.MaxUint64
	defaultRunLoopModeName = "kCFRunLoopDefaultMode"
)

// NSEventType values used when the integration test synthesises real NSEvents.
const (
	evtLeftMouseDown = 1
	evtLeftMouseUp   = 2
	evtKeyDown       = 10
	evtKeyUp         = 11
)

// selectors resolved once.
var (
	selAlloc                = objc.RegisterName("alloc")
	selInit                 = objc.RegisterName("init")
	selRetain               = objc.RegisterName("retain")
	selRelease              = objc.RegisterName("release")
	selSharedApplication    = objc.RegisterName("sharedApplication")
	selSetActivationPolicy  = objc.RegisterName("setActivationPolicy:")
	selActivateIgnoring     = objc.RegisterName("activateIgnoringOtherApps:")
	selNextEvent            = objc.RegisterName("nextEventMatchingMask:untilDate:inMode:dequeue:")
	selSendEvent            = objc.RegisterName("sendEvent:")
	selPostEvent            = objc.RegisterName("postEvent:atStart:")
	selUpdateWindows        = objc.RegisterName("updateWindows")
	selInitContentRect      = objc.RegisterName("initWithContentRect:styleMask:backing:defer:")
	selSetTitle             = objc.RegisterName("setTitle:")
	selSetContentView       = objc.RegisterName("setContentView:")
	selContentView          = objc.RegisterName("contentView")
	selMakeKeyAndOrderFront = objc.RegisterName("makeKeyAndOrderFront:")
	selMakeFirstResponder   = objc.RegisterName("makeFirstResponder:")
	selCenter               = objc.RegisterName("center")
	selSetDelegate          = objc.RegisterName("setDelegate:")
	selBackingScaleFactor   = objc.RegisterName("backingScaleFactor")
	selWindowNumber         = objc.RegisterName("windowNumber")
	selClose                = objc.RegisterName("close")
	selInitWithFrame        = objc.RegisterName("initWithFrame:")
	selBounds               = objc.RegisterName("bounds")
	selSetNeedsDisplay      = objc.RegisterName("setNeedsDisplay:")
	selSetNeedsDisplayRect  = objc.RegisterName("setNeedsDisplayInRect:")
	selDisplayIfNeeded      = objc.RegisterName("displayIfNeeded")
	selInitBitmapRep        = objc.RegisterName("initWithBitmapDataPlanes:pixelsWide:pixelsHigh:bitsPerSample:samplesPerPixel:hasAlpha:isPlanar:colorSpaceName:bytesPerRow:bitsPerPixel:")
	selDrawInRectFull       = objc.RegisterName("drawInRect:fromRect:operation:fraction:respectFlipped:hints:")
	selLocationInWindow     = objc.RegisterName("locationInWindow")
	selScrollingDeltaY      = objc.RegisterName("scrollingDeltaY")
	selKeyCode              = objc.RegisterName("keyCode")
	selCharsIgnoringMods    = objc.RegisterName("charactersIgnoringModifiers")
	selModifierFlags        = objc.RegisterName("modifierFlags")
	selDistantFuture        = objc.RegisterName("distantFuture")
	selDistantPast          = objc.RegisterName("distantPast")
	selMouseEventFactory    = objc.RegisterName("mouseEventWithType:location:modifierFlags:timestamp:windowNumber:context:eventNumber:clickCount:pressure:")
	selKeyEventFactory      = objc.RegisterName("keyEventWithType:location:modifierFlags:timestamp:windowNumber:context:characters:charactersIgnoringModifiers:isARepeat:keyCode:")
)

// damageRenderer is the OPT-IN incremental-present capability, declared
// structurally so this backend needs no import of the parent window package
// (which imports this one). A root that also implements it (e.g.
// toolkit/scene.HostRoot) drives damage-rectangle present; a plain root uses
// whole-surface present. It mirrors window.DamageRenderer exactly.
type damageRenderer interface {
	RenderDamaged(p painter.Painter, th *toolkit.Theme) []toolkit.Rect
}

// Window is an open macOS window bound to a go-widgets scene. It owns the
// backing RGBA framebuffer (in device pixels), presents it to the content view
// and drives the toolkit widget tree from NSEvent input. It satisfies
// window.Backend (Run/Close/Size/String).
type Window struct {
	title string
	theme *toolkit.Theme

	win  objc.ID
	view objc.ID

	mu    sync.Mutex // guards buf/w/h against the -drawRect: reader
	buf   []byte     // RGBA framebuffer, 4*w*h bytes (device pixels)
	w, h  int        // device-pixel size
	scale float64    // backing scale factor (points→pixels)

	root       toolkit.Widget
	dmg        damageRenderer
	buttonHeld bool

	closed bool
}

// active is the single live window the class callbacks route to. A native GUI
// app owns one main-thread run loop and one window here.
var active *Window

// classesOnce registers the view + delegate classes exactly once.
var (
	classesOnce sync.Once
	viewClass   objc.Class
	delegClass  objc.Class
	classesErr  error
)

// frameworksLoaded guards the one-time dlopen of Foundation/AppKit.
var frameworksLoaded bool

func loadFrameworks() error {
	if frameworksLoaded {
		return nil
	}
	if err := objc.Load(objc.Foundation, objc.AppKit); err != nil {
		return err
	}
	frameworksLoaded = true
	return nil
}

func registerClasses() (objc.Class, objc.Class, error) {
	classesOnce.Do(func() {
		viewClass, classesErr = objc.RegisterClass(
			"GoWidgetsWindowView", objc.GetClass("NSView"),
			[]objc.MethodDef{
				{Cmd: objc.RegisterName("isFlipped"), Fn: viewIsFlipped},
				{Cmd: objc.RegisterName("acceptsFirstResponder"), Fn: viewAcceptsFirstResponder},
				{Cmd: objc.RegisterName("drawRect:"), Fn: viewDrawRect},
				{Cmd: objc.RegisterName("mouseDown:"), Fn: viewMouseDown},
				{Cmd: objc.RegisterName("mouseDragged:"), Fn: viewMouseDragged},
				{Cmd: objc.RegisterName("mouseUp:"), Fn: viewMouseUp},
				{Cmd: objc.RegisterName("mouseMoved:"), Fn: viewMouseMoved},
				{Cmd: objc.RegisterName("scrollWheel:"), Fn: viewScrollWheel},
				{Cmd: objc.RegisterName("keyDown:"), Fn: viewKeyDown},
				{Cmd: objc.RegisterName("keyUp:"), Fn: viewKeyUp},
			})
		if classesErr != nil {
			return
		}
		delegClass, classesErr = objc.RegisterClass(
			"GoWidgetsWindowDelegate", objc.GetClass("NSObject"),
			[]objc.MethodDef{
				{Cmd: objc.RegisterName("windowShouldClose:"), Fn: windowShouldClose},
				{Cmd: objc.RegisterName("windowDidResize:"), Fn: windowDidResize},
			})
	})
	return viewClass, delegClass, classesErr
}

// viewIsFlipped makes the view use a top-left origin, matching the buffer.
func viewIsFlipped(_ objc.ID, _ objc.SEL) bool { return true }

// viewAcceptsFirstResponder lets the view receive keyDown:/keyUp: events.
func viewAcceptsFirstResponder(_ objc.ID, _ objc.SEL) bool { return true }

// viewDrawRect blits the current framebuffer into the view. AppKit has already
// clipped the graphics context to the accumulated invalid region, so wrapping
// the whole buffer in an NSBitmapImageRep and drawing it once repaints only the
// damaged pixels on screen. The NSRect dirty-rect argument rides in the float
// registers and is intentionally not declared.
func viewDrawRect(self objc.ID, _ objc.SEL) {
	w := active
	if w == nil {
		return
	}
	w.mu.Lock()
	buf, bw, bh := w.buf, w.w, w.h
	w.mu.Unlock()
	if len(buf) == 0 || bw == 0 || bh == 0 {
		return
	}
	rep := newBitmapRep(buf, bw, bh)
	if rep == 0 {
		return
	}
	bounds := objc.Send[nsRect](self, selBounds)
	// The full drawInRect: form with respectFlipped:YES honours the flipped
	// view so the buffer's row 0 lands at the top of the window; fromRect zero =
	// whole image; operation Copy; fraction 1.0 (opaque); hints nil.
	objc.Send[objc.ID](rep, selDrawInRectFull, bounds, nsRect{}, uint(nsCompositingCopy), 1.0, true, objc.ID(0))
	rep.Send(selRelease)
	runtime.KeepAlive(buf)
}

// newBitmapRep wraps an RGBA buffer in an NSBitmapImageRep that references (does
// not copy) the bytes. buf must outlive the rep's use (the caller keeps it
// alive). The returned rep is owned by the caller (alloc/init) and released
// after the draw.
func newBitmapRep(buf []byte, w, h int) objc.ID {
	planes := [1]unsafe.Pointer{unsafe.Pointer(&buf[0])}
	return objc.ID(objc.GetClass("NSBitmapImageRep")).Send(selAlloc).Send(
		selInitBitmapRep,
		unsafe.Pointer(&planes[0]),
		w, h, 8, 4, true, false,
		objc.NSString("NSDeviceRGBColorSpace"),
		w*4, 32,
	)
}

// viewCoords converts an event's -locationInWindow to device-pixel, top-left
// coordinates via the OS-independent ViewCoords helper.
func (w *Window) viewCoords(self, event objc.ID) (int, int) {
	p := objc.Send[nsPoint](event, selLocationInWindow)
	b := objc.Send[nsRect](self, selBounds)
	return ViewCoords(p.X, p.Y, b.Size.H, w.scale)
}

func eventMods(event objc.ID) (shift, ctrl bool) {
	return DecodeMods(uint64(objc.Send[uint64](event, selModifierFlags)))
}

func viewMouseDown(self objc.ID, _ objc.SEL, event objc.ID) {
	w := active
	if w == nil {
		return
	}
	w.buttonHeld = true
	x, y := w.viewCoords(self, event)
	shift, ctrl := eventMods(event)
	w.dispatch(MapMouseDown(x, y, shift, ctrl))
}

func viewMouseDragged(self objc.ID, _ objc.SEL, event objc.ID) {
	w := active
	if w == nil {
		return
	}
	x, y := w.viewCoords(self, event)
	shift, ctrl := eventMods(event)
	w.dispatch(MapMouseMove(x, y, true, shift, ctrl))
}

func viewMouseUp(self objc.ID, _ objc.SEL, event objc.ID) {
	w := active
	if w == nil {
		return
	}
	w.buttonHeld = false
	x, y := w.viewCoords(self, event)
	shift, ctrl := eventMods(event)
	w.dispatch(MapMouseUp(x, y, shift, ctrl))
}

func viewMouseMoved(self objc.ID, _ objc.SEL, event objc.ID) {
	w := active
	if w == nil {
		return
	}
	x, y := w.viewCoords(self, event)
	shift, ctrl := eventMods(event)
	w.dispatch(MapMouseMove(x, y, false, shift, ctrl))
}

func viewScrollWheel(self objc.ID, _ objc.SEL, event objc.ID) {
	w := active
	if w == nil {
		return
	}
	x, y := w.viewCoords(self, event)
	shift, ctrl := eventMods(event)
	dy := float64(objc.Send[float64](event, selScrollingDeltaY))
	w.dispatch(MapScroll(x, y, dy, shift, ctrl))
}

func viewKeyDown(_ objc.ID, _ objc.SEL, event objc.ID) { keyEvent(event, true) }
func viewKeyUp(_ objc.ID, _ objc.SEL, event objc.ID)   { keyEvent(event, false) }

func keyEvent(event objc.ID, press bool) {
	w := active
	if w == nil {
		return
	}
	code := uint16(objc.Send[uint64](event, selKeyCode))
	chars := objc.GoString(event.Send(selCharsIgnoringMods))
	shift, ctrl := eventMods(event)
	for _, ev := range MapKey(code, chars, shift, ctrl, press) {
		w.dispatch(ev)
	}
}

// windowShouldClose ends the run loop when the user closes the window.
func windowShouldClose(_ objc.ID, _ objc.SEL, _ objc.ID) bool {
	if active != nil {
		active.closed = true
	}
	return true
}

// windowDidResize re-derives the device size from the content view bounds and
// backing scale, reallocates the framebuffer and re-presents the whole surface.
func windowDidResize(_ objc.ID, _ objc.SEL, _ objc.ID) {
	w := active
	if w == nil || w.win == 0 {
		return
	}
	scale := float64(objc.Send[float64](w.win, selBackingScaleFactor))
	if scale <= 0 {
		scale = 1
	}
	cv := w.win.Send(selContentView)
	b := objc.Send[nsRect](cv, selBounds)
	w.resize(int(b.Size.W*scale), int(b.Size.H*scale), scale)
}

// dispatch delivers one toolkit event to the root and re-presents the frame it
// dirtied.
func (w *Window) dispatch(ev toolkit.Event) {
	if w.root != nil {
		w.root.OnEvent(ev)
	}
	w.paintFrame(false)
}

// New creates the NSApplication, an NSWindow with a flipped content view and a
// window delegate, presents an initial blank frame and returns the window ready
// for Run. It must be called on the process main OS thread (the parent Open
// locks it).
func New(title string, width, height int, theme *toolkit.Theme) (*Window, error) {
	if err := loadFrameworks(); err != nil {
		return nil, err
	}
	vc, dc, err := registerClasses()
	if err != nil {
		return nil, err
	}
	if width <= 0 {
		width = 640
	}
	if height <= 0 {
		height = 480
	}
	if theme == nil {
		theme = toolkit.DefaultDark()
	}

	app := objc.ID(objc.GetClass("NSApplication")).Send(selSharedApplication)
	app.Send(selSetActivationPolicy, activationPolicyReg)

	rect := nsRect{Size: nsSize{W: float64(width), H: float64(height)}}
	style := uint(styleTitled | styleClosable | styleMiniaturizable | styleResizable)
	win := objc.ID(objc.GetClass("NSWindow")).Send(selAlloc).
		Send(selInitContentRect, rect, style, backingStoreBuffered, false)
	win.Send(selRetain)

	view := objc.ID(vc).Send(selAlloc).Send(selInitWithFrame, rect)
	view.Send(selRetain)
	win.Send(selSetContentView, view)

	deleg := objc.ID(dc).Send(selAlloc).Send(selInit)
	deleg.Send(selRetain)
	win.Send(selSetDelegate, deleg)

	scale := float64(objc.Send[float64](win, selBackingScaleFactor))
	if scale <= 0 {
		scale = 1
	}
	w := &Window{
		title: title,
		theme: theme,
		win:   win,
		view:  view,
		w:     int(float64(width) * scale),
		h:     int(float64(height) * scale),
		scale: scale,
	}
	w.buf = make([]byte, 4*w.w*w.h)
	active = w

	win.Send(selSetTitle, objc.NSString(title))
	win.Send(selCenter)
	win.Send(selMakeKeyAndOrderFront, objc.ID(0))
	win.Send(selMakeFirstResponder, view)
	app.Send(selActivateIgnoring, true)
	return w, nil
}

// Run binds root, performs the initial layout+present, then pumps NSEvents into
// the widget tree until the window is closed. It is the macOS analogue of the
// X11 backend's event loop.
func (w *Window) Run(root toolkit.Widget) error {
	w.bindAndSeed(root)
	app := objc.ID(objc.GetClass("NSApplication")).Send(selSharedApplication)
	future := objc.ID(objc.GetClass("NSDate")).Send(selDistantFuture)
	mode := objc.NSString(defaultRunLoopModeName)
	for !w.closed {
		ev := app.Send(selNextEvent, uint64(eventMaskAny), future, mode, true)
		if ev != 0 {
			app.Send(selSendEvent, ev)
		}
		app.Send(selUpdateWindows)
	}
	return nil
}

// bindAndSeed binds root and paints+presents the initial full frame. When the
// root is incremental, the first frame is drawn through it so its whole-surface
// seed damage is consumed here — the first interaction is then already a purely
// incremental frame.
func (w *Window) bindAndSeed(root toolkit.Widget) {
	w.root = root
	w.dmg, _ = root.(damageRenderer)
	if w.dmg != nil {
		w.drawIncremental()
	} else {
		w.draw()
	}
	w.presentFull()
}

// draw repaints the whole framebuffer: background fill then the root laid out to
// fill the client area.
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
}

// drawIncremental lays the root out to the full client area and repaints ONLY
// the damage the root reports, returning the repainted rectangles.
func (w *Window) drawIncremental() []toolkit.Rect {
	w.mu.Lock()
	defer w.mu.Unlock()
	p := painter.NewPixelPainter(w.buf, w.w, w.h)
	w.root.SetBounds(toolkit.Rect{X: 0, Y: 0, W: w.w, H: w.h})
	return w.dmg.RenderDamaged(p, w.theme)
}

// paintFrame renders and presents one frame after an event or resize. A plain
// root repaints+re-presents the whole surface; an incremental root invalidates
// and re-presents only its damaged rectangles — except after a resize, where the
// framebuffer was reallocated (whole-surface damage) and the full surface is
// presented.
func (w *Window) paintFrame(resize bool) {
	if w.dmg == nil {
		w.draw()
		w.presentFull()
		return
	}
	rects := w.drawIncremental()
	if resize {
		w.presentFull()
		return
	}
	w.presentRects(rects)
}

// presentFull invalidates and re-blits the whole content view.
func (w *Window) presentFull() {
	if w.view == 0 {
		return
	}
	w.view.Send(selSetNeedsDisplay, true)
	w.view.Send(selDisplayIfNeeded)
}

// presentRects invalidates only the damaged rectangles (converted to the flipped
// view's point coordinates) and blits them.
func (w *Window) presentRects(rects []toolkit.Rect) {
	if w.view == 0 || len(rects) == 0 {
		return
	}
	for _, r := range rects {
		x, y, rw, rh := DirtyRect(r, w.scale)
		w.view.Send(selSetNeedsDisplayRect, nsRect{Origin: nsPoint{X: x, Y: y}, Size: nsSize{W: rw, H: rh}})
	}
	w.view.Send(selDisplayIfNeeded)
}

// resize grows/shrinks the framebuffer to the new device size and re-presents.
func (w *Window) resize(nw, nh int, scale float64) {
	if nw <= 0 || nh <= 0 || (nw == w.w && nh == w.h) {
		return
	}
	w.mu.Lock()
	w.w, w.h, w.scale = nw, nh, scale
	w.buf = make([]byte, 4*nw*nh)
	w.mu.Unlock()
	w.paintFrame(true)
}

// Size returns the current client size in device pixels.
func (w *Window) Size() (int, int) { return w.w, w.h }

// Close releases the window. Safe to call more than once.
func (w *Window) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	if w.win != 0 {
		w.win.Send(selClose)
	}
	if active == w {
		active = nil
	}
	return nil
}

// String identifies the window for debugging.
func (w *Window) String() string {
	return fmt.Sprintf("cocoa.Window(%dx%d %q)", w.w, w.h, w.title)
}
