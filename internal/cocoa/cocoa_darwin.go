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
	"time"
	"unsafe"

	objc "github.com/go-macos/objc"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/window/internal/dnd"
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
	// styleBorderless is NSWindowStyleMaskBorderless: no title bar, no frame,
	// nothing but content. It is what an immersive surface wants.
	styleBorderless = 0
)

const (
	backingStoreBuffered = 2
	activationPolicyReg  = 0 // NSApplicationActivationPolicyRegular
	// activationPolicyAccessory is an application with no Dock icon that never
	// becomes the active one. It is what a passive window's process must be, or
	// opening it takes the keyboard from whatever the person was using.
	activationPolicyAccessory = 1 // NSApplicationActivationPolicyAccessory
	nsCompositingCopy         = 1 // NSCompositingOperationCopy
	eventMaskAny              = math.MaxUint64
	defaultRunLoopModeName    = "kCFRunLoopDefaultMode"
)

// NSEventType values used when the integration test synthesises real NSEvents.
const (
	evtLeftMouseDown  = 1
	evtLeftMouseUp    = 2
	evtRightMouseDown = 3
	evtMouseMoved     = 5
	evtKeyDown        = 10
	evtKeyUp          = 11
)

// selectors resolved once.
var (
	selAlloc                 = objc.RegisterName("alloc")
	selInit                  = objc.RegisterName("init")
	selRetain                = objc.RegisterName("retain")
	selRelease               = objc.RegisterName("release")
	selSetActivationPolicy   = objc.RegisterName("setActivationPolicy:")
	selActivateIgnoring      = objc.RegisterName("activateIgnoringOtherApps:")
	selNextEvent             = objc.RegisterName("nextEventMatchingMask:untilDate:inMode:dequeue:")
	selSendEvent             = objc.RegisterName("sendEvent:")
	selRun                   = objc.RegisterName("run")
	selStop                  = objc.RegisterName("stop:")
	selPostEvent             = objc.RegisterName("postEvent:atStart:")
	selUpdateWindows         = objc.RegisterName("updateWindows")
	selInitContentRect       = objc.RegisterName("initWithContentRect:styleMask:backing:defer:")
	selSetTitle              = objc.RegisterName("setTitle:")
	selSetContentView        = objc.RegisterName("setContentView:")
	selContentView           = objc.RegisterName("contentView")
	selMakeKeyAndOrderFront  = objc.RegisterName("makeKeyAndOrderFront:")
	selSetLevel              = objc.RegisterName("setLevel:")
	selSetCollectionBehavior = objc.RegisterName("setCollectionBehavior:")
	selSetAcceptsMouseMoved  = objc.RegisterName("setAcceptsMouseMovedEvents:")
	selMakeFirstResponder    = objc.RegisterName("makeFirstResponder:")
	selCenter                = objc.RegisterName("center")
	selSetFrameTopLeftPoint  = objc.RegisterName("setFrameTopLeftPoint:")
	selSetDelegate           = objc.RegisterName("setDelegate:")
	selBackingScaleFactor    = objc.RegisterName("backingScaleFactor")
	selMainScreen            = objc.RegisterName("mainScreen")
	selVisibleFrame          = objc.RegisterName("visibleFrame")
	selWindowNumber          = objc.RegisterName("windowNumber")
	selClose                 = objc.RegisterName("close")
	selInitWithFrame         = objc.RegisterName("initWithFrame:")
	selBounds                = objc.RegisterName("bounds")
	selSetNeedsDisplay       = objc.RegisterName("setNeedsDisplay:")
	selSetNeedsDisplayRect   = objc.RegisterName("setNeedsDisplayInRect:")
	selDisplayIfNeeded       = objc.RegisterName("displayIfNeeded")
	selInitBitmapRep         = objc.RegisterName("initWithBitmapDataPlanes:pixelsWide:pixelsHigh:bitsPerSample:samplesPerPixel:hasAlpha:isPlanar:colorSpaceName:bytesPerRow:bitsPerPixel:")
	selDrawInRectFull        = objc.RegisterName("drawInRect:fromRect:operation:fraction:respectFlipped:hints:")
	selLocationInWindow      = objc.RegisterName("locationInWindow")
	selScrollingDeltaY       = objc.RegisterName("scrollingDeltaY")
	selKeyCode               = objc.RegisterName("keyCode")
	selCharsIgnoringMods     = objc.RegisterName("charactersIgnoringModifiers")
	selModifierFlags         = objc.RegisterName("modifierFlags")
	selDistantFuture         = objc.RegisterName("distantFuture")
	selDistantPast           = objc.RegisterName("distantPast")
	selMouseEventFactory     = objc.RegisterName("mouseEventWithType:location:modifierFlags:timestamp:windowNumber:context:eventNumber:clickCount:pressure:")
	selKeyEventFactory       = objc.RegisterName("keyEventWithType:location:modifierFlags:timestamp:windowNumber:context:characters:charactersIgnoringModifiers:isARepeat:keyCode:")
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
// backing RGBA framebuffer (in RENDER pixels), presents it to the content view
// and drives the toolkit widget tree from NSEvent input. It satisfies
// window.Backend (Run/Close/Size/String).
//
// HiDPI model. The toolkit lays out and paints in the framebuffer's coordinate
// space, so that space MUST equal the window's LOGICAL point size for the UI to
// appear at a readable size: the framebuffer is width×height points at
// scale=1, and AppKit up-samples that logical bitmap to the display's backing
// resolution when drawing it into the (point-sized) content view — readable on a
// Retina display, exactly 1:1 on a non-Retina one. (Rendering at device pixels
// instead — logical×backing — makes the toolkit lay the UI out twice as large in
// pixels, so it is presented at HALF its logical size: crisp but far too small
// to read. That was the pre-fix behaviour.) A future crisp-HiDPI path can set
// scale = backing once the toolkit gains a global UI-scale hook that grows every
// widget's pixel metrics to match; the whole coordinate pipeline already honours
// an arbitrary scale, so only renderScaleOverride/New need change.
type Window struct {
	title string
	theme *toolkit.Theme

	win  objc.ID
	view objc.ID

	mu      sync.Mutex // guards buf/w/h against the -drawRect: reader
	buf     []byte     // RGBA framebuffer, 4*w*h bytes (render pixels)
	w, h    int        // render-pixel size (= logical points at scale 1)
	scale   float64    // RENDER scale: framebuffer pixels per point (1 = logical)
	backing float64    // display backing scale factor (1 or 2), for diagnostics
	// follow records that the caller asked for the PANEL's resolution rather
	// than a fixed scale. The request has to outlive construction: a window
	// moved to a display of another density has to re-derive, and the resolved
	// scale alone cannot say whether it is allowed to.
	follow bool
	// passive records that this window must never take the keyboard or the mouse.
	// See Options.Passive.
	passive bool

	root       toolkit.Widget
	dmg        damageRenderer
	buttonHeld bool
	dnd        *dnd.Controller

	// Translucent-material (vibrancy) state. Zero unless the tree carries a
	// toolkit.Material: translucent flips the window non-opaque, container holds
	// the effect views behind the framebuffer view, holes are the framebuffer
	// regions punched transparent so the effect views show through, and
	// materials/effectViews track the installed NSVisualEffectViews.
	translucent bool
	container   objc.ID
	effectViews []objc.ID
	holes       []toolkit.Rect
	materials   []*toolkit.Material

	// toolkit.Native: live AppKit controls embedded over the framebuffer, one per
	// Native in the tree, kept across frames (keyed for identity) so their focus
	// and text survive relayout. See native_darwin.go.
	nativeControls map[string]*liveControl

	// Accessibility publish gate: republishing the NSAccessibilityElement tree
	// is thousands of ObjC round-trips (per node, on the main thread), so it runs
	// only when the tree actually changed. a11yShown records that a tree has been
	// published at least once; lastA11ySig is the signature (roles + names +
	// rects) of that publication, compared against each frame's tree to skip the
	// rebuild while only pixels animate.
	a11yShown   bool
	lastA11ySig uint64
	// lastA11yTime is when the tree was last republished; refreshA11y throttles
	// rebuilds of a continuously-changing tree (a scroll) to a11yMinInterval.
	lastA11yTime time.Time

	closed bool
}

// renderScaleOverride, when > 0, forces the framebuffer render scale (framebuffer
// pixels per logical point) instead of the readable default of 1.0. Production
// leaves it 0. It is the single knob the crisp-HiDPI follow-up will flip (to the
// backing factor, once the toolkit can scale widget geometry to match) and the
// seam the on-device legibility proof uses to render the pre-fix device-pixel
// path and the post-fix logical path side by side and measure the gain.
var renderScaleOverride float64

// active is the single live window the class callbacks route to. A native GUI
// app owns one main-thread run loop and one window here.
var active *Window

// classesOnce registers the view + delegate classes exactly once.
var (
	classesOnce sync.Once
	windowClass objc.Class
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
		// A BORDERLESS window answers NO to -canBecomeKeyWindow unless a subclass
		// says otherwise, and a window that cannot become key receives no keyboard
		// events and no -mouseMoved:. A full-screen window built through
		// Config.Fullscreen therefore showed its picture perfectly and ignored
		// every key and the pointer entirely -- which reads as "the app is broken"
		// rather than "AppKit has an opinion about chrome".
		//
		// The override is unconditional: a titled window already answers YES, so
		// saying it again changes nothing there.
		windowClass, classesErr = objc.RegisterClass(
			"GoWidgetsWindow", objc.GetClass("NSWindow"),
			[]objc.MethodDef{
				{Cmd: objc.RegisterName("canBecomeKeyWindow"), Fn: windowCanBecomeKey},
				{Cmd: objc.RegisterName("canBecomeMainWindow"), Fn: windowCanBecomeKey},
			})
		if classesErr != nil {
			return
		}
		viewClass, classesErr = objc.RegisterClass(
			"GoWidgetsWindowView", objc.GetClass("NSView"),
			append([]objc.MethodDef{
				{Cmd: objc.RegisterName("isFlipped"), Fn: viewIsFlipped},
				{Cmd: objc.RegisterName("acceptsFirstResponder"), Fn: viewAcceptsFirstResponder},
				{Cmd: objc.RegisterName("drawRect:"), Fn: viewDrawRect},
				{Cmd: objc.RegisterName("mouseDown:"), Fn: viewMouseDown},
				{Cmd: objc.RegisterName("rightMouseDown:"), Fn: viewRightMouseDown},
				{Cmd: objc.RegisterName("mouseDragged:"), Fn: viewMouseDragged},
				{Cmd: objc.RegisterName("mouseUp:"), Fn: viewMouseUp},
				{Cmd: objc.RegisterName("mouseMoved:"), Fn: viewMouseMoved},
				{Cmd: objc.RegisterName("scrollWheel:"), Fn: viewScrollWheel},
				{Cmd: objc.RegisterName("keyDown:"), Fn: viewKeyDown},
				{Cmd: objc.RegisterName("keyUp:"), Fn: viewKeyUp},
				{Cmd: objc.RegisterName("viewDidChangeBackingProperties"), Fn: viewDidChangeBackingProperties},
				{Cmd: selRepaintNow, Fn: viewRepaintNow},
				{Cmd: selCloseNow, Fn: viewCloseNow},
			}, a11yMethods()...))
		if classesErr != nil {
			return
		}
		delegClass, classesErr = objc.RegisterClass(
			"GoWidgetsWindowDelegate", objc.GetClass("NSObject"),
			[]objc.MethodDef{
				{Cmd: objc.RegisterName("windowShouldClose:"), Fn: windowShouldClose},
				{Cmd: objc.RegisterName("windowDidResize:"), Fn: windowDidResize},
				{Cmd: objc.RegisterName("windowDidChangeScreen:"), Fn: windowDidChangeScreen},
				{Cmd: objc.RegisterName("windowDidChangeBackingProperties:"), Fn: windowDidChangeBackingProperties},
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
	// whole image; fraction 1.0; hints nil. The op is Copy in the ordinary
	// opaque path; in translucent mode it is SourceOver so the framebuffer's
	// transparent holes (punched over material regions) reveal the effect views
	// composited behind this view.
	op := nsCompositingCopy
	if w.translucent {
		op = nsCompositingSourceOver
	}
	objc.Send[objc.ID](rep, selDrawInRectFull, bounds, nsRect{}, uint(op), 1.0, true, objc.ID(0))
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

func eventMods(event objc.ID) Mods {
	return DecodeMods(uint64(objc.Send[uint64](event, selModifierFlags)))
}

func viewMouseDown(self objc.ID, _ objc.SEL, event objc.ID) {
	w := active
	if w == nil {
		return
	}
	w.buttonHeld = true
	x, y := w.viewCoords(self, event)
	w.dispatch(MapMouseDown(x, y, eventMods(event)))
}

// viewRightMouseDown handles AppKit's -rightMouseDown: — a real right-click, a
// two-finger trackpad tap, or a Control-click, all of which the system delivers
// here. It emits a secondary click so an app can open a context menu; unlike the
// left button it takes no buttonHeld/drag path, because a menu opens on the down
// and there is no secondary-drag gesture to track.
func viewRightMouseDown(self objc.ID, _ objc.SEL, event objc.ID) {
	w := active
	if w == nil {
		return
	}
	x, y := w.viewCoords(self, event)
	w.dispatch(MapSecondaryClick(x, y, eventMods(event)))
}

func viewMouseDragged(self objc.ID, _ objc.SEL, event objc.ID) {
	w := active
	if w == nil {
		return
	}
	x, y := w.viewCoords(self, event)
	w.dispatch(MapMouseMove(x, y, true, eventMods(event)))
}

func viewMouseUp(self objc.ID, _ objc.SEL, event objc.ID) {
	w := active
	if w == nil {
		return
	}
	w.buttonHeld = false
	x, y := w.viewCoords(self, event)
	w.dispatch(MapMouseUp(x, y, eventMods(event)))
}

func viewMouseMoved(self objc.ID, _ objc.SEL, event objc.ID) {
	w := active
	if w == nil {
		return
	}
	x, y := w.viewCoords(self, event)
	w.dispatch(MapMouseMove(x, y, false, eventMods(event)))
}

func viewScrollWheel(self objc.ID, _ objc.SEL, event objc.ID) {
	w := active
	if w == nil {
		return
	}
	x, y := w.viewCoords(self, event)
	dy := float64(objc.Send[float64](event, selScrollingDeltaY))
	w.dispatch(MapScroll(x, y, dy, eventMods(event)))
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
	for _, ev := range MapKey(code, chars, eventMods(event), press) {
		w.dispatch(ev)
	}
}

// windowShouldClose ends the run loop when the user closes the window.
func windowShouldClose(_ objc.ID, _ objc.SEL, _ objc.ID) bool {
	if active != nil {
		active.closed = true
	}
	// -stop: is honoured at the END of the current event cycle, and this runs
	// inside one, so no synthetic wake-up event is needed: Run returns to its
	// caller as soon as this handler does.
	objc.App().Send(selStop, objc.ID(0))
	return true
}

// windowDidResize re-derives the render size from the content view bounds (in
// points) at the current render scale, reallocates the framebuffer and
// re-presents the whole surface. At the default render scale of 1 the
// framebuffer tracks the window's logical point size, so the UI stays readable
// as the window grows or shrinks.
func windowDidResize(_ objc.ID, _ objc.SEL, _ objc.ID) {
	w := active
	if w == nil || w.win == 0 {
		return
	}
	w.updateBacking()
	cv := w.win.Send(selContentView)
	b := objc.Send[nsRect](cv, selBounds)
	w.resize(int(b.Size.W*w.scale), int(b.Size.H*w.scale), w.scale)
}

// viewDidChangeBackingProperties fires when the window moves between displays of
// different pixel density (Retina ↔ non-Retina). The framebuffer is kept in
// LOGICAL points (render scale 1), so its size does not change — only the
// display's up-sampling of that bitmap does — but the recorded backing factor is
// refreshed (diagnostics) and the surface re-presented so AppKit re-samples the
// current frame at the new density.
func viewDidChangeBackingProperties(_ objc.ID, _ objc.SEL) {
	if w := active; w != nil {
		w.followBackingChange()
	}
}

// windowDidChangeScreen fires whenever the window is dragged onto a different
// screen — the reliable signal for a Retina ↔ non-Retina move. The view-level
// -viewDidChangeBackingProperties can miss it (a soft window carries no density
// of its own, and AppKit does not always route that view callback on a bare
// screen move, e.g. an external panel with the lid open), so the NSWindow
// delegate catches the same event a second way and nothing is missed.
func windowDidChangeScreen(_ objc.ID, _ objc.SEL, _ objc.ID) {
	if w := active; w != nil {
		w.followBackingChange()
	}
}

// windowDidChangeBackingProperties is the NSWindow-delegate pendant of the view
// callback, caught in addition to it for the same belt-and-braces reason.
func windowDidChangeBackingProperties(_ objc.ID, _ objc.SEL, _ objc.ID) {
	if w := active; w != nil {
		w.followBackingChange()
	}
}

// followBackingChange refreshes the recorded backing factor after a display
// change and, for a panel-following window, re-scales the framebuffer to the new
// density. Without this a window dragged from a 1x panel to a 2x one kept the
// resolution of whichever display it opened on for the rest of the session,
// silently, since nothing about a soft window says which of the two produced it.
// A window that is not following (a fixed render scale) just re-presents so
// AppKit re-samples the current frame at the new density.
func (w *Window) followBackingChange() {
	w.updateBacking()
	if w.follow && w.backing != w.scale && w.win != 0 {
		w.scale = w.backing
		cv := w.win.Send(selContentView)
		b := objc.Send[nsRect](cv, selBounds)
		w.resize(int(b.Size.W*w.scale), int(b.Size.H*w.scale), w.scale)
		return
	}
	w.presentFull()
}

// updateBacking refreshes the recorded display backing scale factor from the
// window (1 on a non-Retina display, 2 on Retina).
func (w *Window) updateBacking() {
	if w.win == 0 {
		return
	}
	b := float64(objc.Send[float64](w.win, selBackingScaleFactor))
	if b <= 0 {
		b = 1
	}
	w.backing = b
}

// mainScreenVisible returns the main screen's visible frame size in points
// (the usable area, excluding the menu bar and Dock), or (0,0) when no screen is
// available so DefaultContentSize falls back to a fixed size.
func mainScreenVisible() (w, h float64) {
	screen := objc.ID(objc.GetClass("NSScreen")).Send(selMainScreen)
	if screen == 0 {
		return 0, 0
	}
	vf := objc.Send[nsRect](screen, selVisibleFrame)
	return vf.Size.W, vf.Size.H
}

// VisibleScreenSize returns the main screen's usable area (menu bar and Dock
// excluded) in LOGICAL points, with ok=false when no screen is available. It
// loads the AppKit frameworks on first use, so — unlike the internal size
// defaulting in NewScaled — it is safe to call before any window exists: a
// caller can query the screen up front and pass a height back through
// Config.Height, which NewScaled then honours verbatim (no readability clamp is
// applied to an explicit size).
func VisibleScreenSize() (w, h int, ok bool) {
	if err := loadFrameworks(); err != nil {
		return 0, 0, false
	}
	vw, vh := mainScreenVisible()
	if vw <= 0 || vh <= 0 {
		return 0, 0, false
	}
	return int(vw), int(vh), true
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

// RenderScale reports the framebuffer pixels this window allocates per logical
// point. Implements the window.Scaler capability.
func (w *Window) RenderScale() float64 { return w.scale }

// selRepaintNow is a selector of our own on the view class: AppKit has no
// no-argument "redraw yourself" message that can be performed on the main
// thread, and performSelectorOnMainThread: cannot pass the BOOL that
// setNeedsDisplay: wants.
var selRepaintNow = objc.RegisterName("goWidgetsRepaintNow")

var selPerformOnMain = objc.RegisterName("performSelectorOnMainThread:withObject:waitUntilDone:")

// Repaint asks for a frame from any goroutine. Implements the
// window.Repainter capability.
//
// The work is performed on the main thread because that is where AppKit
// requires it, and waitUntilDone is NO so a producer goroutine is never blocked
// by the frame it asked for -- a network fetch should not wait on a paint.
func (w *Window) Repaint() {
	if w == nil || w.view == 0 {
		return
	}
	w.view.Send(selPerformOnMain, selRepaintNow, objc.ID(0), false)
}

// viewRepaintNow is the main-thread half of Repaint.
func viewRepaintNow(_ objc.ID, _ objc.SEL) {
	if active != nil {
		active.paintFrame(false)
	}
}

// windowCanBecomeKey answers YES for every window this package makes, unless it
// asked to be passive. See registerClasses for why a borderless one needs
// telling, and Options.Passive for why one might not want it.
//
// The answer is read from the ACTIVE window rather than from the receiver,
// because this process runs one window at a time -- Run blocks on it -- and a
// method on a registered class has no other way to reach Go state. A window that
// has not been made active yet cannot be asked this question by AppKit.
func windowCanBecomeKey(_ objc.ID, _ objc.SEL) bool {
	return active == nil || !active.passive
}

// selCloseNow is a selector of our own, for the same reason as selRepaintNow:
// the teardown is more than -[NSWindow close] and has to happen in one hop.
var selCloseNow = objc.RegisterName("goWidgetsCloseNow")

// Posting a wake-up event needs the long NSEvent constructor; selPostEvent is
// already declared with the other NSApplication selectors above.
var selOtherEventType = objc.RegisterName(
	"otherEventWithType:location:modifierFlags:timestamp:windowNumber:context:subtype:data1:data2:")

// nsAppKitDefined is NSEventTypeApplicationDefined: an event the application
// invents, which AppKit delivers and nothing acts on.
const nsAppKitDefined = 15

// viewCloseNow is the main-thread half of Close.
//
// It must stop the application as well as close the window. -[NSWindow close]
// does NOT invoke -windowShouldClose:, which is the delegate callback that ends
// the run loop -- that one is only sent for a close the USER initiates. So
// without the -stop: here, Close() tore the window down and left [NSApp run]
// spinning on an empty application, and Run never returned to its caller.
//
// That is not a corner case. A borderless full-screen window has no close
// button and cannot be closed by the user at all, so closing itself is the ONLY
// way such an application can end.
func viewCloseNow(_ objc.ID, _ objc.SEL) {
	w := active
	if w == nil || w.closed {
		return
	}
	w.closed = true
	if w.win != 0 {
		w.win.Send(selClose)
	}
	active = nil
	stopApp()
}

// stopApp ends -[NSApplication run].
//
// -stop: only takes effect at the end of the current event cycle, and it is not
// enough on its own from a run-loop source: with no further event to finish
// processing, the loop can sit idle and never notice. So a do-nothing event is
// posted to wake it, which is the difference between Close returning promptly
// and Close appearing to hang.
func stopApp() {
	app := objc.App()
	app.Send(selStop, objc.ID(0))
	ev := objc.ID(objc.GetClass("NSEvent")).Send(selOtherEventType,
		uint64(nsAppKitDefined), nsPoint{}, uint64(0), 0.0, int64(0), objc.ID(0),
		int16(0), int64(0), int64(0))
	if ev != 0 {
		app.Send(selPostEvent, ev, true)
	}
}

// New creates the NSApplication, an NSWindow with a flipped content view and a
// window delegate, presents an initial blank frame and returns the window ready
// for Run. It must be called on the process main OS thread (the parent Open
// locks it).
func New(title string, width, height int, theme *toolkit.Theme) (*Window, error) {
	return NewScaled(title, width, height, theme, 0)
}

// NewScaled is New with an explicit render scale: framebuffer pixels per
// logical point. 0 keeps the readable default of 1, a negative value follows the
// display's backing factor, and a positive one is used as-is. The parent package
// documents when each is right (window.Config.RenderScale).
func NewScaled(title string, width, height int, theme *toolkit.Theme, renderScale float64) (*Window, error) {
	return NewWithOptions(Options{Title: title, Width: width, Height: height, Theme: theme, RenderScale: renderScale})
}

// NewWithOptions is the real constructor: New and NewScaled are shorthands for
// it. See Options for what placing a window on a chosen screen involves.
func NewWithOptions(o Options) (*Window, error) {
	title, width, height, theme, renderScale := o.Title, o.Width, o.Height, o.Theme, o.RenderScale
	if err := loadFrameworks(); err != nil {
		return nil, err
	}
	// The shared application FIRST, and only then the screens. AppKit refreshes
	// its cached NSScreen list solely for a running application, so a process
	// that listed the displays on its way in -- and every process that chooses a
	// display does exactly that -- is holding an arrangement that may have
	// stopped being true. Creating the application and giving the run loop a
	// turn is what makes AppKit notice; syncAppKitScreens waits for it to.
	//
	// A refusal is not raised when it does not: the placement itself is computed
	// from the window server's own rectangles, so it is right either way. What
	// AppKit's agreement buys is that -[NSWindow screen] and the backing factor
	// name the display the window is actually on.
	app := objc.App()
	policy := activationPolicyReg
	if o.Passive {
		policy = activationPolicyAccessory
	}
	app.Send(selSetActivationPolicy, policy)
	syncAppKitScreens(appKitScreenSyncTimeout)

	screen, err := o.resolveScreen()
	if err != nil {
		return nil, err
	}
	vc, dc, err := registerClasses()
	if err != nil {
		return nil, err
	}
	// No explicit size → pick a readable default from the main screen's visible
	// frame (a fraction of the usable area, clamped), so a defaulted window is
	// legible without the caller having to size it. Width/Height are LOGICAL
	// points; a desktop shell can pass its own point size through Config.
	if width <= 0 || height <= 0 {
		width, height = DefaultContentSize(mainScreenVisible())
	}
	if theme == nil {
		theme = toolkit.DefaultDark()
	}

	rect := nsRect{Size: nsSize{W: float64(width), H: float64(height)}}
	style := framedStyle(o.FixedSize)
	// A fullscreen placement is a BORDERLESS window at the screen's own frame,
	// as the window server reports it. It is deliberately not macOS native full
	// screen, which animates into its own Space and keeps a menu bar -- wrong
	// for an immersive surface that must own every pixel of the panel.
	// topLeft, when non-zero, is where the finished window's FRAME is put once
	// AppKit has told us how big its chrome is (see below).
	var topLeft *nsPoint
	if screen != nil {
		// The screen's rectangle in AppKit's own space, derived from the window
		// server's live bounds. Both branches below need it, and neither may
		// derive it any other way: an origin computed from a display list that
		// has stopped being true is how a window ends up on somebody else's
		// panel with nothing reported.
		primary, err := primaryBounds()
		if err != nil {
			return nil, err
		}
		frame := screen.appKitFrame(primary.H)
		if o.Fullscreen {
			rect = frame
			style = styleBorderless
			width, height = int(rect.Size.W), int(rect.Size.H)
		} else {
			// Not fullscreen, but still on a chosen display: keep the requested
			// size and put the window in the display's top-left usable corner.
			//
			// Its FRAME, not its content. A titled window's title bar sits ABOVE
			// its content, so a content rectangle at the display's top edge puts
			// the title bar past it -- and AppKit, which will not leave a title
			// bar where it cannot be grabbed, then moves the whole window
			// somewhere it fits. Which was, in the measured case, the main
			// display: the caller chose a screen and got the desktop's.
			//
			// The chrome's height is AppKit's to know and it is only knowable
			// once the window exists, so the placement is finished after
			// creation with -setFrameTopLeftPoint:.
			p := screen.visibleTopLeftInAppKit(primary.H)
			topLeft = &p
			rect.Origin = nsPoint{
				X: frame.Origin.X,
				Y: frame.Origin.Y + frame.Size.H - float64(height),
			}
		}
	}
	// GoWidgetsWindow rather than NSWindow: it is the subclass that answers YES
	// to -canBecomeKeyWindow, without which a borderless window gets no keys and
	// no pointer motion.
	win := objc.ID(windowClass).Send(selAlloc).
		Send(selInitContentRect, rect, style, backingStoreBuffered, false)
	win.Send(selRetain)

	view := objc.ID(vc).Send(selAlloc).Send(selInitWithFrame, rect)
	view.Send(selRetain)
	win.Send(selSetContentView, view)
	// Ask AppKit to deliver -mouseMoved: as the pointer moves over the window,
	// not just while a button is held. Without this a hover never reaches the
	// view, so widgets that highlight what the pointer is over (a context menu's
	// rows, hoverable controls) stay dark and wheel routing has no fresh pointer
	// position between notches.
	win.Send(selSetAcceptsMouseMoved, true)

	deleg := objc.ID(dc).Send(selAlloc).Send(selInit)
	deleg.Send(selRetain)
	win.Send(selSetDelegate, deleg)

	backing := float64(objc.Send[float64](win, selBackingScaleFactor))
	if backing <= 0 {
		backing = 1
	}
	// Render scale: 1 (logical points) so the UI is presented at a readable size;
	// the framebuffer is up-sampled to the backing resolution by AppKit. An
	// override (the crisp-HiDPI seam / legibility proof) forces a different scale.
	scale := 1.0
	switch {
	case renderScale < 0:
		// Follow the panel: the caller has told us its root composes its own
		// pixels at whatever size it is handed.
		scale = backing
	case renderScale > 0:
		scale = renderScale
	case renderScaleOverride > 0:
		scale = renderScaleOverride
	}
	w := &Window{
		title:   title,
		theme:   theme,
		follow:  renderScale < 0,
		win:     win,
		view:    view,
		w:       int(float64(width) * scale),
		h:       int(float64(height) * scale),
		scale:   scale,
		backing: backing,
		passive: o.Passive,
	}
	w.buf = make([]byte, 4*w.w*w.h)
	active = w
	if o.Passive {
		// The pointer goes straight through to whatever is behind, and to the
		// display underneath: a picture is not something to click on.
		//
		// canBecomeKeyWindow already answers NO for this window, but AppKit only
		// asks that when something tries to MAKE it key -- a click on it would
		// otherwise still arrive as a mouse event, and the desk would act on a
		// press meant for the application behind it.
		win.Send(objc.RegisterName("setIgnoresMouseEvents:"), true)
		// And it must not steal the keyboard when it appears. An accessory
		// application cannot become active, which is the whole of that.
		win.Send(objc.RegisterName("setCanHide:"), false)
	}

	win.Send(selSetTitle, objc.NSString(title))
	if o.Immersive {
		// NSStatusWindowLevel is 25: one above the menu bar's 24 and five above
		// the Dock's 20, and below the levels the system keeps for its own
		// alerts. High enough to cover the furniture, not so high as to cover a
		// dialogue the viewer needs to answer.
		const nsStatusWindowLevel = 25
		win.Send(selSetLevel, nsStatusWindowLevel)

		// AND IT STAYS PUT WHEN THE SPACE CHANGES.
		//
		// Without this a window belongs to the ONE Space it was opened in, and
		// macOS slides it away whenever that display changes Space -- which
		// clicking a desktop can do. What a viewer sees is the picture sliding
		// off to the side and the real desktop underneath it, which is exactly
		// how it was reported: "on voit que tu superposes une image sur le
		// bureau car elle glisse sur le cote".
		//
		// An immersive window is not a document. It is a surface put over a
		// display on purpose, so it joins every Space and does not move with
		// them, and it is allowed over a full-screen application rather than
		// being hidden by one.
		//
		// canJoinAllSpaces 1<<0, stationary 1<<4, fullScreenAuxiliary 1<<8.
		const stays = 1 | 1<<4 | 1<<8
		win.Send(selSetCollectionBehavior, uint(stays))
	}
	if topLeft != nil {
		win.Send(selSetFrameTopLeftPoint, *topLeft)
	}
	if screen == nil {
		// Centring is the right default, but on a chosen screen it would undo the
		// placement by pulling the window back to the main display.
		win.Send(selCenter)
	}
	win.Send(selMakeKeyAndOrderFront, objc.ID(0))
	win.Send(selMakeFirstResponder, view)
	app.Send(selActivateIgnoring, true)
	return w, nil
}

// Run binds root, performs the initial layout+present, then pumps NSEvents into
// the widget tree until the window is closed. It is the macOS analogue of the
// X11 backend's event loop.
// Run binds root and hands control to AppKit's own run loop.
//
// It used to pump events by hand — nextEventMatchingMask + sendEvent +
// updateWindows in a loop — which delivers input and repaints correctly and is
// NOT enough: an application that never goes through -[NSApplication run]
// never completes its launch, and is therefore never registered with the
// ACCESSIBILITY system. Measured: AXUIElementCreateApplication reported the
// process as having no windows at all, however many were on screen, while a
// control that does call -run exposed its window and its whole tree.
//
// Nothing else changes. Presentation already happens in -drawRect: and input
// already arrives through the NSView methods, so -run drives exactly the same
// code the manual pump did — it simply also performs the launch handshake, the
// window-server registration and the per-cycle housekeeping that AppKit
// expects and that no application can usefully reimplement.
func (w *Window) Run(root toolkit.Widget) error {
	w.bindAndSeed(root)
	objc.App().Send(selRun)
	return nil
}

// bindAndSeed binds root and paints+presents the initial full frame. When the
// root is incremental, the first frame is drawn through it so its whole-surface
// seed damage is consumed here — the first interaction is then already a purely
// incremental frame.
func (w *Window) bindAndSeed(root toolkit.Widget) {
	w.root = root
	w.dmg, _ = root.(damageRenderer)
	if w.dnd == nil {
		w.dnd = dnd.New()
	}
	w.dnd.Bind(root)
	// First layout pass so every Material learns its bounds.
	if w.dmg != nil {
		w.drawIncremental()
	} else {
		w.draw()
	}
	// Install native vibrancy views behind any Material, then repaint so the
	// framebuffer holes are punched and each material renders its native
	// (child-only) path. A tree with no Material leaves everything unchanged.
	w.syncMaterials(root)
	// ⛔ REPAINT IF A CONTROL WAS JUST CLAIMED. A toolkit.Native paints its
	// Fallback until a host claims it, and the claim happens INSIDE syncNative
	// -- after the frame above painted the fallback. Without this, an opaque
	// window kept those pixels for good, with the real AppKit control
	// composited on top: two buttons, one over the other, their labels a pixel
	// apart. Seen in a settings window converted to native controls.
	claimed := w.syncNative(root)
	if w.translucent || claimed {
		// A FULL draw when a control was just claimed, never the incremental
		// one: nothing marked the region damaged, so an incremental pass would
		// leave exactly the pixels this exists to clear.
		if w.dmg != nil && !claimed {
			w.drawIncremental()
		} else {
			w.draw()
		}
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
	w.punchHoles()
}

// drawIncremental lays the root out to the full client area and repaints ONLY
// the damage the root reports, returning the repainted rectangles.
func (w *Window) drawIncremental() []toolkit.Rect {
	w.mu.Lock()
	defer w.mu.Unlock()
	p := painter.NewPixelPainter(w.buf, w.w, w.h)
	w.root.SetBounds(toolkit.Rect{X: 0, Y: 0, W: w.w, H: w.h})
	rects := w.dmg.RenderDamaged(p, w.theme)
	w.punchHoles()
	return rects
}

// paintFrame renders and presents one frame after an event or resize. A plain
// root repaints+re-presents the whole surface; an incremental root invalidates
// and re-presents only its damaged rectangles — except after a resize, where the
// framebuffer was reallocated (whole-surface damage) and the full surface is
// presented.
func (w *Window) paintFrame(resize bool) {
	// The frame and the tree a screen reader reads are published from the same
	// place, so the description cannot lag the pixels a sighted user sees.
	//
	// AFTER the draw, not before. A widget tree describes itself the same either
	// way, but a root that RENDERS ITS OWN PIXELS -- a toolkit.Surface over an
	// application's scene -- only knows what it is showing once it has been
	// asked to show it. Refreshing first published the PREVIOUS frame's tree,
	// which on the first paint is no tree at all: such an application came up
	// with an empty accessibility tree and, with nothing repainting it while
	// idle, kept one.
	defer w.refreshA11y(resize)
	// Reconcile embedded native controls with the freshly laid-out tree, so one
	// tracks its Native through scrolling and interaction. After the draw (this
	// is deferred), for the same reason the a11y refresh is.
	//
	// ⛔ AND ONE MORE FRAME WHEN A CONTROL IS CLAIMED. The Native painted its
	// Fallback into the frame that just went out; the claim happens here, and
	// nothing marks the region damaged -- so without this the fallback's pixels
	// stay under the real control for as long as the window is idle. Once per
	// control, not per frame: only a control that was CREATED costs a repaint.
	defer func() {
		if w.syncNative(w.root) {
			w.draw()
			w.presentFull()
		}
	}()
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

// resize grows/shrinks the framebuffer to the new render size (points × render
// scale) and re-presents.
func (w *Window) resize(nw, nh int, scale float64) {
	if nw <= 0 || nh <= 0 || (nw == w.w && nh == w.h) {
		return
	}
	w.mu.Lock()
	w.w, w.h, w.scale = nw, nh, scale
	w.buf = make([]byte, 4*nw*nh)
	w.mu.Unlock()
	if w.translucent {
		// Relayout at the new size so the materials report fresh bounds, rebuild
		// the effect views over them, then repaint so the holes match, and blit.
		w.draw()
		w.syncMaterials(w.root)
		w.syncNative(w.root)
		w.draw()
		w.presentFull()
		return
	}
	w.paintFrame(true)
}

// Size returns the current client size in framebuffer (render) pixels — equal to
// the window's logical point size at the default render scale of 1.
func (w *Window) Size() (int, int) { return w.w, w.h }

// Bounds reports where the window ACTUALLY is: its rectangle on the desktop, in
// the same global top-left coordinates [ScreenInfo] uses, with ok=false when
// the display arrangement cannot be read.
//
// "The window opened" and "the window opened where it was asked to" are
// different claims, and only the first of them used to be checkable from
// outside this package. An application that takes over a display -- an XR
// headset, a kiosk, a presentation -- needs the second, and so does any test
// that wants to assert placement without photographing somebody's desktop.
//
// It reads -[NSWindow frame], which is the window's own rectangle and is
// therefore correct whatever AppKit currently believes about the display list,
// and converts it with the window server's live primary height. Call it from
// the goroutine that opened the window: it messages AppKit.
func (w *Window) Bounds() (x, y, width, height int, ok bool) {
	primary, err := primaryBounds()
	if err != nil {
		return 0, 0, 0, 0, false
	}
	f := objc.Send[nsRect](w.win, selFrame)
	return int(f.Origin.X), int(flipY(f.Origin.Y, f.Size.H, primary.H)),
		int(f.Size.W), int(f.Size.H), true
}

// Close releases the window. Safe to call more than once.
// Close closes the window. Safe from any goroutine.
//
// AppKit may only be touched from the main thread, so the work is performed
// there -- an application closing its window from a fetch's completion, a menu
// handler or a shutdown path would otherwise be sending -close from whichever
// thread happened to be running. waitUntilDone is NO for the same reason as in
// Repaint: the caller asked for the window to go, not to be blocked until it
// has gone.
func (w *Window) Close() error {
	if w == nil || w.view == 0 {
		return nil
	}
	w.view.Send(selPerformOnMain, selCloseNow, objc.ID(0), false)
	return nil
}

// String identifies the window for debugging.
func (w *Window) String() string {
	return fmt.Sprintf("cocoa.Window(%dx%d %q)", w.w, w.h, w.title)
}

// framedStyle is the AppKit style mask for an ordinary framed window: titled,
// closable, miniaturisable, and resizable unless the caller asked for a fixed
// size.
//
// Without the resizable mask AppKit gives no resize control, refuses a drag on
// any edge, and disables the zoom button -- which is the whole of "this window
// is the size it is". It is a pure function so the decision can be checked
// without a display; the mask itself only means anything to AppKit.
func framedStyle(fixed bool) uint {
	style := uint(styleTitled | styleClosable | styleMiniaturizable | styleResizable)
	if fixed {
		style &^= styleResizable
	}
	return style
}

// Number is the window's system-wide identifier — -[NSWindow windowNumber],
// which on macOS IS the CGWindowID that CoreGraphics and ScreenCaptureKit use.
//
// It exists so a program can tell a capture about its own window. Capturing a
// display that this window sits on otherwise feeds the window back into itself:
// an overlay filming the screen it covers. ScreenCaptureKit takes a list of
// window ids to leave out, and this is the only way a consumer can name one it
// did not create itself.
//
// Zero means the window has no number yet, which is what an unopened or already
// closed one answers.
func (w *Window) Number() uint32 {
	if w == nil || w.win == 0 {
		return 0
	}
	return uint32(objc.Send[int64](w.win, selWindowNumber))
}
