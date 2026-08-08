// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package wayland

import "fmt"

// XdgWmBase is the xdg_wm_base global (stable xdg-shell): the factory for
// window-manager surface roles. It answers the compositor's liveness pings
// automatically so the window is never declared unresponsive.
type XdgWmBase struct {
	conn *Conn
	id   uint32
}

const xdgWmBaseIfaceVersion = 4

// xdg_wm_base request opcodes.
const (
	xdgWmBaseReqDestroy       = 0
	xdgWmBaseReqGetXdgSurface = 2
	xdgWmBaseReqPong          = 3
)

// xdg_wm_base event opcode.
const xdgWmBaseEvtPing = 0

// bindXdgWmBase binds the xdg_wm_base global from the registry.
func bindXdgWmBase(reg *Registry, g Global) (*XdgWmBase, error) {
	ver := min32(g.Version, xdgWmBaseIfaceVersion)
	id, err := reg.bind(g.Name, "xdg_wm_base", ver)
	if err != nil {
		return nil, err
	}
	b := &XdgWmBase{conn: reg.conn, id: id}
	reg.conn.register(id, b.handle)
	return b, nil
}

// handle answers ping with pong; other events are ignored.
func (b *XdgWmBase) handle(opcode uint16, d *decoder) error {
	if opcode != xdgWmBaseEvtPing {
		return nil
	}
	serial := d.getU32()
	if !d.ok {
		return fmt.Errorf("wayland: truncated xdg_wm_base.ping")
	}
	return b.Pong(serial)
}

// Pong answers a liveness ping.
func (b *XdgWmBase) Pong(serial uint32) error {
	e := newEncoder(b.conn.order)
	e.putU32(serial)
	return b.conn.send(b.id, xdgWmBaseReqPong, e.buf, nil)
}

// GetXdgSurface gives a wl_surface the xdg_surface role.
func (b *XdgWmBase) GetXdgSurface(surf *Surface) (*XdgSurface, error) {
	id := b.conn.allocID()
	e := newEncoder(b.conn.order)
	e.putU32(id)
	e.putU32(surf.id)
	if err := b.conn.send(b.id, xdgWmBaseReqGetXdgSurface, e.buf, nil); err != nil {
		return nil, err
	}
	xs := &XdgSurface{conn: b.conn, id: id, surface: surf}
	b.conn.register(id, xs.handle)
	return xs, nil
}

// Destroy releases the xdg_wm_base object.
func (b *XdgWmBase) Destroy() error {
	err := b.conn.send(b.id, xdgWmBaseReqDestroy, nil, nil)
	b.conn.unregister(b.id)
	return err
}

// XdgSurface adds window-manager semantics (configure/ack) to a wl_surface.
type XdgSurface struct {
	conn    *Conn
	id      uint32
	surface *Surface
	// OnConfigure, if set, is called with each configure serial. The window
	// layer acks it (after applying any toplevel size) via AckConfigure.
	OnConfigure func(serial uint32)
	lastSerial  uint32
	configured  bool
}

// xdg_surface request opcodes.
const (
	xdgSurfaceReqDestroy      = 0
	xdgSurfaceReqGetToplevel  = 1
	xdgSurfaceReqAckConfigure = 4
)

// xdg_surface event opcode.
const xdgSurfaceEvtConfigure = 0

// handle records the configure serial, marks the surface configured and
// notifies the window layer.
func (xs *XdgSurface) handle(opcode uint16, d *decoder) error {
	if opcode != xdgSurfaceEvtConfigure {
		return nil
	}
	serial := d.getU32()
	if !d.ok {
		return fmt.Errorf("wayland: truncated xdg_surface.configure")
	}
	xs.lastSerial = serial
	xs.configured = true
	if xs.OnConfigure != nil {
		xs.OnConfigure(serial)
	}
	return nil
}

// Configured reports whether the compositor has sent the first configure.
func (xs *XdgSurface) Configured() bool { return xs.configured }

// LastSerial is the most recent configure serial.
func (xs *XdgSurface) LastSerial() uint32 { return xs.lastSerial }

// AckConfigure acknowledges a configure serial; the client must do this
// before committing the buffer that satisfies the configure.
func (xs *XdgSurface) AckConfigure(serial uint32) error {
	e := newEncoder(xs.conn.order)
	e.putU32(serial)
	return xs.conn.send(xs.id, xdgSurfaceReqAckConfigure, e.buf, nil)
}

// GetToplevel gives the xdg_surface the toplevel (application window) role.
func (xs *XdgSurface) GetToplevel() (*XdgToplevel, error) {
	id := xs.conn.allocID()
	e := newEncoder(xs.conn.order)
	e.putU32(id)
	if err := xs.conn.send(xs.id, xdgSurfaceReqGetToplevel, e.buf, nil); err != nil {
		return nil, err
	}
	tl := &XdgToplevel{conn: xs.conn, id: id}
	xs.conn.register(id, tl.handle)
	return tl, nil
}

// Destroy releases the xdg_surface object.
func (xs *XdgSurface) Destroy() error {
	err := xs.conn.send(xs.id, xdgSurfaceReqDestroy, nil, nil)
	xs.conn.unregister(xs.id)
	return err
}

// XdgToplevel is the application-window role: it carries the title/app-id
// and delivers resize (configure) and close intents.
type XdgToplevel struct {
	conn *Conn
	id   uint32
	// OnConfigure is called with the compositor-suggested size (0 means "you
	// choose") and the raw states array. The window layer resizes to it.
	OnConfigure func(width, height int, states []byte)
	// OnClose is called when the user asks to close the window.
	OnClose func()
}

// xdg_toplevel request opcodes.
const (
	xdgToplevelReqDestroy  = 0
	xdgToplevelReqSetTitle = 2
	xdgToplevelReqSetAppID = 3
)

// xdg_toplevel event opcodes.
const (
	xdgToplevelEvtConfigure = 0
	xdgToplevelEvtClose     = 1
)

// handle dispatches configure (size) and close.
func (tl *XdgToplevel) handle(opcode uint16, d *decoder) error {
	switch opcode {
	case xdgToplevelEvtConfigure:
		w := d.getI32()
		h := d.getI32()
		states := d.getArray()
		if !d.ok {
			return fmt.Errorf("wayland: truncated xdg_toplevel.configure")
		}
		if tl.OnConfigure != nil {
			tl.OnConfigure(int(w), int(h), states)
		}
		return nil
	case xdgToplevelEvtClose:
		if tl.OnClose != nil {
			tl.OnClose()
		}
		return nil
	default:
		return nil
	}
}

// SetTitle sets the window title.
func (tl *XdgToplevel) SetTitle(title string) error {
	e := newEncoder(tl.conn.order)
	e.putString(title)
	return tl.conn.send(tl.id, xdgToplevelReqSetTitle, e.buf, nil)
}

// SetAppID sets the application identifier (used for grouping / .desktop
// matching).
func (tl *XdgToplevel) SetAppID(appID string) error {
	e := newEncoder(tl.conn.order)
	e.putString(appID)
	return tl.conn.send(tl.id, xdgToplevelReqSetAppID, e.buf, nil)
}

// Destroy releases the xdg_toplevel object.
func (tl *XdgToplevel) Destroy() error {
	err := tl.conn.send(tl.id, xdgToplevelReqDestroy, nil, nil)
	tl.conn.unregister(tl.id)
	return err
}
