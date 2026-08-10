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
	"github.com/go-widgets/window/internal/atspi"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/window/internal/x11"
)

// ErrUnsupported is returned by Open on platforms with no windowing backend —
// everything that is neither Linux (X11/Wayland) nor macOS (Cocoa/AppKit) nor
// Windows (Win32/GDI) nor the js/wasm wasmbox environment.
var ErrUnsupported = errors.New("window: no native windowing backend for this platform")

// Backend is an open, backend-specific window bound to a go-widgets scene. The
// X11 (*Window), Wayland, macOS Cocoa, Windows Win32 and wasmbox backends all
// satisfy it, so Open can return whichever the environment selects and a
// go-widgets application is backend-agnostic: it just calls Run, Size, String
// and Close.
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
	// Width and Height are the initial client size in LOGICAL points (the unit
	// the toolkit lays out and the user reads in — not device pixels). A value ≤ 0
	// asks the backend for a readable default: the macOS (Cocoa) backend derives
	// it from the main screen's visible frame; the X11/Wayland backends use their
	// standard 640×480. A desktop shell should pass its own point size here.
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

	// MIT-SHM present path (nil when the server lacks the extension or
	// fd-passing): shm is the queried extension, seg the shared segment that
	// mirrors the framebuffer as a ZPixmap image. present falls back to plain
	// PutImage whenever seg is nil.
	shm *x11.Shm
	seg *x11.Segment

	w, h  int
	buf   []byte // RGBA framebuffer, 4*w*h bytes
	theme *toolkit.Theme

	keymap *x11.Keymap

	wmProtocols    uint32
	wmDeleteWindow uint32

	root   toolkit.Widget
	dmg    DamageRenderer // non-nil when root opts into incremental present
	closed bool

	// originX/originY are the window's position, kept for the accessibility
	// bridge (see the ConfigureNotify branch in events.go).
	originX, originY int
	title            string
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
		// The accessibility bus names the application object; the window title
		// is what the user already knows it by.
		title: cfg.Title,
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
	// Best-effort MIT-SHM: a failure to set it up is never fatal — the window
	// simply keeps the plain PutImage path.
	w.setupSHM()
	return w, nil
}

// setupSHM queries MIT-SHM and, when the server supports the 1.2 fd-passing
// path, allocates and attaches a shared segment sized to the framebuffer. Any
// failure leaves seg nil so present() falls back to PutImage.
func (w *Window) setupSHM() {
	shm, err := w.conn.QueryShm()
	if err != nil || shm == nil || !shm.FDCapable {
		return
	}
	w.shm = shm
	w.ensureSegment()
}

// ensureSegment (re)allocates the shared segment to match the current
// framebuffer size, detaching any previous one. On any failure it disables the
// SHM path (seg stays/reverts to nil) rather than erroring.
func (w *Window) ensureSegment() {
	if w.shm == nil {
		return
	}
	if w.seg != nil {
		_ = w.shm.Detach(w.seg.Seg)
		_ = w.seg.Close()
		w.seg = nil
	}
	size := w.pres.SegmentSize(w.w, w.h)
	seg, err := x11.NewSegment(w.conn.NewID(), size)
	if err != nil {
		w.shm = nil // give up on SHM for this window
		return
	}
	if err := w.shm.AttachFd(seg.Seg, seg.FD, false); err != nil {
		_ = seg.Close()
		w.shm = nil
		return
	}
	w.seg = seg
}

// Size returns the current client size in pixels.
func (w *Window) Size() (int, int) { return w.w, w.h }

// Close closes the window's connection to the server.
func (w *Window) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	if w.seg != nil {
		_ = w.shm.Detach(w.seg.Seg)
		_ = w.seg.Close()
		w.seg = nil
	}
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

// drawIncremental lays the root out to the full client area and repaints ONLY
// the damage the root reports, returning the repainted rectangles for the
// caller to present. It is used exclusively when the root opts into incremental
// present (w.dmg != nil). The framebuffer persists across frames, so the pixels
// outside the damage are already correct from the previous frame.
func (w *Window) drawIncremental() []toolkit.Rect {
	p := painter.NewPixelPainter(w.buf, w.w, w.h)
	w.root.SetBounds(toolkit.Rect{X: 0, Y: 0, W: w.w, H: w.h})
	return w.dmg.RenderDamaged(p, w.theme)
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
	if w.seg != nil {
		// SHM path: mirror the damaged rect into the shared segment, then blit
		// it from shared memory with one tiny request (no pixels on the wire).
		if err := w.pres.EncodeRectInto(w.seg.Data, w.w, w.buf, w.w*4, x, y, rw, rh); err != nil {
			return err
		}
		return w.shm.PutImage(w.pres, w.win, w.gc, w.seg.Seg, 0, w.w, w.h, x, y, rw, rh, x, y)
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
	// Grow the shared segment to match, if the SHM path is active.
	w.ensureSegment()
}

// Run binds root to the window, performs the initial layout+draw+present,
// then dispatches server events into the toolkit until the window is
// closed (WM_DELETE_WINDOW) or the connection ends. It is the real-window
// analogue of the wasm compositor host loop.
func (w *Window) Run(root toolkit.Widget) error {
	w.root = root
	w.dmg, _ = root.(DamageRenderer)
	// Initial frame: paint the whole framebuffer, then present the full surface
	// for the window's first map. When the root is incremental, draw through it
	// so its full-surface seed damage is consumed here — the first interaction
	// is then already a purely incremental frame.
	if w.dmg != nil {
		w.drawIncremental()
	} else {
		w.draw()
	}
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
			if err := w.paintFrame(out.resize, out.expose); err != nil {
				return err
			}
		}
	}
}

// paintFrame renders and presents one frame. With a plain root it repaints and
// blits the whole surface. With an incremental root it presents only the damaged
// rectangles — except after a resize (the framebuffer was reallocated, so the
// root reports whole-surface damage, which drawIncremental repaints, then the
// full surface is blitted) or an Expose (the framebuffer is intact, so the whole
// surface is re-blitted with no redraw).
func (w *Window) paintFrame(resize, expose bool) error {
	// The frame about to be shown and the tree a screen reader reads are
	// published from the same place, so the description never lags the pixels.
	// Activation replays an ordinary click, so every click behaviour comes free.
	// Apply anything a screen reader asked for since the last frame, HERE, on
	// the thread that owns the widget tree — the bridge only records the
	// request. Replaying it as an ordinary click means every behaviour a click
	// has is had by an accessibility action, with no second path to drift.
	for _, p := range atspi.TakePending() {
		if w.root != nil {
			w.root.OnEvent(toolkit.Event{Kind: toolkit.EventClick, X: p.X, Y: p.Y})
		}
	}
	atspi.Publish(w.root, w.title, w.originX, w.originY)
	if w.dmg == nil {
		w.draw()
		return w.present()
	}
	if expose {
		return w.present() // contents intact; re-blit whole surface
	}
	rects := w.drawIncremental()
	if resize {
		return w.present() // reallocated framebuffer: blit the whole surface
	}
	for _, r := range rects {
		if err := w.presentRect(r.X, r.Y, r.W, r.H); err != nil {
			return err
		}
	}
	return nil
}

// String identifies the window for debugging.
func (w *Window) String() string {
	return fmt.Sprintf("window(%dx%d id=%#x)", w.w, w.h, w.win)
}
