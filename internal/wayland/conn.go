// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package wayland

import (
	"fmt"
	"sync"
)

// handler dispatches one event (identified by opcode) for a single object,
// reading its arguments from d. Returning an error is fatal to the session.
type handler func(opcode uint16, d *decoder) error

// Conn is a Wayland connection: the object table, the request encoder and
// the event dispatcher over a transport. It is transport-agnostic — the
// same machine drives a real UNIX socket in production and an in-process
// fake compositor in tests.
type Conn struct {
	t     transport
	order ByteOrder

	wmu      sync.Mutex         // serialises request writes
	nextID   uint32             // next client-allocated object id
	handlers map[uint32]handler // object id -> event dispatcher

	display *Display
	err     error // latched fatal protocol error (from wl_display.error)
}

// displayID is the well-known object id of the wl_display singleton; it is
// implicitly present on every connection before any request is sent.
const displayID = 1

// firstClientID is the first object id a client may allocate (id 1 is the
// display).
const firstClientID = 2

// NewConn builds a connection over t using the given wire byte order and
// installs the wl_display singleton. Order is normally NativeOrder.
func NewConn(t transport, order ByteOrder) *Conn {
	c := &Conn{
		t:        t,
		order:    order,
		nextID:   firstClientID,
		handlers: make(map[uint32]handler),
	}
	c.display = newDisplay(c)
	return c
}

// Display returns the wl_display singleton.
func (c *Conn) Display() *Display { return c.display }

// Close releases the underlying transport.
func (c *Conn) Close() error { return c.t.Close() }

// Err returns the latched fatal protocol error, if any.
func (c *Conn) Err() error { return c.err }

// allocID reserves a fresh client object id.
func (c *Conn) allocID() uint32 {
	id := c.nextID
	c.nextID++
	return id
}

// register installs an event handler for object id.
func (c *Conn) register(id uint32, h handler) { c.handlers[id] = h }

// unregister removes an object's handler (after it is destroyed).
func (c *Conn) unregister(id uint32) { delete(c.handlers, id) }

// send frames and writes one request: the 8-byte header (object id, then
// size<<16|opcode) followed by the already-encoded, already-padded body,
// carrying fds out-of-band.
func (c *Conn) send(objID uint32, opcode uint16, body []byte, fds []int) error {
	total := 8 + len(body)
	e := newEncoder(c.order)
	e.putU32(objID)
	e.putU32(uint32(opcode) | uint32(total)<<16)
	e.putBytes(body)

	c.wmu.Lock()
	defer c.wmu.Unlock()
	return c.t.write(e.buf, fds)
}

// recvFD returns the next file descriptor the compositor passed over
// SCM_RIGHTS, oldest first. Handlers for events carrying an fd (e.g.
// wl_keyboard.keymap) call it in argument order.
func (c *Conn) recvFD() (int, bool) { return c.t.popFD() }

// Dispatch reads and delivers exactly one event. Events for objects with
// no handler (e.g. an object destroyed after the compositor queued an
// event for it) are read and discarded. A latched protocol error is
// returned in preference to anything else.
func (c *Conn) Dispatch() error {
	if c.err != nil {
		return c.err
	}
	msg, err := c.t.read()
	if err != nil {
		return err
	}
	d := newDecoder(c.order, msg)
	objID := d.getU32()
	word := d.getU32()
	if !d.ok {
		return fmt.Errorf("wayland: truncated message header")
	}
	opcode := uint16(word & 0xffff)
	h := c.handlers[objID]
	if h == nil {
		return nil
	}
	if err := h(opcode, d); err != nil {
		return err
	}
	return c.err
}

// Roundtrip issues a wl_display.sync and dispatches events until the
// resulting callback fires, i.e. until the compositor has processed every
// request sent before the sync. It is the Wayland analogue of an X11
// round-tripping request.
func (c *Conn) Roundtrip() error {
	cb, err := c.display.sync()
	if err != nil {
		return err
	}
	for !cb.done {
		if err := c.Dispatch(); err != nil {
			return err
		}
	}
	return nil
}

// --- wl_display -----------------------------------------------------------

// Display is the wl_display singleton (object id 1): the root of every
// connection. It creates the registry and issues synchronisation
// callbacks, and it is the sink for global protocol errors.
type Display struct {
	conn *Conn
	id   uint32
}

// wl_display request opcodes.
const (
	displayReqSync        = 0
	displayReqGetRegistry = 1
)

// wl_display event opcodes.
const (
	displayEvtError    = 0
	displayEvtDeleteID = 1
)

// newDisplay installs the wl_display handler at the well-known id.
func newDisplay(c *Conn) *Display {
	d := &Display{conn: c, id: displayID}
	c.register(d.id, d.handle)
	return d
}

// handle dispatches wl_display events. error latches a fatal error;
// delete_id acknowledges server-side id recycling (the id becomes free,
// but this sovereign client never reuses ids, so it only unregisters).
func (d *Display) handle(opcode uint16, dec *decoder) error {
	switch opcode {
	case displayEvtError:
		objID := dec.getU32()
		code := dec.getU32()
		msg := dec.getString()
		if !dec.ok {
			return fmt.Errorf("wayland: truncated wl_display.error")
		}
		d.conn.err = fmt.Errorf("wayland: protocol error on object %d (code %d): %s", objID, code, msg)
		return d.conn.err
	case displayEvtDeleteID:
		id := dec.getU32()
		if !dec.ok {
			return fmt.Errorf("wayland: truncated wl_display.delete_id")
		}
		d.conn.unregister(id)
		return nil
	default:
		return nil
	}
}

// sync issues wl_display.sync, returning the callback that fires once the
// compositor has processed all prior requests.
func (d *Display) sync() (*Callback, error) {
	cb := newCallback(d.conn)
	e := newEncoder(d.conn.order)
	e.putU32(cb.id)
	if err := d.conn.send(d.id, displayReqSync, e.buf, nil); err != nil {
		return nil, err
	}
	return cb, nil
}

// GetRegistry issues wl_display.get_registry and returns the registry proxy.
func (d *Display) GetRegistry() (*Registry, error) {
	r := &Registry{conn: d.conn, id: d.conn.allocID()}
	d.conn.register(r.id, r.handle)
	e := newEncoder(d.conn.order)
	e.putU32(r.id)
	if err := d.conn.send(d.id, displayReqGetRegistry, e.buf, nil); err != nil {
		return nil, err
	}
	return r, nil
}

// --- wl_callback ----------------------------------------------------------

// Callback is a one-shot wl_callback: it fires its done event once and is
// then finished.
type Callback struct {
	conn *Conn
	id   uint32
	done bool
	data uint32
}

// wl_callback event opcode.
const callbackEvtDone = 0

// newCallback allocates and registers a callback object.
func newCallback(c *Conn) *Callback {
	cb := &Callback{conn: c, id: c.allocID()}
	c.register(cb.id, cb.handle)
	return cb
}

// handle records the done event and retires the callback.
func (cb *Callback) handle(opcode uint16, d *decoder) error {
	if opcode != callbackEvtDone {
		return nil
	}
	cb.data = d.getU32()
	if !d.ok {
		return fmt.Errorf("wayland: truncated wl_callback.done")
	}
	cb.done = true
	cb.conn.unregister(cb.id)
	return nil
}

// --- wl_registry ----------------------------------------------------------

// Global is one advertised global: a compositor-assigned name, the
// interface it implements and the maximum version offered.
type Global struct {
	Name      uint32
	Interface string
	Version   uint32
}

// Registry is wl_registry: it enumerates the compositor's globals and binds
// them into interface proxies.
type Registry struct {
	conn    *Conn
	id      uint32
	globals []Global
}

// wl_registry request opcode.
const registryReqBind = 0

// wl_registry event opcodes.
const (
	registryEvtGlobal       = 0
	registryEvtGlobalRemove = 1
)

// handle dispatches wl_registry events, maintaining the live global list.
func (r *Registry) handle(opcode uint16, d *decoder) error {
	switch opcode {
	case registryEvtGlobal:
		g := Global{Name: d.getU32(), Interface: d.getString(), Version: d.getU32()}
		if !d.ok {
			return fmt.Errorf("wayland: truncated wl_registry.global")
		}
		r.globals = append(r.globals, g)
		return nil
	case registryEvtGlobalRemove:
		name := d.getU32()
		if !d.ok {
			return fmt.Errorf("wayland: truncated wl_registry.global_remove")
		}
		r.remove(name)
		return nil
	default:
		return nil
	}
}

// remove drops the global with the given name, if present.
func (r *Registry) remove(name uint32) {
	for i := range r.globals {
		if r.globals[i].Name == name {
			r.globals = append(r.globals[:i], r.globals[i+1:]...)
			return
		}
	}
}

// Globals returns a copy of the currently advertised globals.
func (r *Registry) Globals() []Global {
	return append([]Global(nil), r.globals...)
}

// Find returns the advertised global for the given interface name and
// whether it was found. When several versions are advertised the first is
// returned (compositors advertise one global per interface).
func (r *Registry) Find(iface string) (Global, bool) {
	for _, g := range r.globals {
		if g.Interface == iface {
			return g, true
		}
	}
	return Global{}, false
}

// bind issues wl_registry.bind for the named global at the negotiated
// version, allocating and returning the new object's id. The caller wraps
// the id in the appropriate interface proxy and registers its handler.
func (r *Registry) bind(name uint32, iface string, version uint32) (uint32, error) {
	id := r.conn.allocID()
	e := newEncoder(r.conn.order)
	e.putU32(name)
	e.putString(iface)
	e.putU32(version)
	e.putU32(id)
	if err := r.conn.send(r.id, registryReqBind, e.buf, nil); err != nil {
		return 0, err
	}
	return id, nil
}
