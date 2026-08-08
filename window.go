// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// Package window is a pure-Go (CGO-free, no Xlib/XCB) X11 windowing
// backend for the go-widgets toolkit. It opens a real window on an X11
// server, blits the toolkit's RGBA framebuffer into it via the core
// protocol's PutImage, and routes X input events into toolkit.Event, so a
// go-widgets widget tree runs on a Linux desktop exactly as it does in the
// browser/wasm host.
//
// The X11 protocol itself is implemented from scratch in the internal/x11
// package over a raw byte stream, mirroring the sovereign transport+codec
// approach of github.com/go-freedesktop/dbus.
//
// Open dials the server named by $DISPLAY; it is implemented on Linux and
// returns ErrUnsupported elsewhere, so cross-builds stay green. The
// windowing logic (framebuffer, present, event translation, run loop) is
// platform-independent and driven through the transport-agnostic
// internal/x11 connection.
package window

import (
	"errors"
	"fmt"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/window/internal/x11"
)

// ErrUnsupported is returned by Open on platforms with no windowing
// backend (non-Linux).
var ErrUnsupported = errors.New("window: a native windowing backend is only supported on Linux")

// Backend is an open, backend-specific window bound to a go-widgets scene.
// Both the X11 (*Window) and the Wayland backend satisfy it, so Open can
// return whichever the environment selects and a go-widgets application is
// backend-agnostic: it just calls Run, Size, String and Close.
type Backend interface {
	// Run binds root, performs the initial layout+present, then dispatches
	// server/compositor events into the widget tree until the window closes.
	Run(root toolkit.Widget) error
	// Close releases the window and its connection.
	Close() error
	// Size returns the current client size in pixels.
	Size() (int, int)
	// String identifies the window for debugging.
	String() string
}

// Compile-time assurance that the X11 window satisfies Backend.
var _ Backend = (*Window)(nil)

// Config parametrises a window.
type Config struct {
	// Title is the WM_NAME shown in the title bar.
	Title string
	// Instance and Class populate WM_CLASS (window-manager grouping). When
	// empty they default to Title (Instance) and Title (Class).
	Instance string
	Class    string
	// Width and Height are the initial client size in pixels.
	Width  int
	Height int
	// Display overrides $DISPLAY (e.g. ":0"). Empty uses the environment.
	Display string
	// Theme overrides the toolkit theme used to paint the background and
	// widgets. Nil uses toolkit.DefaultDark.
	Theme *toolkit.Theme
}

// Window is an open X11 window bound to a go-widgets scene. It owns the
// backing RGBA framebuffer, presents it to the server and drives the
// toolkit widget tree from X input events.
type Window struct {
	conn *x11.Conn
	pres *x11.Presenter

	win   uint32
	gc    uint32
	depth uint8

	w, h  int
	buf   []byte // RGBA framebuffer, 4*w*h bytes
	theme *toolkit.Theme

	keymap *x11.Keymap

	wmProtocols    uint32
	wmDeleteWindow uint32

	root   toolkit.Widget
	closed bool
}

// newWindow builds a Window over an already-handshaken connection: it
// selects the screen's TrueColor visual, creates and maps the window,
// installs the WM properties and delete-protocol, creates a GC and fetches
// the keyboard mapping. It is transport-agnostic (the connection may be a
// real socket or an in-process pipe), which is what makes the whole
// windowing path testable without an X server.
func newWindow(conn *x11.Conn, cfg Config) (*Window, error) {
	if cfg.Width <= 0 {
		cfg.Width = 640
	}
	if cfg.Height <= 0 {
		cfg.Height = 480
	}
	theme := cfg.Theme
	if theme == nil {
		theme = toolkit.DefaultDark()
	}

	setup := conn.Setup()
	screen := &setup.Screens[0]
	vis := screen.RootVisualType()
	depth := screen.RootDepth
	pres, err := x11.NewPresenter(setup, vis, depth)
	if err != nil {
		return nil, err
	}

	w := &Window{
		conn:  conn,
		pres:  pres,
		depth: depth,
		w:     cfg.Width,
		h:     cfg.Height,
		theme: theme,
		buf:   make([]byte, 4*cfg.Width*cfg.Height),
	}
	w.win = conn.NewID()
	w.gc = conn.NewID()

	if err := conn.CreateWindow(w.win, screen.Root, 0, 0,
		uint16(cfg.Width), uint16(cfg.Height),
		screen.BlackPixel, screen.BlackPixel, x11.DefaultEventMask); err != nil {
		return nil, err
	}
	if err := conn.SetWMName(w.win, cfg.Title); err != nil {
		return nil, err
	}
	inst, class := cfg.Instance, cfg.Class
	if inst == "" {
		inst = cfg.Title
	}
	if class == "" {
		class = cfg.Title
	}
	if err := conn.SetWMClass(w.win, inst, class); err != nil {
		return nil, err
	}

	// Register for graceful close via WM_DELETE_WINDOW.
	if w.wmProtocols, err = conn.InternAtom("WM_PROTOCOLS", false); err != nil {
		return nil, err
	}
	if w.wmDeleteWindow, err = conn.InternAtom("WM_DELETE_WINDOW", false); err != nil {
		return nil, err
	}
	if err := conn.SetWMProtocols(w.win, w.wmProtocols, w.wmDeleteWindow); err != nil {
		return nil, err
	}

	if err := conn.CreateGC(w.gc, w.win); err != nil {
		return nil, err
	}
	if err := conn.MapWindow(w.win); err != nil {
		return nil, err
	}
	if w.keymap, err = conn.FetchKeymap(); err != nil {
		return nil, err
	}
	return w, nil
}

// Size returns the current client size in pixels.
func (w *Window) Size() (int, int) { return w.w, w.h }

// Close closes the window's connection to the server.
func (w *Window) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	return w.conn.Close()
}

// draw repaints the whole framebuffer: background fill then the root widget
// laid out to fill the client area.
func (w *Window) draw() {
	p := painter.NewPixelPainter(w.buf, w.w, w.h)
	full := toolkit.Rect{X: 0, Y: 0, W: w.w, H: w.h}
	p.FillRect(full, w.theme.Background)
	if w.root != nil {
		w.root.SetBounds(full)
		w.root.Draw(p, w.theme)
	}
}

// present blits the whole framebuffer to the server.
func (w *Window) present() error {
	return w.presentRect(0, 0, w.w, w.h)
}

// presentRect blits only the given rectangle of the framebuffer — the
// damage-region path used for Expose events. (The toolkit exposes no scene
// damage list today, so full-surface present is used after input; when a
// scene damage region becomes available it can feed straight into this
// method.)
func (w *Window) presentRect(x, y, rw, rh int) error {
	if x < 0 {
		rw += x
		x = 0
	}
	if y < 0 {
		rh += y
		y = 0
	}
	if x+rw > w.w {
		rw = w.w - x
	}
	if y+rh > w.h {
		rh = w.h - y
	}
	if rw <= 0 || rh <= 0 {
		return nil
	}
	return w.conn.PutImage(w.pres, w.win, w.gc, w.buf, w.w*4, x, y, rw, rh, x, y)
}

// resize grows/shrinks the framebuffer to the new client size.
func (w *Window) resize(nw, nh int) {
	if nw <= 0 || nh <= 0 || (nw == w.w && nh == w.h) {
		return
	}
	w.w, w.h = nw, nh
	w.buf = make([]byte, 4*nw*nh)
}

// Run binds root to the window, performs the initial layout+draw+present,
// then dispatches server events into the toolkit until the window is
// closed (WM_DELETE_WINDOW) or the connection ends. It is the real-window
// analogue of the wasm compositor host loop.
func (w *Window) Run(root toolkit.Widget) error {
	w.root = root
	w.draw()
	if err := w.present(); err != nil {
		return err
	}
	for {
		xe, err := w.conn.NextEvent()
		if err != nil {
			return err
		}
		out := w.mapEvent(xe)
		if out.quit {
			return nil
		}
		if out.resize {
			w.resize(out.rw, out.rh)
		}
		for _, ev := range out.events {
			if w.root != nil {
				w.root.OnEvent(ev)
			}
		}
		if out.repaint || out.resize || len(out.events) > 0 {
			w.draw()
			if err := w.present(); err != nil {
				return err
			}
		}
	}
}

// String identifies the window for debugging.
func (w *Window) String() string {
	return fmt.Sprintf("window(%dx%d id=%#x)", w.w, w.h, w.win)
}
