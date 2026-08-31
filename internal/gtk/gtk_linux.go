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

// Run binds root and drives the GLib main loop until the window is closed,
// presenting a fresh frame every vsync from the window's frame clock.
func (w *Window) Run(root toolkit.Widget) error {
	w.root = root
	w.loop = gtk4.MainLoopNew()
	w.win.Connect("close-request", func() { w.loop.Quit() })
	w.win.Present()
	// Present each frame on the frame clock, not once. The root is an
	// application's own Surface — an immediate-mode scene whose pixels change as
	// data loads, a caret blinks, a list scrolls — so a single present would
	// freeze whatever existed before the first layout (and before the window was
	// even mapped, the frame clock has not started). AddTickCallback fires on the
	// window's GdkFrameClock while it is mapped and is quiescent when it is not,
	// so an idle or hidden window costs nothing; syncNative inside frame()
	// reconciles the overlaid controls by key, so ticking never rebuilds them or
	// fights a caret. It stops when the window closes (loop quits, widget
	// unmaps).
	w.win.AddTickCallback(func() bool {
		w.frame()
		return true
	})
	w.loop.Run()
	return nil
}

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
