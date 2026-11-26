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
	box   gtk4.Widget // win's child: the menu bar (when there is one) above overlay
	fixed gtk4.Widget
	// overlay is the window's child: the picture, with fixed above it. Its
	// allocation is the drawable area, in points.
	overlay gtk4.Widget
	pic     gtk4.Picture
	loop    gtk4.MainLoop

	root  toolkit.Widget
	theme *toolkit.Theme
	w, h  int
	scale float64
	buf   []byte
	sf    *gtk4.SharedFrame // buf's memory, handed to GTK without a copy; nil: buf is Go memory
	fr    framer            // whole versus damaged present

	native  map[string]*liveControl
	dirty   atomic.Bool
	btnDown bool           // a mouse button is held: OnMotion is a drag, not a move
	im      gtk4.IMContext // the input method in front of the key controller; 0: none

	hideOnClose atomic.Bool // the user's close hides the window instead of ending Run
	closing     atomic.Bool // Close was called or Run has returned: the window is going

	mb menuBarState // the application menu bar (menubar_linux.go)

	// Secondary windows (child_linux.go): child marks one that shares its
	// parent's loop, children are the ones this window owns, onClose is called
	// once when a child goes away.
	child    bool
	parent   *Window
	children []*Window
	onClose  func()
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
	// The picture is the overlay's main child, so it takes the whole window and
	// follows its resizes; the GtkFixed of native controls rides above it. (A
	// GtkFixed child gets only its requested size, which for a picture with no
	// content is 0×0, and a fixed request would pin the window to its first size.)
	overlay := gtk4.OverlayNew()
	overlay.SetOverlayChild(pic.Widget())
	overlay.AddOverlay(fixed)
	// The window's child is a vertical box so SetMenuBar can put a bar above the
	// overlay; the overlay takes whatever the bar leaves.
	overlay.SetExpand(true, true)
	box := gtk4.BoxNew(false)
	box.Append(overlay)
	win.SetChild(box)

	pxW, pxH := int(float64(width)*scale), int(float64(height)*scale)
	w := &Window{
		win: win, box: box, fixed: fixed, overlay: overlay, pic: pic,
		theme: theme, w: pxW, h: pxH, scale: scale,
		native: map[string]*liveControl{},
	}
	w.buf, w.sf = newFrameBuffer(pxW, pxH)
	return w, nil
}

// newFrameBuffer returns the pixels of a w×h framebuffer: GLib memory GTK can
// show without a copy when it can be had (sf non-nil, buf its view), else Go
// memory that every present copies. A partial present needs the texture's bytes
// to span the whole frame (gdk_memory_texture_builder_build refuses less), so the
// copy cannot be narrowed to the damage; sharing the buffer is what removes it.
func newFrameBuffer(w, h int) ([]byte, *gtk4.SharedFrame) {
	if sf := gtk4.NewSharedFrame(w, h); sf != nil {
		return sf.Pix(), sf
	}
	return make([]byte, w*h*4), nil
}

// Run binds root and drives the GLib main loop until the window is closed. It
// presents a frame only when one was asked for — see [Window.Repaint].
func (w *Window) Run(root toolkit.Widget) error {
	w.loop = gtk4.MainLoopNew()
	w.present(root)
	w.loop.Run()
	w.closing.Store(true)
	return nil
}

// present binds root, shows the window and starts the frame clock that draws
// it. Everything of Run but the loop itself, so a secondary window (OpenChild)
// can share the main window's loop.
func (w *Window) present(root toolkit.Widget) {
	w.root = root
	w.fr.bind(root)
	w.win.OnCloseRequest(w.onCloseRequest)
	w.installInput() // drawn regions become clickable/scrollable/typeable
	w.win.Present()
	w.im.FocusIn()      // the window has the keyboard; a no-op without an input method
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
		if w.fit() {
			w.dirty.Store(true)
		}
		if w.dirty.Swap(false) {
			w.frame()
		}
		return true
	})
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
	if w.child {
		return w.closeChild()
	}
	w.closing.Store(true) // an explicit Close ends the window even under HideOnClose
	w.closeChildren()
	if w.loop != 0 {
		w.loop.Quit()
	}
	return nil
}

// fit reallocates the framebuffer when the window was resized, and reports
// whether it did. The next present is then whole (the picture shows nothing of the
// new frame yet), after which damage resumes. Before the window has an allocation
// (not yet mapped) it keeps the size Open chose.
func (w *Window) fit() bool {
	aw, ah := w.overlay.AllocatedSize()
	nw, nh, changed := fitSize(aw, ah, w.scale, w.w, w.h)
	if !changed {
		return false
	}
	w.realloc(nw, nh)
	return true
}

// realloc replaces the framebuffer with an nw×nh one and arms a whole present.
func (w *Window) realloc(nw, nh int) {
	buf, sf := newFrameBuffer(nw, nh)
	w.sf.Release()
	w.buf, w.sf, w.w, w.h = buf, sf, nw, nh
	w.fr.resize()
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
	if w.fr.incremental() {
		w.fr.present(w.root, w.theme, w.buf, w.w, w.h, pictureTarget{w.pic, w.sf})
		w.syncNative(w.root)
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

// pictureTarget presents on the GtkPicture. The partial path needs GTK 4.16
// (GdkMemoryTextureBuilder update-texture/update-region), checked at run time by
// gtk4.TextureUpdateSupported; older libgtk-4 keeps the whole upload.
//
// With a shared frame (sf non-nil, buf its pixels) a present copies nothing: the
// texture is built over the frame's own memory. Without one every present copies
// the whole frame (w*h*4 bytes).
type pictureTarget struct {
	pic gtk4.Picture
	sf  *gtk4.SharedFrame
}

func (t pictureTarget) Full(buf []byte, w, h int) int {
	if t.sf != nil {
		t.pic.SetFrame(t.sf)
		return 0
	}
	t.pic.SetRGBA(buf, w, h)
	return w * h * 4
}

func (t pictureTarget) Update(buf []byte, w, h int, rects []toolkit.Rect) (int, bool) {
	rs := make([]gtk4.Rect, len(rects))
	for i, r := range rects {
		rs[i] = gtk4.Rect{X: r.X, Y: r.Y, W: r.W, H: r.H}
	}
	if t.sf != nil {
		return 0, t.pic.UpdateFrame(t.sf, rs)
	}
	return w * h * 4, t.pic.UpdateRGBA(buf, w, h, rs)
}
