// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package wayland

import (
	"encoding/binary"
	"errors"
	"testing"
)

func TestCompositorAndSurface(t *testing.T) {
	order := binary.LittleEndian
	st := &stubTransport{}
	c := NewConn(st, order)
	reg := &Registry{conn: c}
	reg.globals = []Global{{Name: 1, Interface: "wl_compositor", Version: 4}}

	comp, err := reg.Compositor()
	if err != nil {
		t.Fatalf("Compositor: %v", err)
	}
	if err := comp.handle(0, nil); err != nil {
		t.Errorf("compositor.handle = %v", err)
	}
	surf, err := comp.CreateSurface()
	if err != nil {
		t.Fatalf("CreateSurface: %v", err)
	}
	if surf.ID() == 0 {
		t.Error("surface id should be nonzero")
	}
	if err := surf.handle(surfaceEvtEnter, nil); err != nil {
		t.Errorf("surface.handle enter = %v", err)
	}

	// Attach with a buffer marks it not-released; nil detaches.
	buf := &Buffer{conn: c, id: 77, released: true}
	if err := surf.Attach(buf, 1, 2); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if buf.released {
		t.Error("Attach should mark the buffer in-use")
	}
	obj, op, d := lastWrite(t, st, order)
	if obj != surf.id || op != surfaceReqAttach {
		t.Fatalf("attach obj=%d op=%d", obj, op)
	}
	if id := d.getU32(); id != 77 {
		t.Errorf("attach buffer id = %d", id)
	}
	if err := surf.Attach(nil, 0, 0); err != nil {
		t.Fatalf("Attach(nil): %v", err)
	}
	_, _, d = lastWrite(t, st, order)
	if id := d.getU32(); id != 0 {
		t.Errorf("detach buffer id = %d, want 0", id)
	}

	if err := surf.Damage(3, 4, 5, 6); err != nil {
		t.Fatalf("Damage: %v", err)
	}
	obj, op, d = lastWrite(t, st, order)
	if op != surfaceReqDamage {
		t.Fatalf("damage op=%d", op)
	}
	if x := d.getI32(); x != 3 {
		t.Errorf("damage x = %d", x)
	}
	if err := surf.DamageBuffer(1, 1, 2, 2); err != nil {
		t.Fatalf("DamageBuffer: %v", err)
	}
	if _, op, _ = lastWrite(t, st, order); op != surfaceReqDamageBuffer {
		t.Fatalf("damage_buffer op=%d", op)
	}
	if err := surf.Commit(); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if _, op, _ = lastWrite(t, st, order); op != surfaceReqCommit {
		t.Fatalf("commit op=%d", op)
	}

	cb, err := surf.Frame()
	if err != nil {
		t.Fatalf("Frame: %v", err)
	}
	if cb == nil || cb.done {
		t.Error("fresh frame callback should be pending")
	}
	if _, op, _ = lastWrite(t, st, order); op != surfaceReqFrame {
		t.Fatalf("frame op=%d", op)
	}

	if err := surf.Destroy(); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if _, ok := c.handlers[surf.id]; ok {
		t.Error("Destroy should unregister the surface")
	}
}

func TestSurfaceWriteErrors(t *testing.T) {
	c := NewConn(&stubTransport{writeErr: errors.New("nope")}, binary.LittleEndian)
	s := &Surface{conn: c, id: 5}
	if err := s.Attach(nil, 0, 0); err == nil {
		t.Error("Attach write error")
	}
	if err := s.Damage(0, 0, 1, 1); err == nil {
		t.Error("Damage write error")
	}
	if err := s.Commit(); err == nil {
		t.Error("Commit write error")
	}
	if _, err := s.Frame(); err == nil {
		t.Error("Frame write error")
	}
	if err := s.Destroy(); err == nil {
		t.Error("Destroy write error")
	}
}

func TestCompositorCreateSurfaceWriteError(t *testing.T) {
	c := NewConn(&stubTransport{writeErr: errors.New("nope")}, binary.LittleEndian)
	comp := &Compositor{conn: c, id: 3}
	if _, err := comp.CreateSurface(); err == nil {
		t.Error("CreateSurface write error should propagate")
	}
}

func TestCompositorBindWriteError(t *testing.T) {
	c := NewConn(&stubTransport{writeErr: errors.New("nope")}, binary.LittleEndian)
	reg := &Registry{conn: c}
	reg.globals = []Global{{Name: 1, Interface: "wl_compositor", Version: 4}}
	if _, err := reg.Compositor(); err == nil {
		t.Error("compositor bind write error should propagate")
	}
}

func TestCompositorBindLowerVersion(t *testing.T) {
	// When the compositor offers a lower version than we cap at, bind that.
	order := binary.LittleEndian
	st := &stubTransport{}
	c := NewConn(st, order)
	reg := &Registry{conn: c}
	reg.globals = []Global{{Name: 1, Interface: "wl_compositor", Version: 2}}
	if _, err := reg.Compositor(); err != nil {
		t.Fatalf("Compositor: %v", err)
	}
	_, _, d := lastWrite(t, st, order)
	d.getU32()      // name
	d.getString()   // interface
	if v := d.getU32(); v != 2 {
		t.Errorf("bound version = %d, want 2 (min of offered/cap)", v)
	}
}
