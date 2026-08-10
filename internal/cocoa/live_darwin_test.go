// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin && integration

// This is the live macOS proof. It runs only under -tags=integration with
// WINDOW_COCOA_INTEGRATION set. It opens a REAL NSWindow showing a few
// go-widgets widgets (VBox + Labels + a Button), renders the view on-device
// through AppKit, captures the rendered pixels two ways — a permission-free
// -[NSView cacheDisplayInRect:toBitmapImageRep:] render (the primary assertion,
// which works headless) and a best-effort `screencapture -l<windowNumber>`
// true on-screen shot (saved as an artifact) — and asserts sampled pixels
// (theme background present, the button face differs from the background).
//
// It then SYNTHESISES real NSEvents (a left mouse down+up at the button centre
// and a key press) and posts them through NSApp, so they flow through the real
// AppKit event dispatch into the view's mouseDown:/keyUp: callbacks and the
// sovereign mapping.go decode, and asserts BOTH the toolkit.Event dispatched to
// the root AND that the button's OnClick counter went 0→1.
//
// All AppKit work runs on the process MAIN OS thread (TestMain reserves it and
// callOnMain funnels the window work there), as NSApplication/NSWindow require.
package cocoa

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"runtime"
	"testing"
	"unsafe"

	objc "github.com/go-macos/objc"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// --- main-thread plumbing --------------------------------------------------

var mainfuncs = make(chan func())

func TestMain(m *testing.M) {
	runtime.LockOSThread()
	go func() {
		code := m.Run()
		// Drain any late submission then exit from the test goroutine.
		os.Exit(code)
	}()
	for f := range mainfuncs {
		f()
	}
}

// callOnMain runs f on the reserved main OS thread and blocks until it returns.
func callOnMain(f func()) {
	done := make(chan struct{})
	mainfuncs <- func() {
		f()
		close(done)
	}
	<-done
}

// --- a recording root that also drives a real widget tree ------------------

// recordingRoot draws a real go-widgets VBox and records every toolkit.Event
// delivered to it while forwarding it to the tree (so the Button still fires).
type recordingRoot struct {
	toolkit.Base
	inner  toolkit.Widget
	events []toolkit.Event
}

func (r *recordingRoot) SetBounds(b toolkit.Rect) {
	r.Base.SetBounds(b)
	r.inner.SetBounds(b)
}
func (r *recordingRoot) Draw(p painter.Painter, th *toolkit.Theme) { r.inner.Draw(p, th) }
func (r *recordingRoot) OnEvent(ev toolkit.Event) {
	r.events = append(r.events, ev)
	r.inner.OnEvent(ev)
}
func (r *recordingRoot) has(kind toolkit.EventKind, code string) bool {
	for _, e := range r.events {
		if e.Kind == kind && (code == "" || e.Code == code) {
			return true
		}
	}
	return false
}

// --- selectors used only by the live test ----------------------------------

var (
	selBitmapForCaching = objc.RegisterName("bitmapImageRepForCachingDisplayInRect:")
	selCacheDisplay     = objc.RegisterName("cacheDisplayInRect:toBitmapImageRep:")
	selRepUsingType     = objc.RegisterName("representationUsingType:properties:")
	selBytes            = objc.RegisterName("bytes")
	selLength           = objc.RegisterName("length")
)

const nsBitmapFileTypePNG = 4 // NSBitmapImageFileTypePNG

// renderViewPNG renders the view's current content into an NSBitmapImageRep via
// cacheDisplayInRect: (no window-server round-trip, no Screen-Recording
// permission) and returns its PNG encoding. This is the on-device capture of
// exactly what AppKit drew from our framebuffer.
func renderViewPNG(view objc.ID) ([]byte, error) {
	bounds := objc.Send[nsRect](view, selBounds)
	rep := view.Send(selBitmapForCaching, bounds)
	if rep == 0 {
		return nil, fmt.Errorf("bitmapImageRepForCachingDisplayInRect: returned nil")
	}
	view.Send(selCacheDisplay, bounds, rep)
	data := rep.Send(selRepUsingType, uint(nsBitmapFileTypePNG), objc.ID(0))
	if data == 0 {
		return nil, fmt.Errorf("representationUsingType: returned nil")
	}
	n := int(data.Send(selLength))
	if n <= 0 {
		return nil, fmt.Errorf("PNG NSData empty")
	}
	ptr := objc.Send[unsafe.Pointer](data, selBytes)
	return append([]byte(nil), unsafe.Slice((*byte)(ptr), n)...), nil
}

// --- NSEvent synthesis ------------------------------------------------------

var (
	selMouseFactory = selMouseEventFactory
	selKeyFactory   = selKeyEventFactory
)

// postMouse posts a real left-mouse NSEvent (window/base coords, bottom-left
// origin) to NSApp and pumps it through dispatch.
func (w *Window) postMouse(app objc.ID, evtType int, loc nsPoint) {
	wn := int(w.win.Send(selWindowNumber))
	ev := objc.ID(objc.GetClass("NSEvent")).Send(selMouseFactory,
		uint(evtType), loc, uint(0), 0.0, wn, objc.ID(0), 0, 1, 1.0)
	app.Send(selPostEvent, ev, false)
	w.pumpPending(app)
}

// postKey posts a real key NSEvent carrying characters/keyCode and pumps it.
func (w *Window) postKey(app objc.ID, evtType int, chars string, keyCode uint16) {
	wn := int(w.win.Send(selWindowNumber))
	s := objc.NSString(chars)
	ev := objc.ID(objc.GetClass("NSEvent")).Send(selKeyFactory,
		uint(evtType), nsPoint{}, uint(0), 0.0, wn, objc.ID(0), s, s, false, keyCode)
	app.Send(selPostEvent, ev, false)
	w.pumpPending(app)
}

// pumpPending drains and dispatches every queued NSEvent without blocking.
func (w *Window) pumpPending(app objc.ID) {
	past := objc.ID(objc.GetClass("NSDate")).Send(selDistantPast)
	mode := objc.NSString(defaultRunLoopModeName)
	for {
		ev := app.Send(selNextEvent, uint64(eventMaskAny), past, mode, true)
		if ev == 0 {
			break
		}
		app.Send(selSendEvent, ev)
	}
	app.Send(selUpdateWindows)
}

// spin runs the main run loop briefly so the window appears and composites.
func spin(seconds float64) {
	rl := objc.ID(objc.GetClass("NSRunLoop")).Send(objc.RegisterName("currentRunLoop"))
	until := objc.ID(objc.GetClass("NSDate")).Send(objc.RegisterName("dateWithTimeIntervalSinceNow:"), seconds)
	rl.Send(objc.RegisterName("runUntilDate:"), until)
}

// --- the test ---------------------------------------------------------------

func TestLiveCocoaWindow(t *testing.T) {
	if os.Getenv("WINDOW_COCOA_INTEGRATION") == "" {
		t.Skip("set WINDOW_COCOA_INTEGRATION=1 to run the live macOS window proof")
	}

	theme := toolkit.DefaultDark()
	var (
		win     *Window
		root    *recordingRoot
		btn     *toolkit.Button
		clicks  int
		pngData []byte
		btnRect toolkit.Rect
		setupErr error
	)

	callOnMain(func() {
		win, setupErr = New("go-widgets/window cocoa proof", 480, 320, theme)
		if setupErr != nil {
			return
		}
		vbox := toolkit.NewVBox()
		vbox.Append(toolkit.NewLabel("Hello from go-widgets/window"))
		vbox.Append(toolkit.NewLabel("Pure-Go macOS Cocoa backend — no cgo"))
		lbl := toolkit.NewLabel("clicks: 0")
		btn = toolkit.NewButton("Click me", func() {
			clicks++
			lbl.Text = fmt.Sprintf("clicks: %d", clicks)
		})
		vbox.Append(btn)
		vbox.Append(lbl)
		root = &recordingRoot{inner: vbox}

		win.bindAndSeed(root)
		spin(0.4) // let the window map + composite
		win.presentFull()

		// On-device capture of what AppKit rendered from our framebuffer.
		pngData, setupErr = renderViewPNG(win.view)
		if setupErr != nil {
			return
		}
		btnRect = btn.Bounds()

		app := objc.ID(objc.GetClass("NSApplication")).Send(selSharedApplication)

		// Synthesise a click at the button centre. btnRect is in device pixels
		// (top-left); convert its centre to window/base points (bottom-left).
		cx := float64(btnRect.X+btnRect.W/2) / win.scale
		cyTop := float64(btnRect.Y+btnRect.H/2) / win.scale
		viewHpts := float64(win.h) / win.scale
		loc := nsPoint{X: cx, Y: viewHpts - cyTop}
		win.postMouse(app, evtLeftMouseDown, loc)
		win.postMouse(app, evtLeftMouseUp, loc)

		// Synthesise a printable key 'a' (keyCode 0 on ANSI → printable path).
		win.postKey(app, evtKeyDown, "a", 0)
		win.postKey(app, evtKeyUp, "a", 0)

		// Best-effort true on-screen screenshot artifact (needs Screen Recording
		// permission; ignored if it fails / is unavailable, e.g. headless CI).
		trySystemScreencapture(win)
	})

	if setupErr != nil {
		t.Fatalf("window setup/capture failed: %v", setupErr)
	}

	// --- pixel assertions on the on-device render ---
	img, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		t.Fatalf("decode rendered PNG: %v", err)
	}
	b := img.Bounds()
	if b.Dx() < 100 || b.Dy() < 100 {
		t.Fatalf("rendered image too small: %v", b)
	}
	// Save the captured render as a dated artifact.
	out := "cocoa-capture-2026-08-10.png"
	if werr := os.WriteFile(out, pngData, 0o644); werr != nil {
		t.Logf("could not save %s: %v", out, werr)
	} else {
		t.Logf("saved on-device render to %s (%dx%d)", out, b.Dx(), b.Dy())
	}

	// The background must appear (top-left corner is above the widget content).
	bg := theme.Background
	cornerR, cornerG, cornerB, _ := img.At(b.Min.X+2, b.Min.Y+2).RGBA()
	if !near(uint8(cornerR>>8), bg.R) || !near(uint8(cornerG>>8), bg.G) || !near(uint8(cornerB>>8), bg.B) {
		t.Fatalf("top-left corner (%d,%d,%d) not near theme background (%d,%d,%d)",
			cornerR>>8, cornerG>>8, cornerB>>8, bg.R, bg.G, bg.B)
	}
	// The button face must differ from the background (the button drew).
	sx := b.Min.X + int(float64(btnRect.X+btnRect.W/2)*float64(b.Dx())/float64(win.w))
	sy := b.Min.Y + int(float64(btnRect.Y+btnRect.H/2)*float64(b.Dy())/float64(win.h))
	pr, pg, pb, _ := img.At(sx, sy).RGBA()
	if near(uint8(pr>>8), bg.R) && near(uint8(pg>>8), bg.G) && near(uint8(pb>>8), bg.B) {
		t.Fatalf("button centre pixel (%d,%d) = (%d,%d,%d) equals background — button did not paint",
			sx, sy, pr>>8, pg>>8, pb>>8)
	}
	if allUniform(img) {
		t.Fatalf("rendered image is a single flat colour — nothing painted")
	}

	// --- input round-trip assertions ---
	if clicks != 1 {
		t.Fatalf("button OnClick counter = %d after one synthesised click, want 1", clicks)
	}
	if !root.has(toolkit.EventClick, "") {
		t.Fatalf("no EventClick dispatched to root; got %+v", root.events)
	}
	if !root.has(toolkit.EventKeyDown, "a") {
		t.Fatalf("no EventKeyDown 'a' dispatched to root; got %+v", root.events)
	}
	if !root.has(toolkit.EventChar, "a") {
		t.Fatalf("no EventChar 'a' dispatched to root; got %+v", root.events)
	}
	if !root.has(toolkit.EventKeyUp, "a") {
		t.Fatalf("no EventKeyUp 'a' dispatched to root; got %+v", root.events)
	}

	callOnMain(func() { _ = win.Close() })
}

// trySystemScreencapture attempts a real window-server screenshot of the window
// and saves it; failures (missing permission / headless) are non-fatal.
func trySystemScreencapture(w *Window) {
	wn := int(w.win.Send(selWindowNumber))
	out := "cocoa-screencapture-2026-08-10.png"
	_ = exec.Command("screencapture", "-x", "-o", "-l", fmt.Sprintf("%d", wn), out).Run()
}

func near(a, b uint8) bool {
	d := int(a) - int(b)
	if d < 0 {
		d = -d
	}
	return d <= 24
}

func allUniform(img image.Image) bool {
	b := img.Bounds()
	r0, g0, b0, _ := img.At(b.Min.X, b.Min.Y).RGBA()
	for y := b.Min.Y; y < b.Max.Y; y += 7 {
		for x := b.Min.X; x < b.Max.X; x += 7 {
			r, g, bb, _ := img.At(x, y).RGBA()
			if r != r0 || g != g0 || bb != b0 {
				return false
			}
		}
	}
	return true
}
