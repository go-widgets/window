// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package wayland

import (
	"encoding/binary"
	"errors"
	"testing"
)

func TestXdgWmBase(t *testing.T) {
	order := binary.LittleEndian
	st := &stubTransport{}
	c := NewConn(st, order)
	reg := &Registry{conn: c}
	reg.globals = []Global{{Name: 1, Interface: "xdg_wm_base", Version: 3}}

	wm, err := reg.XdgWmBase()
	if err != nil {
		t.Fatalf("XdgWmBase: %v", err)
	}
	// ping -> pong with the same serial.
	if err := wm.handle(xdgWmBaseEvtPing, newDecoder(order, bodyOf(order, func(e *encoder) { e.putU32(0x1234) }))); err != nil {
		t.Fatalf("ping: %v", err)
	}
	obj, op, d := lastWrite(t, st, order)
	if obj != wm.id || op != xdgWmBaseReqPong {
		t.Fatalf("pong obj=%d op=%d", obj, op)
	}
	if s := d.getU32(); s != 0x1234 {
		t.Errorf("pong serial = %#x", s)
	}
	// unknown event ignored, truncated ping errors.
	if err := wm.handle(99, newDecoder(order, nil)); err != nil {
		t.Errorf("unknown wm event = %v", err)
	}
	if err := wm.handle(xdgWmBaseEvtPing, newDecoder(order, nil)); err == nil {
		t.Error("truncated ping should error")
	}

	if err := wm.Destroy(); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if _, ok := c.handlers[wm.id]; ok {
		t.Error("Destroy should unregister wm_base")
	}
}

func TestXdgWmBaseBindWriteError(t *testing.T) {
	c := NewConn(&stubTransport{writeErr: errors.New("nope")}, binary.LittleEndian)
	reg := &Registry{conn: c}
	reg.globals = []Global{{Name: 1, Interface: "xdg_wm_base", Version: 3}}
	if _, err := reg.XdgWmBase(); err == nil {
		t.Error("xdg_wm_base bind write error should propagate")
	}
}

func TestXdgWmBasePongWriteError(t *testing.T) {
	c := NewConn(&stubTransport{writeErr: errors.New("nope")}, binary.LittleEndian)
	wm := &XdgWmBase{conn: c, id: 3}
	if err := wm.Pong(1); err == nil {
		t.Error("Pong write error should propagate")
	}
	if _, err := wm.GetXdgSurface(&Surface{conn: c, id: 4}); err == nil {
		t.Error("GetXdgSurface write error should propagate")
	}
	if err := wm.Destroy(); err == nil {
		t.Error("Destroy write error should propagate")
	}
}

func TestXdgSurface(t *testing.T) {
	order := binary.LittleEndian
	st := &stubTransport{}
	c := NewConn(st, order)
	wm := &XdgWmBase{conn: c, id: 3}
	surf := &Surface{conn: c, id: 5}
	xs, err := wm.GetXdgSurface(surf)
	if err != nil {
		t.Fatalf("GetXdgSurface: %v", err)
	}
	if xs.Configured() {
		t.Error("not configured before first configure")
	}

	var gotSerial uint32
	xs.OnConfigure = func(s uint32) { gotSerial = s }
	if err := xs.handle(xdgSurfaceEvtConfigure, newDecoder(order, bodyOf(order, func(e *encoder) { e.putU32(0xABCD) }))); err != nil {
		t.Fatalf("configure: %v", err)
	}
	if !xs.Configured() || xs.LastSerial() != 0xABCD || gotSerial != 0xABCD {
		t.Fatalf("configure state serial=%#x configured=%v cb=%#x", xs.LastSerial(), xs.Configured(), gotSerial)
	}
	// ack.
	if err := xs.AckConfigure(0xABCD); err != nil {
		t.Fatalf("AckConfigure: %v", err)
	}
	if obj, op, d := lastWrite(t, st, order); obj != xs.id || op != xdgSurfaceReqAckConfigure || d.getU32() != 0xABCD {
		t.Errorf("ack mismatch obj=%d op=%d", obj, op)
	}
	// unknown event ignored; truncated configure errors.
	if err := xs.handle(99, newDecoder(order, nil)); err != nil {
		t.Errorf("unknown xdg_surface event = %v", err)
	}
	if err := xs.handle(xdgSurfaceEvtConfigure, newDecoder(order, nil)); err == nil {
		t.Error("truncated configure should error")
	}
	// configure with no OnConfigure set still works.
	xs.OnConfigure = nil
	if err := xs.handle(xdgSurfaceEvtConfigure, newDecoder(order, bodyOf(order, func(e *encoder) { e.putU32(1) }))); err != nil {
		t.Fatal(err)
	}

	tl, err := xs.GetToplevel()
	if err != nil {
		t.Fatalf("GetToplevel: %v", err)
	}
	if tl.id == 0 {
		t.Error("toplevel id should be nonzero")
	}
	if err := xs.Destroy(); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
}

func TestXdgSurfaceWriteErrors(t *testing.T) {
	c := NewConn(&stubTransport{writeErr: errors.New("nope")}, binary.LittleEndian)
	xs := &XdgSurface{conn: c, id: 6}
	if err := xs.AckConfigure(1); err == nil {
		t.Error("AckConfigure write error")
	}
	if _, err := xs.GetToplevel(); err == nil {
		t.Error("GetToplevel write error")
	}
	if err := xs.Destroy(); err == nil {
		t.Error("Destroy write error")
	}
}

func TestXdgToplevel(t *testing.T) {
	order := binary.LittleEndian
	st := &stubTransport{}
	c := NewConn(st, order)
	tl := &XdgToplevel{conn: c, id: 7}

	if err := tl.SetTitle("hello"); err != nil {
		t.Fatalf("SetTitle: %v", err)
	}
	if obj, op, d := lastWrite(t, st, order); obj != tl.id || op != xdgToplevelReqSetTitle || d.getString() != "hello" {
		t.Errorf("set_title mismatch obj=%d op=%d", obj, op)
	}
	if err := tl.SetAppID("app.id"); err != nil {
		t.Fatalf("SetAppID: %v", err)
	}
	if _, op, d := lastWrite(t, st, order); op != xdgToplevelReqSetAppID || d.getString() != "app.id" {
		t.Errorf("set_app_id mismatch op=%d", op)
	}

	// configure delivers size + states.
	var gotW, gotH int
	var gotStates int
	tl.OnConfigure = func(w, h int, states []byte) { gotW, gotH, gotStates = w, h, len(states) }
	body := bodyOf(order, func(e *encoder) {
		e.putI32(800)
		e.putI32(600)
		e.putArray([]byte{4, 0, 0, 0}) // one state word
	})
	if err := tl.handle(xdgToplevelEvtConfigure, newDecoder(order, body)); err != nil {
		t.Fatalf("configure: %v", err)
	}
	if gotW != 800 || gotH != 600 || gotStates != 4 {
		t.Fatalf("configure got %dx%d states=%d", gotW, gotH, gotStates)
	}
	// configure with nil callback is fine.
	tl.OnConfigure = nil
	if err := tl.handle(xdgToplevelEvtConfigure, newDecoder(order, body)); err != nil {
		t.Fatal(err)
	}

	// close.
	closed := false
	tl.OnClose = func() { closed = true }
	if err := tl.handle(xdgToplevelEvtClose, nil); err != nil {
		t.Fatalf("close: %v", err)
	}
	if !closed {
		t.Error("OnClose not invoked")
	}
	tl.OnClose = nil
	if err := tl.handle(xdgToplevelEvtClose, nil); err != nil {
		t.Fatal(err)
	}
	// unknown event ignored; truncated configure errors.
	if err := tl.handle(99, nil); err != nil {
		t.Errorf("unknown toplevel event = %v", err)
	}
	if err := tl.handle(xdgToplevelEvtConfigure, newDecoder(order, nil)); err == nil {
		t.Error("truncated configure should error")
	}

	if err := tl.Destroy(); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
}

func TestXdgToplevelWriteErrors(t *testing.T) {
	c := NewConn(&stubTransport{writeErr: errors.New("nope")}, binary.LittleEndian)
	tl := &XdgToplevel{conn: c, id: 7}
	if err := tl.SetTitle("x"); err == nil {
		t.Error("SetTitle write error")
	}
	if err := tl.SetAppID("x"); err == nil {
		t.Error("SetAppID write error")
	}
	if err := tl.Destroy(); err == nil {
		t.Error("Destroy write error")
	}
}
