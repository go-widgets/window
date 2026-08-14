// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"errors"
	"fmt"
	"sync"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/window/internal/dnd"
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
	registry *wayland.Registry
	surface  *wayland.Surface
	xdgSurf  *wayland.XdgSurface
	toplevel *wayland.XdgToplevel
	shm      *wayland.Shm
	seat     *wayland.Seat
	pointer  *wayland.Pointer
	keyboard *wayland.Keyboard
	inputErr error // latched error from dynamic input hot-plug

	// Clipboard state, bound on first use — see clipboard_wayland.go. clipText
	// is what we last offered and clipOwned whether the compositor still counts
	// us as the owner, because the only way to read our own selection would be
	// to ask ourselves through the dispatch we are standing in.
	ddm             *wayland.DataDeviceManager
	dataDev         *wayland.DataDevice
	clipSource      *wayland.DataSource
	clipText        string
	clipOwned       bool
	clipUnavailable bool // this session has no wl_data_device_manager

	portal portalConn // desktop appearance, shared with the X11 back-end

	// fbmu guards the shared-memory pool and everything that writes into it.
	//
	// Close may be called from any goroutine -- an application closes its window
	// from a menu item, a signal handler, a shutdown path -- and it UNMAPS the
	// pool. A run loop painting into that mapping at the same time takes a
	// segmentation fault, not an error: it happened, in CI, in PackARGB8888. The
	// mutex is uncontended in the ordinary case, since only the run loop paints.
	fbmu     sync.Mutex
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
	dmg   DamageRenderer // non-nil when root opts into incremental present
	dnd   *dnd.Controller

	// bufDamage[i] is the region pool buffer i still owes relative to the live
	// framebuffer — the pixels changed while it was NOT the attached buffer,
	// plus this frame's damage. Because the two pool buffers alternate, a buffer
	// is up to a frame stale when re-chosen; packing its owed region (not just
	// this frame's) keeps every attached buffer fully correct, so a wl_shm
	// buffer the compositor may sample outside the surface-damage rect never
	// shows a stale pixel. Seeded to the whole surface on (re)creation.
	bufDamage [2][]toolkit.Rect

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
	// Kept, not just used: the clipboard binds its global on first use rather
	// than at bring-up, so it needs the registry to still be reachable then.
	w.registry = reg
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
		w.queue(w.translateButton(button, pressed, w.mods()))
	}
	w.pointer.OnAxis = func(axis uint32, value wayland.Fixed) {
		w.queue(w.translateAxis(axis, value, w.mods()))
	}
}

// wireKeyboard installs the key callback that translates a keycode into
// key/char toolkit events via the parsed xkb keymap and modifier state.
func (w *wlWindow) wireKeyboard() {
	w.keyboard.OnKey = func(evdev uint32, pressed bool) {
		w.queue(translateKey(w.keyboard.Keymap(), evdev, pressed, w.mods()))
	}
	w.keyboard.OnModifiers = func() {}
}

// wlmods is the decoded modifier state: Shift, Control, Alt and Meta (the
// Super/logo key). Deriving all four lets a widget tell a plain Ctrl chord from
// an Alt/Meta one (paste vs paste-as-move in the file manager).
type wlmods struct{ shift, ctrl, alt, meta bool }

// with stamps the four modifier flags onto ev.
func (m wlmods) with(ev toolkit.Event) toolkit.Event {
	ev.Shift, ev.Ctrl, ev.Alt, ev.Meta = m.shift, m.ctrl, m.alt, m.meta
	return ev
}

// mods returns the current modifier state (all false if no keyboard).
func (w *wlWindow) mods() wlmods {
	if w.keyboard == nil {
		return wlmods{}
	}
	return wlmods{
		shift: w.keyboard.Shift(),
		ctrl:  w.keyboard.Ctrl(),
		alt:   w.keyboard.Alt(),
		meta:  w.keyboard.Logo(),
	}
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
func translateKey(km *wayland.Keymap, evdev uint32, pressed bool, m wlmods) []toolkit.Event {
	key := km.Lookup(evdev, m.shift)
	if key.IsModifier {
		return nil
	}
	if key.Name != "" {
		kind := toolkit.EventKeyDown
		if !pressed {
			kind = toolkit.EventKeyUp
		}
		return []toolkit.Event{m.with(toolkit.Event{Kind: kind, Code: key.Name})}
	}
	if key.HasRune {
		s := string(key.Rune)
		if pressed {
			return []toolkit.Event{
				m.with(toolkit.Event{Kind: toolkit.EventKeyDown, Code: s}),
				m.with(toolkit.Event{Kind: toolkit.EventChar, Code: s}),
			}
		}
		return []toolkit.Event{m.with(toolkit.Event{Kind: toolkit.EventKeyUp, Code: s})}
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
func (w *wlWindow) translateButton(button uint32, pressed bool, m wlmods) []toolkit.Event {
	bit := buttonBit(button)
	if bit == 0 {
		return nil
	}
	if pressed {
		w.buttons |= bit
		return []toolkit.Event{m.with(toolkit.Event{Kind: toolkit.EventClick, X: w.ptrX, Y: w.ptrY})}
	}
	w.buttons &^= bit
	return []toolkit.Event{m.with(toolkit.Event{Kind: toolkit.EventMouseUp, X: w.ptrX, Y: w.ptrY})}
}

// translateMotion maps a pointer motion to a drag (a button held) or a plain
// hover move (no button) at the current pointer position.
func (w *wlWindow) translateMotion(m wlmods) []toolkit.Event {
	kind := toolkit.EventMouseMove
	if w.buttons != 0 {
		kind = toolkit.EventMouseDrag
	}
	return []toolkit.Event{m.with(toolkit.Event{Kind: kind, X: w.ptrX, Y: w.ptrY})}
}

// translateAxis maps a vertical scroll axis tick to an EventScroll (one row
// per tick, sign following the scroll direction). Horizontal scroll carries
// no toolkit meaning and is dropped.
func (w *wlWindow) translateAxis(axis uint32, value wayland.Fixed, m wlmods) []toolkit.Event {
	if axis != wayland.AxisVerticalScroll || value == 0 {
		return nil
	}
	delta := 1
	if value.Float() < 0 {
		delta = -1
	}
	return []toolkit.Event{m.with(toolkit.Event{Kind: toolkit.EventScroll, X: w.ptrX, Y: w.ptrY, Delta: delta})}
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
	// Both buffers are freshly allocated (uninitialised): each owes the whole
	// surface, so the first pack into either fills it completely.
	full := toolkit.Rect{X: 0, Y: 0, W: w.w, H: w.h}
	w.bufDamage[0] = append(w.bufDamage[0][:0], full)
	w.bufDamage[1] = append(w.bufDamage[1][:0], full)
	return nil
}

// present packs the RGBA framebuffer into a free pool buffer as ARGB8888,
// attaches it, marks whole-surface buffer damage, requests a frame-throttle
// callback and commits.
func (w *wlWindow) present() error {
	w.fbmu.Lock()
	defer w.fbmu.Unlock()
	if !w.configured || w.closed {
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

// drawIncremental lays the root out to the full surface and repaints ONLY the
// damage the root reports, returning the repainted rectangles. Used only when
// the root opts into incremental present (w.dmg != nil). The framebuffer
// persists across frames, so pixels outside the damage stay correct.
func (w *wlWindow) drawIncremental() []toolkit.Rect {
	p := painter.NewPixelPainter(w.buf, w.w, w.h)
	w.root.SetBounds(toolkit.Rect{X: 0, Y: 0, W: w.w, H: w.h})
	return w.dmg.RenderDamaged(p, w.theme)
}

// presentDamaged packs and commits only the damaged region. It packs into the
// chosen buffer every rectangle that buffer owes (this frame's damage plus any
// it missed while unattached — see bufDamage), so the buffer is fully correct,
// but marks as surface damage only this frame's rectangles (the pixels that
// differ from what is currently on screen). frame must be non-empty.
func (w *wlWindow) presentDamaged(frame []toolkit.Rect) error {
	w.fbmu.Lock()
	defer w.fbmu.Unlock()
	if !w.configured || w.closed {
		return nil
	}
	if err := w.ensureBuffers(); err != nil {
		return err
	}
	// Record this frame's damage against both buffers.
	for _, r := range frame {
		w.bufDamage[0] = addDamage(w.bufDamage[0], r)
		w.bufDamage[1] = addDamage(w.bufDamage[1], r)
	}
	idx := w.cur
	if !w.buffers[idx].Released() && w.buffers[1-idx].Released() {
		idx = 1 - idx
	}
	off := idx * w.stride * w.h
	dst := w.poolData[off:]
	// Pack every rectangle this buffer owes so it holds the full correct image.
	for _, r := range w.bufDamage[idx] {
		c := clampRect(r, w.w, w.h)
		if c.W <= 0 || c.H <= 0 {
			continue
		}
		dOff := c.Y*w.stride + c.X*4
		sOff := c.Y*w.w*4 + c.X*4
		wayland.PackARGB8888(dst[dOff:], w.stride, w.buf[sOff:], w.w*4, c.W, c.H)
	}
	w.bufDamage[idx] = w.bufDamage[idx][:0] // now fully current
	if err := w.surface.Attach(w.buffers[idx], 0, 0); err != nil {
		return err
	}
	// Surface damage: only this frame's rectangles differ from the previously
	// committed (on-screen) buffer.
	for _, r := range frame {
		c := clampRect(r, w.w, w.h)
		if c.W <= 0 || c.H <= 0 {
			continue
		}
		if err := w.surface.DamageBuffer(c.X, c.Y, c.W, c.H); err != nil {
			return err
		}
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
	w.dmg, _ = root.(DamageRenderer)
	if w.dnd == nil {
		w.dnd = dnd.New()
	}
	w.dnd.Bind(root)
	if err := w.paintInitial(); err != nil {
		return err
	}
	for !w.quit {
		if err := w.conn.Dispatch(); err != nil {
			// Not an event but an interruption: somebody outside this loop asked
			// for a frame. It carries nothing else, so the only thing to do with
			// it is to paint.
			if errors.Is(err, wayland.ErrWoken) {
				w.repaint = true
			} else {
				return err
			}
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
				for _, dev := range w.dnd.Process(ev) {
					w.root.OnEvent(dev)
				}
			}
		}
		if w.repaint || len(w.pending) > 0 {
			if err := w.paintFrame(); err != nil {
				return err
			}
		}
		w.pending = w.pending[:0]
		w.repaint = false
	}
	return nil
}

// paintInitial paints and presents the window's first frame. It just defers to
// paintFrame: the surface is typically not configured yet (the first xdg
// configure arrives during the run loop, not at bring-up), so the incremental
// path must NOT render-and-consume its damage before it can present — see
// paintFrame.
func (w *wlWindow) paintInitial() error { return w.paintFrame() }

// paintFrame renders and presents one frame. A plain root repaints+commits the
// whole surface every call (present() itself no-ops until the surface is
// configured; the immediate-mode redraw is idempotent, so nothing is lost by
// re-running it once configured). An incremental root instead repaints+commits
// only its damage — but ONLY once configured: RenderDamaged consumes the scene's
// accumulated damage, so rendering before we can present would silently drop the
// first (full-surface seed) frame and leave later frames with nothing to show.
// Gating on w.configured keeps the pending damage intact until the first
// configure, after which the full seed is drawn and presented. A resize routes
// through the same path: the root reports whole-surface damage and the recreated
// pool buffers each owe the whole surface, so the frame is packed in full.
func (w *wlWindow) paintFrame() error {
	if w.dmg == nil {
		w.draw()
		return w.present()
	}
	if !w.configured {
		return nil // cannot present yet; keep the pending damage for later
	}
	rects := w.drawIncremental()
	if len(rects) == 0 {
		return nil // nothing changed this frame
	}
	return w.presentDamaged(rects)
}

// Size returns the current client size in pixels.
func (w *wlWindow) Size() (int, int) { return w.w, w.h }

// Close destroys the pool and closes the connection (idempotent).
// Close destroys the pool and closes the connection (idempotent).
//
// Safe from any goroutine: the pool is unmapped under fbmu, so a run loop
// painting into it finishes its frame first and finds the window closed on the
// next one.
func (w *wlWindow) Close() error {
	w.fbmu.Lock()
	if w.closed {
		w.fbmu.Unlock()
		return nil
	}
	w.closed = true
	if w.pool != nil {
		_ = w.pool.Destroy()
		w.pool = nil
	}
	w.fbmu.Unlock()
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
