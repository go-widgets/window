// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package wayland

import (
	"encoding/binary"
	"errors"
	"testing"
)

func TestNewConnBasics(t *testing.T) {
	st := &stubTransport{}
	c := NewConn(st, binary.LittleEndian)
	if c.Display() == nil || c.Display().id != displayID {
		t.Fatal("display singleton missing")
	}
	if got := c.allocID(); got != firstClientID {
		t.Fatalf("first allocID = %d, want %d", got, firstClientID)
	}
	if got := c.allocID(); got != firstClientID+1 {
		t.Fatalf("second allocID = %d", got)
	}
	if c.Err() != nil {
		t.Fatalf("fresh conn Err = %v", c.Err())
	}
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if !st.closed {
		t.Error("Close should close the transport")
	}
}

// TestRegistryHandshake drives the real socket path end to end: get the
// registry, receive two globals, round-trip to the sync callback, and
// verify Find/Globals.
func TestRegistryHandshake(t *testing.T) {
	bothOrders(t, func(t *testing.T, order ByteOrder) {
		conn, fs := newTestConn(t, order)
		serverErr := make(chan error, 1)
		go func() {
			// get_registry
			obj, op, d, err := fs.readReq()
			if err != nil {
				serverErr <- err
				return
			}
			if obj != displayID || op != displayReqGetRegistry {
				serverErr <- errUnexpected("get_registry", obj, op)
				return
			}
			regID := d.getU32()
			g1 := bodyOf(order, func(e *encoder) { e.putU32(1); e.putString("wl_compositor"); e.putU32(4) })
			g2 := bodyOf(order, func(e *encoder) { e.putU32(2); e.putString("wl_shm"); e.putU32(1) })
			_ = fs.sendEvt(regID, registryEvtGlobal, g1)
			_ = fs.sendEvt(regID, registryEvtGlobal, g2)
			// sync
			obj, op, d, err = fs.readReq()
			if err != nil {
				serverErr <- err
				return
			}
			if obj != displayID || op != displayReqSync {
				serverErr <- errUnexpected("sync", obj, op)
				return
			}
			cbID := d.getU32()
			_ = fs.sendEvt(cbID, callbackEvtDone, bodyOf(order, func(e *encoder) { e.putU32(0) }))
			serverErr <- nil
		}()

		reg, err := conn.Display().GetRegistry()
		if err != nil {
			t.Fatalf("GetRegistry: %v", err)
		}
		if err := conn.Roundtrip(); err != nil {
			t.Fatalf("Roundtrip: %v", err)
		}
		if err := <-serverErr; err != nil {
			t.Fatalf("server: %v", err)
		}
		if g, ok := reg.Find("wl_compositor"); !ok || g.Name != 1 || g.Version != 4 {
			t.Fatalf("Find(wl_compositor) = %+v ok=%v", g, ok)
		}
		if _, ok := reg.Find("nope"); ok {
			t.Error("Find of absent interface should be false")
		}
		if len(reg.Globals()) != 2 {
			t.Fatalf("Globals len = %d, want 2", len(reg.Globals()))
		}
	})
}

func errUnexpected(what string, obj uint32, op uint16) error {
	return errors.New("server: unexpected " + what)
}

func TestRegistryBind(t *testing.T) {
	order := binary.LittleEndian
	conn, fs := newTestConn(t, order)
	reg := &Registry{conn: conn, id: 2}
	id, err := reg.bind(5, "wl_compositor", 4)
	if err != nil {
		t.Fatalf("bind: %v", err)
	}
	if id != firstClientID {
		t.Fatalf("bind id = %d, want %d", id, firstClientID)
	}
	obj, op, d, err := fs.readReq()
	if err != nil {
		t.Fatalf("readReq: %v", err)
	}
	if obj != 2 || op != registryReqBind {
		t.Fatalf("bind req obj=%d op=%d", obj, op)
	}
	if name := d.getU32(); name != 5 {
		t.Errorf("bind name = %d, want 5", name)
	}
	if iface := d.getString(); iface != "wl_compositor" {
		t.Errorf("bind iface = %q", iface)
	}
	if ver := d.getU32(); ver != 4 {
		t.Errorf("bind version = %d", ver)
	}
	if newID := d.getU32(); newID != id {
		t.Errorf("bind new_id = %d, want %d", newID, id)
	}
}

func TestRegistryGlobalRemove(t *testing.T) {
	order := binary.LittleEndian
	c := NewConn(&stubTransport{}, order)
	reg := &Registry{conn: c, id: 2}
	// Two globals, then remove the first.
	if err := reg.handle(registryEvtGlobal, newDecoder(order, bodyOf(order, func(e *encoder) { e.putU32(1); e.putString("a"); e.putU32(1) }))); err != nil {
		t.Fatal(err)
	}
	if err := reg.handle(registryEvtGlobal, newDecoder(order, bodyOf(order, func(e *encoder) { e.putU32(2); e.putString("b"); e.putU32(1) }))); err != nil {
		t.Fatal(err)
	}
	if err := reg.handle(registryEvtGlobalRemove, newDecoder(order, bodyOf(order, func(e *encoder) { e.putU32(1) }))); err != nil {
		t.Fatal(err)
	}
	if _, ok := reg.Find("a"); ok {
		t.Error("global a should be removed")
	}
	if _, ok := reg.Find("b"); !ok {
		t.Error("global b should remain")
	}
	// Removing an unknown name is a no-op.
	reg.remove(999)
	// An unknown opcode is ignored.
	if err := reg.handle(99, newDecoder(order, nil)); err != nil {
		t.Errorf("unknown registry opcode = %v", err)
	}
}

func TestRegistryHandleTruncated(t *testing.T) {
	order := binary.LittleEndian
	c := NewConn(&stubTransport{}, order)
	reg := &Registry{conn: c, id: 2}
	if err := reg.handle(registryEvtGlobal, newDecoder(order, nil)); err == nil {
		t.Error("truncated global should error")
	}
	if err := reg.handle(registryEvtGlobalRemove, newDecoder(order, nil)); err == nil {
		t.Error("truncated global_remove should error")
	}
}

func TestDisplayError(t *testing.T) {
	order := binary.LittleEndian
	c := NewConn(&stubTransport{}, order)
	body := bodyOf(order, func(e *encoder) { e.putU32(7); e.putU32(3); e.putString("bad object") })
	err := c.display.handle(displayEvtError, newDecoder(order, body))
	if err == nil {
		t.Fatal("wl_display.error should return an error")
	}
	if c.Err() == nil {
		t.Fatal("wl_display.error should latch Conn.Err")
	}
	// Once latched, Dispatch surfaces it immediately.
	if got := c.Dispatch(); got == nil {
		t.Error("Dispatch after latched error should return it")
	}
	// Roundtrip too.
	if got := c.Roundtrip(); got == nil {
		t.Error("Roundtrip after latched error should return it")
	}
}

func TestDisplayErrorTruncated(t *testing.T) {
	order := binary.LittleEndian
	c := NewConn(&stubTransport{}, order)
	if err := c.display.handle(displayEvtError, newDecoder(order, nil)); err == nil {
		t.Error("truncated error event should error")
	}
}

func TestDisplayDeleteID(t *testing.T) {
	order := binary.LittleEndian
	c := NewConn(&stubTransport{}, order)
	c.register(42, func(uint16, *decoder) error { return nil })
	body := bodyOf(order, func(e *encoder) { e.putU32(42) })
	if err := c.display.handle(displayEvtDeleteID, newDecoder(order, body)); err != nil {
		t.Fatalf("delete_id: %v", err)
	}
	if _, ok := c.handlers[42]; ok {
		t.Error("delete_id should unregister the object")
	}
	// Truncated delete_id errors.
	if err := c.display.handle(displayEvtDeleteID, newDecoder(order, nil)); err == nil {
		t.Error("truncated delete_id should error")
	}
	// Unknown display opcode is ignored.
	if err := c.display.handle(77, newDecoder(order, nil)); err != nil {
		t.Errorf("unknown display opcode = %v", err)
	}
}

func TestCallback(t *testing.T) {
	order := binary.LittleEndian
	c := NewConn(&stubTransport{}, order)
	cb := newCallback(c)
	// Unknown opcode ignored, callback not done.
	if err := cb.handle(99, newDecoder(order, nil)); err != nil {
		t.Fatal(err)
	}
	if cb.done {
		t.Fatal("callback should not be done after unknown opcode")
	}
	// done sets the flag and unregisters.
	if err := cb.handle(callbackEvtDone, newDecoder(order, bodyOf(order, func(e *encoder) { e.putU32(99) }))); err != nil {
		t.Fatal(err)
	}
	if !cb.done || cb.data != 99 {
		t.Fatalf("callback done=%v data=%d", cb.done, cb.data)
	}
	if _, ok := c.handlers[cb.id]; ok {
		t.Error("callback should unregister after done")
	}
	// Truncated done errors.
	cb2 := newCallback(c)
	if err := cb2.handle(callbackEvtDone, newDecoder(order, nil)); err == nil {
		t.Error("truncated done should error")
	}
}

func TestDispatchUnknownObject(t *testing.T) {
	order := binary.LittleEndian
	st := &stubTransport{reads: [][]byte{frame(order, 999, 0, nil)}}
	c := NewConn(st, order)
	if err := c.Dispatch(); err != nil {
		t.Errorf("dispatch to unknown object = %v, want nil", err)
	}
}

func TestDispatchTruncatedHeader(t *testing.T) {
	order := binary.LittleEndian
	// A 4-byte message: object id present, header word missing.
	st := &stubTransport{reads: [][]byte{{1, 0, 0, 0}}}
	c := NewConn(st, order)
	if err := c.Dispatch(); err == nil {
		t.Error("truncated header should error")
	}
}

func TestDispatchReadError(t *testing.T) {
	order := binary.LittleEndian
	st := &stubTransport{readErr: errors.New("boom")}
	c := NewConn(st, order)
	if err := c.Dispatch(); err == nil {
		t.Error("read error should propagate")
	}
}

func TestDispatchHandlerError(t *testing.T) {
	order := binary.LittleEndian
	// A wl_display.error event dispatched through the full path.
	body := bodyOf(order, func(e *encoder) { e.putU32(1); e.putU32(2); e.putString("x") })
	st := &stubTransport{reads: [][]byte{frame(order, displayID, displayEvtError, body)}}
	c := NewConn(st, order)
	if err := c.Dispatch(); err == nil {
		t.Error("handler error should propagate through Dispatch")
	}
}

func TestSyncWriteError(t *testing.T) {
	st := &stubTransport{writeErr: errors.New("nope")}
	c := NewConn(st, binary.LittleEndian)
	if _, err := c.display.sync(); err == nil {
		t.Error("sync should surface a write error")
	}
	if err := c.Roundtrip(); err == nil {
		t.Error("Roundtrip should surface a sync write error")
	}
}

func TestGetRegistryWriteError(t *testing.T) {
	st := &stubTransport{writeErr: errors.New("nope")}
	c := NewConn(st, binary.LittleEndian)
	if _, err := c.Display().GetRegistry(); err == nil {
		t.Error("GetRegistry should surface a write error")
	}
}

func TestBindWriteError(t *testing.T) {
	st := &stubTransport{writeErr: errors.New("nope")}
	c := NewConn(st, binary.LittleEndian)
	reg := &Registry{conn: c, id: 2}
	if _, err := reg.bind(1, "wl_shm", 1); err == nil {
		t.Error("bind should surface a write error")
	}
}
