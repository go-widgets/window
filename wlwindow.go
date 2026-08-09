// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"fmt"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/window/internal/wayland"
)

// wlWindow is the Wayland-backed window. It owns the RGBA framebuffer,
// presents it through a double-buffered wl_shm pool and drives the toolkit
// widget tree from wl_pointer / wl_keyboard events routed through the
// sovereign internal/wayland protocol machine. It satisfies Backend, so a
// go-widgets application runs on it exactly as on the X11 backend.
//
// The connection setup, buffer management and event translation are all
// transport-agnostic (the connection may be a real compositor socket or an
// in-process fake compositor), which is what makes the whole Wayland path
// testable without a display server.
type wlWindow struct {
	conn     *wayland.Conn
	surface  *wayland.Surface
	xdgSurf  *wayland.XdgSurface
	toplevel *wayland.XdgToplevel
	shm      *wayland.Shm
	seat     *wayland.Seat
	pointer  *wayland.Pointer
	keyboard *wayland.Keyboard
	inputErr error // latched error from dynamic input hot-plug

	pool     *wayland.ShmPool
	buffers  [2]*wayland.Buffer
	poolData []byte
	poolCap  int
	stride   int
	bufW     int
	bufH     int
	cur      int

	w, h  int
	buf   []byte // RGBA framebuffer, 4*w*h bytes
	theme *toolkit.Theme
	root  toolkit.Widget

	ptrX, ptrY int
	buttons    int // bitmask of pressed pointer buttons (for drag detection)

	pending []toolkit.Event
	repaint bool
	quit    bool
	closed  bool

	needResize         bool
	pendingW, pendingH int
	configured         bool
	needAck            bool
	ackSerial          uint32
}

// Required Wayland globals for a shell window.
const (
	ifaceCompositor = "wl_compositor"
	ifaceShm        = "wl_shm"
	ifaceXdgWmBase  = "xdg_wm_base"
)

// newWaylandWindow performs the full xdg-shell bring-up over conn: enumerate
// globals, bind the compositor/shm/xdg_wm_base (and the seat, if any),
// create the surface + xdg toplevel, set its identity and wait for the first
// configure. It is transport-agnostic, so a fake compositor drives it in
// tests.
func newWaylandWindow(conn *wayland.Conn, cfg Config) (*wlWindow, error) {
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

	w := &wlWindow{
		conn:  conn,
		w:     cfg.Width,
		h:     cfg.Height,
		theme: theme,
		buf:   make([]byte, 4*cfg.Width*cfg.Height),
	}

	reg, err := conn.Display().GetRegistry()
	if err != nil {
		return nil, err
	}
	if err := conn.Roundtrip(); err != nil {
		return nil, err
	}

	comp, err := reg.Compositor()
	if err != nil {
		return nil, err
	}
	if w.shm, err = reg.Shm(); err != nil {
		return nil, err
	}
	wm, err := reg.XdgWmBase()
	if err != nil {
		return nil, err
	}

	var seat *wayland.Seat
	if _, ok := reg.Find("wl_seat"); ok {
		if seat, err = reg.Seat(); err != nil {
			return nil, err
		}
	}

	// A second round-trip delivers the shm formats and seat capabilities.
	if err := conn.Roundtrip(); err != nil {
		return nil, err
	}
	w.seat = seat
	if err := w.bindInput(seat); err != nil {
		return nil, err
	}
	// Track later capability changes: a device (e.g. a keyboard or pointer)
	// that appears after bring-up — as happens when a virtual input device is
	// attached to an otherwise device-less headless seat — is obtained then.
	if seat != nil {
		seat.OnCapabilities = func(caps uint32) { w.onSeatCaps(caps) }
	}

	if w.surface, err = comp.CreateSurface(); err != nil {
		return nil, err
	}
	if w.xdgSurf, err = wm.GetXdgSurface(w.surface); err != nil {
		return nil, err
	}
	if w.toplevel, err = w.xdgSurf.GetToplevel(); err != nil {
		return nil, err
	}
	w.wireShell(cfg)

	if err := w.toplevel.SetTitle(cfg.Title); err != nil {
		return nil, err
	}
	appID := cfg.Instance
	if appID == "" {
		appID = cfg.Class
	}
	if appID == "" {
		appID = cfg.Title
	}
	if err := w.toplevel.SetAppID(appID); err != nil {
		return nil, err
	}
	// Commit the role with no buffer to elicit the initial configure, then
	// wait for it (the ack happens on the next dispatch in the run loop, but
	// we perform the pending ack here too so the first present is valid).
	if err := w.surface.Commit(); err != nil {
		return nil, err
	}
	if err := conn.Roundtrip(); err != nil {
		return nil, err
	}
	if err := w.flushAck(); err != nil {
		return nil, err
	}
	return w, nil
}

// bindInput obtains the pointer and keyboard advertised by the seat at
// bring-up (when any). Devices that appear later are picked up by
// onSeatCaps via the seat's capability callback.
func (w *wlWindow) bindInput(seat *wayland.Seat) error {
	if seat == nil {
		return nil
	}
	w.onSeatCaps(seat.Capabilities())
	return w.inputErr
}

// onSeatCaps reacts to a capability update by obtaining any newly present
// device it has not already bound. It is idempotent (a device is bound at
// most once) so it is safe to call for both the initial and later
// capability events. A bind failure is latched and surfaced by the run loop.
func (w *wlWindow) onSeatCaps(caps uint32) {
	if w.seat == nil {
		return
	}
	if caps&wayland.SeatCapabilityPointer != 0 && w.pointer == nil {
		p, err := w.seat.GetPointer()
		if err != nil {
			w.inputErr = err
			return
		}
		w.pointer = p
		w.wirePointer()
	}
	if caps&wayland.SeatCapabilityKeyboard != 0 && w.keyboard == nil {
		k, err := w.seat.GetKeyboard()
		if err != nil {
			w.inputErr = err
			return
		}
		w.keyboard = k
		w.wireKeyboard()
	}
}

// wireShell installs the xdg_surface / xdg_toplevel configure + close
// callbacks. Configure resizes on a nonzero suggested size; the ack is
// deferred to the run loop via flushAck.
func (w *wlWindow) wireShell(_ Config) {
	w.xdgSurf.OnConfigure = func(serial uint32) {
		w.ackSerial = serial
		w.needAck = true
		w.configured = true
		w.repaint = true
	}
	w.toplevel.OnConfigure = func(cw, ch int, _ []byte) {
		if cw > 0 && ch > 0 && (cw != w.w || ch != w.h) {
			w.pendingW, w.pendingH = cw, ch
			w.needResize = true
		}
	}
	w.toplevel.OnClose = func() { w.quit = true }
}

// wirePointer installs the pointer event callbacks that translate motion,
// buttons and axis into queued toolkit events.
func (w *wlWindow) wirePointer() {
	w.pointer.OnEnter = func(x, y wayland.Fixed) { w.ptrX, w.ptrY = x.Int(), y.Int() }
	w.pointer.OnLeave = func() {}
	w.pointer.OnMotion = func(x, y wayland.Fixed) {
		w.ptrX, w.ptrY = x.Int(), y.Int()
		w.queue(w.translateMotion(w.mods()))
	}
	w.pointer.OnButton = func(button uint32, pressed bool) {
		s, c := w.mods()
		w.queue(w.translateButton(button, pressed, s, c))
	}
	w.pointer.OnAxis = func(axis uint32, value wayland.Fixed) {
		s, c := w.mods()
		w.queue(w.translateAxis(axis, value, s, c))
	}
}

// wireKeyboard installs the key callback that translates a keycode into
// key/char toolkit events via the parsed xkb keymap and modifier state.
func (w *wlWindow) wireKeyboard() {
	w.keyboard.OnKey = func(evdev uint32, pressed bool) {
		w.queue(translateKey(w.keyboard.Keymap(), evdev, pressed, w.keyboard.Shift(), w.keyboard.Ctrl()))
	}
	w.keyboard.OnModifiers = func() {}
}

// mods returns the current Shift/Ctrl modifier state (false if no keyboard).
func (w *wlWindow) mods() (shift, ctrl bool) {
	if w.keyboard == nil {
		return false, false
	}
	return w.keyboard.Shift(), w.keyboard.Ctrl()
}

// queue appends translated events to the pending batch and marks a repaint.
func (w *wlWindow) queue(evs []toolkit.Event) {
	if len(evs) == 0 {
		return
	}
	w.pending = append(w.pending, evs...)
	w.repaint = true
}

// --- event → toolkit translation (pure) -----------------------------------

// translateKey maps an evdev keycode at the current shift level to toolkit
// key/char events. A modifier key yields nothing; a named key yields a
// single KeyDown/KeyUp carrying the name; a printable key yields KeyDown+Char
// on press and KeyUp on release. An unmapped key yields nothing.
func translateKey(km *wayland.Keymap, evdev uint32, pressed, shift, ctrl bool) []toolkit.Event {
	key := km.Lookup(evdev, shift)
	if key.IsModifier {
		return nil
	}
	if key.Name != "" {
		kind := toolkit.EventKeyDown
		if !pressed {
			kind = toolkit.EventKeyUp
		}
		return []toolkit.Event{{Kind: kind, Code: key.Name, Ctrl: ctrl, Shift: shift}}
	}
	if key.HasRune {
		s := string(key.Rune)
		if pressed {
			return []toolkit.Event{
				{Kind: toolkit.EventKeyDown, Code: s, Ctrl: ctrl, Shift: shift},
				{Kind: toolkit.EventChar, Code: s, Ctrl: ctrl, Shift: shift},
			}
		}
		return []toolkit.Event{{Kind: toolkit.EventKeyUp, Code: s, Ctrl: ctrl, Shift: shift}}
	}
	return nil
}

// buttonBit maps a Linux button code to a drag-tracking bit; 0 for buttons
// that carry no toolkit meaning.
func buttonBit(button uint32) int {
	switch button {
	case wayland.BtnLeft:
		return 1
	case wayland.BtnMiddle:
		return 2
	case wayland.BtnRight:
		return 4
	default:
		return 0
	}
}

// translateButton maps a pointer button press/release to a click (press) or
// mouse-up (release) at the last-known pointer position, updating the held
// -button mask used for drag detection.
func (w *wlWindow) translateButton(button uint32, pressed, shift, ctrl bool) []toolkit.Event {
	bit := buttonBit(button)
	if bit == 0 {
		return nil
	}
	if pressed {
		w.buttons |= bit
		return []toolkit.Event{{Kind: toolkit.EventClick, X: w.ptrX, Y: w.ptrY, Ctrl: ctrl, Shift: shift}}
	}
	w.buttons &^= bit
	return []toolkit.Event{{Kind: toolkit.EventMouseUp, X: w.ptrX, Y: w.ptrY, Ctrl: ctrl, Shift: shift}}
}

// translateMotion maps a pointer motion to a drag (a button held) or a plain
// hover move (no button) at the current pointer position.
func (w *wlWindow) translateMotion(shift, ctrl bool) []toolkit.Event {
	kind := toolkit.EventMouseMove
	if w.buttons != 0 {
		kind = toolkit.EventMouseDrag
	}
	return []toolkit.Event{{Kind: kind, X: w.ptrX, Y: w.ptrY, Ctrl: ctrl, Shift: shift}}
}

// translateAxis maps a vertical scroll axis tick to an EventScroll (one row
// per tick, sign following the scroll direction). Horizontal scroll carries
// no toolkit meaning and is dropped.
func (w *wlWindow) translateAxis(axis uint32, value wayland.Fixed, shift, ctrl bool) []toolkit.Event {
	if axis != wayland.AxisVerticalScroll || value == 0 {
		return nil
	}
	delta := 1
	if value.Float() < 0 {
		delta = -1
	}
	return []toolkit.Event{{Kind: toolkit.EventScroll, X: w.ptrX, Y: w.ptrY, Delta: delta, Ctrl: ctrl, Shift: shift}}
}

// --- present --------------------------------------------------------------

// draw repaints the whole framebuffer: background fill then the root widget
// laid out to fill the client area.
func (w *wlWindow) draw() {
	p := painter.NewPixelPainter(w.buf, w.w, w.h)
	full := toolkit.Rect{X: 0, Y: 0, W: w.w, H: w.h}
	p.FillRect(full, w.theme.Background)
	if w.root != nil {
		w.root.SetBounds(full)
		w.root.Draw(p, w.theme)
	}
}

// ensureBuffers (re)creates the double-buffered wl_shm pool whenever the
// surface size changes, carving two ARGB8888 buffers from it.
func (w *wlWindow) ensureBuffers() error {
	if w.pool != nil && w.bufW == w.w && w.bufH == w.h {
		return nil
	}
	if w.pool != nil {
		for i := range w.buffers {
			if w.buffers[i] != nil {
				_ = w.buffers[i].Destroy()
				w.buffers[i] = nil
			}
		}
		_ = w.pool.Destroy()
		w.pool = nil
	}
	stride := w.w * 4
	size := stride * w.h * 2
	pool, err := w.shm.CreatePool(size)
	if err != nil {
		return err
	}
	for i := 0; i < 2; i++ {
		buf, err := pool.CreateBuffer(i*stride*w.h, w.w, w.h, stride, wayland.ShmFormatARGB8888)
		if err != nil {
			return err
		}
		w.buffers[i] = buf
	}
	w.pool = pool
	w.poolData = pool.Data()
	w.poolCap = size
	w.stride = stride
	w.bufW, w.bufH = w.w, w.h
	w.cur = 0
	return nil
}

// present packs the RGBA framebuffer into a free pool buffer as ARGB8888,
// attaches it, marks whole-surface buffer damage, requests a frame-throttle
// callback and commits.
func (w *wlWindow) present() error {
	if !w.configured {
		return nil
	}
	if err := w.ensureBuffers(); err != nil {
		return err
	}
	idx := w.cur
	if !w.buffers[idx].Released() && w.buffers[1-idx].Released() {
		idx = 1 - idx
	}
	off := idx * w.stride * w.h
	wayland.PackARGB8888(w.poolData[off:], w.stride, w.buf, w.w*4, w.w, w.h)
	if err := w.surface.Attach(w.buffers[idx], 0, 0); err != nil {
		return err
	}
	if err := w.surface.DamageBuffer(0, 0, w.w, w.h); err != nil {
		return err
	}
	if _, err := w.surface.Frame(); err != nil {
		return err
	}
	if err := w.surface.Commit(); err != nil {
		return err
	}
	w.cur = 1 - idx
	return nil
}

// flushAck acknowledges a pending configure serial, if any.
func (w *wlWindow) flushAck() error {
	if !w.needAck {
		return nil
	}
	w.needAck = false
	return w.xdgSurf.AckConfigure(w.ackSerial)
}

// applyResize grows/shrinks the framebuffer to the pending size.
func (w *wlWindow) applyResize() {
	if w.pendingW <= 0 || w.pendingH <= 0 {
		w.needResize = false
		return
	}
	w.w, w.h = w.pendingW, w.pendingH
	w.buf = make([]byte, 4*w.w*w.h)
	w.needResize = false
	w.repaint = true
}

// --- Backend --------------------------------------------------------------

// Run binds root, performs the initial layout+present, then dispatches
// compositor events into the toolkit until the window is closed
// (xdg_toplevel.close) or the connection ends. It is the Wayland analogue of
// the X11 host loop and the wasm compositor host loop.
func (w *wlWindow) Run(root toolkit.Widget) error {
	w.root = root
	w.draw()
	if err := w.present(); err != nil {
		return err
	}
	for !w.quit {
		if err := w.conn.Dispatch(); err != nil {
			return err
		}
		if w.inputErr != nil {
			return w.inputErr
		}
		if err := w.flushAck(); err != nil {
			return err
		}
		if w.needResize {
			w.applyResize()
		}
		if w.root != nil {
			for _, ev := range w.pending {
				w.root.OnEvent(ev)
			}
		}
		if w.repaint || len(w.pending) > 0 {
			w.draw()
			if err := w.present(); err != nil {
				return err
			}
		}
		w.pending = w.pending[:0]
		w.repaint = false
	}
	return nil
}

// Size returns the current client size in pixels.
func (w *wlWindow) Size() (int, int) { return w.w, w.h }

// Close destroys the pool and closes the connection (idempotent).
func (w *wlWindow) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	if w.pool != nil {
		_ = w.pool.Destroy()
		w.pool = nil
	}
	return w.conn.Close()
}

// String identifies the window for debugging.
func (w *wlWindow) String() string {
	var sid uint32
	if w.surface != nil {
		sid = w.surface.ID()
	}
	return fmt.Sprintf("wayland-window(%dx%d surface=%d)", w.w, w.h, sid)
}
