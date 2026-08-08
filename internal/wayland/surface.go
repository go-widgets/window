// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package wayland

// Compositor is the wl_compositor global: it creates surfaces (and regions,
// unused here).
type Compositor struct {
	conn *Conn
	id   uint32
}

const compositorIfaceVersion = 4

// wl_compositor request opcode.
const compositorReqCreateSurface = 0

// bindCompositor binds the wl_compositor global from the registry.
func bindCompositor(reg *Registry, g Global) (*Compositor, error) {
	ver := min32(g.Version, compositorIfaceVersion)
	id, err := reg.bind(g.Name, "wl_compositor", ver)
	if err != nil {
		return nil, err
	}
	c := &Compositor{conn: reg.conn, id: id}
	reg.conn.register(id, c.handle)
	return c, nil
}

// handle: wl_compositor has no events; the dispatcher is a no-op.
func (c *Compositor) handle(uint16, *decoder) error { return nil }

// CreateSurface issues wl_compositor.create_surface and returns the surface.
func (c *Compositor) CreateSurface() (*Surface, error) {
	id := c.conn.allocID()
	e := newEncoder(c.conn.order)
	e.putU32(id)
	if err := c.conn.send(c.id, compositorReqCreateSurface, e.buf, nil); err != nil {
		return nil, err
	}
	s := &Surface{conn: c.conn, id: id}
	c.conn.register(id, s.handle)
	return s, nil
}

// Surface is a wl_surface: the drawable region attached to a shell role and
// filled from a wl_buffer.
type Surface struct {
	conn *Conn
	id   uint32
}

// wl_surface request opcodes.
const (
	surfaceReqDestroy      = 0
	surfaceReqAttach       = 1
	surfaceReqDamage       = 2
	surfaceReqFrame        = 3
	surfaceReqCommit       = 6
	surfaceReqDamageBuffer = 9
)

// wl_surface event opcodes (enter/leave carry a wl_output the client tracks
// for scale/placement; this backend ignores them).
const (
	surfaceEvtEnter = 0
	surfaceEvtLeave = 1
)

// ID returns the surface's object id.
func (s *Surface) ID() uint32 { return s.id }

// handle ignores wl_surface events (enter/leave/preferred_*).
func (s *Surface) handle(uint16, *decoder) error { return nil }

// Attach binds buf as the surface's pending content at the given offset. A
// nil buffer detaches (attaches the null object).
func (s *Surface) Attach(buf *Buffer, x, y int) error {
	var bufID uint32
	if buf != nil {
		bufID = buf.id
		buf.released = false
	}
	e := newEncoder(s.conn.order)
	e.putU32(bufID)
	e.putI32(int32(x))
	e.putI32(int32(y))
	return s.conn.send(s.id, surfaceReqAttach, e.buf, nil)
}

// Damage marks a rectangle of the surface (in surface coordinates) as
// changed since the last commit.
func (s *Surface) Damage(x, y, w, h int) error {
	return s.rect(surfaceReqDamage, x, y, w, h)
}

// DamageBuffer marks a rectangle in buffer coordinates as changed (the
// scale-independent damage request preferred since wl_surface v4).
func (s *Surface) DamageBuffer(x, y, w, h int) error {
	return s.rect(surfaceReqDamageBuffer, x, y, w, h)
}

// rect emits a four-int rectangle request.
func (s *Surface) rect(opcode uint16, x, y, w, h int) error {
	e := newEncoder(s.conn.order)
	e.putI32(int32(x))
	e.putI32(int32(y))
	e.putI32(int32(w))
	e.putI32(int32(h))
	return s.conn.send(s.id, opcode, e.buf, nil)
}

// Commit atomically applies the pending surface state (attached buffer,
// damage, frame request) to the displayed surface.
func (s *Surface) Commit() error {
	return s.conn.send(s.id, surfaceReqCommit, nil, nil)
}

// Frame requests a throttling callback that fires when the compositor is
// ready for the next frame; the returned Callback's done event carries a
// timestamp and marks it ready.
func (s *Surface) Frame() (*Callback, error) {
	cb := newCallback(s.conn)
	e := newEncoder(s.conn.order)
	e.putU32(cb.id)
	if err := s.conn.send(s.id, surfaceReqFrame, e.buf, nil); err != nil {
		return nil, err
	}
	return cb, nil
}

// Destroy releases the surface object.
func (s *Surface) Destroy() error {
	err := s.conn.send(s.id, surfaceReqDestroy, nil, nil)
	s.conn.unregister(s.id)
	return err
}
