// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux && !android

// Package gtk is the GTK4-hosted Linux back-end: instead of blitting to a
// from-scratch X11/Wayland surface, GTK owns the window, the toolkit's pixel
// framebuffer goes in a GtkPicture, and native platform controls
// (toolkit.NativeControl) are real GtkEntry/GtkButton/… overlaid above it in a
// GtkFixed. It links no cgo — everything is github.com/go-gtk/gtk4 over purego.
//
// It is the third native-control host, the sibling of internal/cocoa and
// internal/win32, and runs the same NativeControl descriptor reconcile
// (syncNative) they do — but here the widgets are GTK's, so a radio group finally
// groups properly rather than sharing one global action.
package gtk

import (
	"errors"
	"sync/atomic"

	"github.com/go-gtk/gtk4"
	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// Window is the GTK4-hosted back-end window. It satisfies the window.Backend
// contract (Run/Close/Size/String).
type Window struct {
	win   gtk4.Widget
	fixed gtk4.Widget
	pic   gtk4.Picture
	loop  gtk4.MainLoop

	root  toolkit.Widget
	theme *toolkit.Theme
	w, h  int
	scale float64
	buf   []byte

	native map[string]*liveControl
	dirty  atomic.Bool
}

// Open creates the GTK window (but does not enter the loop; Run does). width and
// height are logical points; scale is framebuffer pixels per point.
func Open(title string, width, height int, theme *toolkit.Theme, scale float64) (*Window, error) {
	if theme == nil {
		theme = toolkit.DefaultDark()
	}
	if scale <= 0 {
		scale = 1
	}
	ok, err := gtk4.Init()
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, errors.New("gtk: no display (gtk_init_check == false)")
	}
	win := gtk4.WindowNew()
	win.SetTitle(title)
	win.SetDefaultSize(width, height)
	fixed := gtk4.FixedNew()
	pic := gtk4.PictureNew()
	// A GtkFixed gives each child its requested size, and a GtkPicture with no
	// content requests nothing — so without this the framebuffer would be laid out
	// at 0×0 and never seen. Size it to the logical window; the picture scales its
	// (hi-dpi) texture down to fit.
	pic.Widget().SetSizeRequest(width, height)
	fixed.Put(pic.Widget(), 0, 0)
	win.SetChild(fixed)

	pxW, pxH := int(float64(width)*scale), int(float64(height)*scale)
	return &Window{
		win: win, fixed: fixed, pic: pic,
		theme: theme, w: pxW, h: pxH, scale: scale,
		buf:    make([]byte, pxW*pxH*4),
		native: map[string]*liveControl{},
	}, nil
}

// Run binds root and drives the GLib main loop until the window is closed. It
// presents a frame only when one was asked for — see [Window.Repaint].
func (w *Window) Run(root toolkit.Widget) error {
	w.root = root
	w.loop = gtk4.MainLoopNew()
	w.win.Connect("close-request", func() { w.loop.Quit() })
	w.win.Present()
	w.dirty.Store(true) // draw the first frame once the clock starts (post-map)
	// The frame clock is the main-thread pump that turns a Repaint request into a
	// present. It ticks while the window is mapped (and only then), and each tick
	// draws ONLY if a repaint was asked for since the last one — so an idle window
	// costs a flag read per frame, not a full layout + texture upload + recompose.
	// This is the same policy the Cocoa back-end runs (present on request, not on a
	// timer): go-widgets/application's loop calls Repaint at up to 60 Hz gated by
	// the handler's NeedsPresent, so nothing is drawn while nothing changes. A
	// single persistent callback avoids exhausting purego's callback table.
	w.win.AddTickCallback(func() bool {
		if w.dirty.Swap(false) {
			w.frame()
		}
		return true
	})
	w.loop.Run()
	return nil
}

// Repaint asks for a frame from any goroutine. It implements
// [github.com/go-widgets/window.Repainter]: the application present loop calls it
// (gated by the handler's NeedsPresent), and a background producer that has queued
// a scene change may call it directly. It only raises a flag the frame clock reads
// on the main thread, so it makes no GTK call off that thread and never allocates
// a callback — cheap enough to call every tick.
func (w *Window) Repaint() { w.dirty.Store(true) }

// Close quits the loop and drops the window.
func (w *Window) Close() error {
	if w.loop != 0 {
		w.loop.Quit()
	}
	return nil
}

// Size returns the current framebuffer size in pixels.
func (w *Window) Size() (int, int) { return w.w, w.h }

// String identifies the window.
func (w *Window) String() string { return "gtk4-hosted window" }

// frame lays the root out, presents its pixels in the picture, and reconciles the
// native controls over them.
func (w *Window) frame() {
	if w.root == nil {
		return
	}
	p := painter.NewPixelPainter(w.buf, w.w, w.h)
	full := toolkit.Rect{X: 0, Y: 0, W: w.w, H: w.h}
	p.FillRect(full, w.theme.Background)
	w.root.SetBounds(full)
	w.root.Draw(p, w.theme)
	w.pic.SetRGBA(w.buf, w.w, w.h)
	w.syncNative(w.root)
}
