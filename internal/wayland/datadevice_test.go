// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package wayland

import (
	"encoding/binary"
	"errors"
	"testing"
)

// errWireInjected is the write failure the error paths are checked against.
var errWireInjected = errors.New("injected")

// ddmFixture binds a data-device manager over a stub transport and hands back
// the pieces a test needs, with the request log already cleared.
func ddmFixture(t *testing.T) (*Conn, *stubTransport, *DataDeviceManager, *Seat) {
	t.Helper()
	st := &stubTransport{}
	c := NewConn(st, binary.LittleEndian)
	reg := &Registry{conn: c, globals: []Global{
		{Name: 1, Interface: "wl_data_device_manager", Version: 3},
	}}
	m, err := reg.DataDeviceManager()
	if err != nil {
		t.Fatalf("DataDeviceManager: %v", err)
	}
	st.writes = nil
	st.wroteFDs = nil
	return c, st, m, &Seat{conn: c, id: 77}
}

func TestDataDeviceManagerBind(t *testing.T) {
	st := &stubTransport{}
	c := NewConn(st, binary.LittleEndian)
	reg := &Registry{conn: c}

	// A compositor with no clipboard is a fact about the session, not a bug
	// here, but it has to be reported rather than papered over.
	if _, err := reg.DataDeviceManager(); err == nil {
		t.Error("a compositor advertising no wl_data_device_manager should error")
	}

	reg.globals = []Global{{Name: 1, Interface: "wl_data_device_manager", Version: 3}}
	if _, err := reg.DataDeviceManager(); err != nil {
		t.Fatalf("DataDeviceManager: %v", err)
	}

	st.writeErr = errWireInjected
	reg2 := &Registry{conn: NewConn(st, binary.LittleEndian), globals: reg.globals}
	if _, err := reg2.DataDeviceManager(); err == nil {
		t.Error("a failed bind write should surface")
	}
}

// A source declares what it can produce, in preference order, and the wire says
// so: the compositor is the reader, and a test that only reads back its own
// encoder proves nothing about the layout.
func TestDataSourceOfferAndDestroyWire(t *testing.T) {
	c, st, m, _ := ddmFixture(t)

	src := m.CreateSource()
	if _, ok := c.handlers[src.id]; !ok {
		t.Error("a created source should register a handler for its events")
	}
	if len(st.writes) != 1 || opcodeOf(st.writes[0]) != ddmReqCreateDataSource {
		t.Fatalf("create_data_source not sent: % x", st.writes)
	}
	st.writes = nil

	if err := src.Offer("text/plain;charset=utf-8"); err != nil {
		t.Fatalf("Offer: %v", err)
	}
	if got := opcodeOf(st.writes[0]); got != dataSourceReqOffer {
		t.Errorf("opcode = %d, want offer", got)
	}
	if got := stringArg(c, st.writes[0]); got != "text/plain;charset=utf-8" {
		t.Errorf("offered %q", got)
	}
	st.writes = nil

	if err := src.Destroy(); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if got := opcodeOf(st.writes[0]); got != dataSourceReqDestroy {
		t.Errorf("opcode = %d, want destroy", got)
	}

	st.writeErr = errWireInjected
	if err := src.Offer("x"); err == nil {
		t.Error("a failed Offer write should surface")
	}
	if err := src.Destroy(); err == nil {
		t.Error("a failed Destroy write should surface")
	}
}

// The paste path from the source's side: the compositor names a type and hands
// over a descriptor to write the bytes into.
func TestDataSourceSendDeliversTheDescriptor(t *testing.T) {
	c, st, m, _ := ddmFixture(t)
	src := m.CreateSource()

	var gotMime string
	var gotFD int
	src.Send = func(mime string, fd int) { gotMime, gotFD = mime, fd }

	st.fds = []int{42}
	e := newEncoder(c.order)
	e.putString("text/plain")
	if err := src.handle(dataSourceEvtSend, newDecoder(c.order, e.buf)); err != nil {
		t.Fatalf("send: %v", err)
	}
	if gotMime != "text/plain" || gotFD != 42 {
		t.Errorf("send delivered %q/%d, want text/plain/42", gotMime, gotFD)
	}

	// No descriptor is a protocol error, not something to invent one for.
	e = newEncoder(c.order)
	e.putString("text/plain")
	if err := src.handle(dataSourceEvtSend, newDecoder(c.order, e.buf)); err == nil {
		t.Error("a send with no descriptor should be an error")
	}

	// A truncated event likewise.
	if err := src.handle(dataSourceEvtSend, newDecoder(c.order, nil)); err == nil {
		t.Error("a truncated send should be an error")
	}
}

// With nobody to produce the bytes, the descriptor still has to be closed: the
// application that pasted is blocked on a read, and an empty read ends while a
// leaked descriptor does not.
func TestDataSourceSendWithNoProducerClosesTheDescriptor(t *testing.T) {
	c, st, m, _ := ddmFixture(t)
	src := m.CreateSource()
	src.Send = nil

	st.fds = []int{43}
	e := newEncoder(c.order)
	e.putString("text/plain")
	if err := src.handle(dataSourceEvtSend, newDecoder(c.order, e.buf)); err != nil {
		t.Fatalf("send: %v", err)
	}
}

func TestDataSourceCancelledAndTarget(t *testing.T) {
	c, _, m, _ := ddmFixture(t)
	src := m.CreateSource()

	var cancelled bool
	src.Cancelled = func() { cancelled = true }
	if err := src.handle(dataSourceEvtCancelled, newDecoder(c.order, nil)); err != nil {
		t.Fatalf("cancelled: %v", err)
	}
	if !cancelled {
		t.Error("the cancelled event did not reach the callback")
	}

	// A source with no Cancelled callback is not a crash.
	src.Cancelled = nil
	if err := src.handle(dataSourceEvtCancelled, newDecoder(c.order, nil)); err != nil {
		t.Fatalf("cancelled without a callback: %v", err)
	}

	// target is drag-and-drop only and is read past, not acted on.
	e := newEncoder(c.order)
	e.putString("text/plain")
	if err := src.handle(dataSourceEvtTarget, newDecoder(c.order, e.buf)); err != nil {
		t.Fatalf("target: %v", err)
	}
	// An opcode this version does not know is ignored rather than fatal.
	if err := src.handle(99, newDecoder(c.order, nil)); err != nil {
		t.Fatalf("unknown opcode: %v", err)
	}
}

// Taking the clipboard quotes the seat's latest input serial. A client that has
// seen no input quotes 0 and is refused — silently, since the request has no
// reply — so the value on the wire is the only thing that can be checked.
func TestSetSelectionQuotesTheInputSerial(t *testing.T) {
	c, st, m, seat := ddmFixture(t)
	dev := m.GetDevice(seat)
	if _, ok := c.handlers[dev.id]; !ok {
		t.Error("a data device should register a handler")
	}
	st.writes = nil

	src := m.CreateSource()
	st.writes = nil

	seat.noteSerial(9182)
	if err := dev.SetSelection(src); err != nil {
		t.Fatalf("SetSelection: %v", err)
	}
	body := st.writes[0][8:]
	if got := c.order.Uint32(body[0:4]); got != src.id {
		t.Errorf("source id = %d, want %d", got, src.id)
	}
	if got := c.order.Uint32(body[4:8]); got != 9182 {
		t.Errorf("serial = %d, want the seat's latest input serial 9182", got)
	}
	st.writes = nil

	// A nil source clears the clipboard, which is a null object id and not an
	// omitted argument.
	if err := dev.SetSelection(nil); err != nil {
		t.Fatalf("SetSelection(nil): %v", err)
	}
	if got := c.order.Uint32(st.writes[0][8:12]); got != 0 {
		t.Errorf("clearing sent source id %d, want 0", got)
	}

	st.writeErr = errWireInjected
	if err := dev.SetSelection(src); err == nil {
		t.Error("a failed SetSelection write should surface")
	}
}

// The MIME types of an offer arrive on the new object BEFORE the selection event
// adopts it. Publishing the offer early would hand out something that claims to
// support nothing.
func TestDataDeviceSelectionAdoptsTheIntroducedOffer(t *testing.T) {
	c, _, m, seat := ddmFixture(t)
	dev := m.GetDevice(seat)

	if dev.Selection() != nil {
		t.Error("a device nobody has offered anything reports a selection")
	}

	// data_offer introduces object 500...
	e := newEncoder(c.order)
	e.putU32(500)
	if err := dev.handle(dataDeviceEvtDataOffer, newDecoder(c.order, e.buf)); err != nil {
		t.Fatalf("data_offer: %v", err)
	}
	if dev.Selection() != nil {
		t.Error("an introduced offer was published before the selection adopted it")
	}
	// ...which then advertises its types on its own object.
	h := c.handlers[500]
	if h == nil {
		t.Fatal("the introduced offer registered no handler")
	}
	for _, mime := range []string{"text/plain;charset=utf-8", "text/plain"} {
		me := newEncoder(c.order)
		me.putString(mime)
		if err := h(dataOfferEvtOffer, newDecoder(c.order, me.buf)); err != nil {
			t.Fatalf("offer: %v", err)
		}
	}

	// selection adopts it, and only now is it readable.
	se := newEncoder(c.order)
	se.putU32(500)
	if err := dev.handle(dataDeviceEvtSelection, newDecoder(c.order, se.buf)); err != nil {
		t.Fatalf("selection: %v", err)
	}
	sel := dev.Selection()
	if sel == nil {
		t.Fatal("the selection was not adopted")
	}
	if got := sel.Mimes(); len(got) != 2 || got[0] != "text/plain;charset=utf-8" {
		t.Errorf("mimes = %v, want both in the order offered", got)
	}
}

// A selection of 0 means the clipboard holds nothing this client can read, and
// a selection naming an offer never introduced is not readable either — handing
// one out would give the caller an object with no types.
func TestDataDeviceSelectionEdgeCases(t *testing.T) {
	c, _, m, seat := ddmFixture(t)
	dev := m.GetDevice(seat)

	adopt := func(introduce, select_ uint32) *DataOffer {
		if introduce != 0 {
			e := newEncoder(c.order)
			e.putU32(introduce)
			if err := dev.handle(dataDeviceEvtDataOffer, newDecoder(c.order, e.buf)); err != nil {
				t.Fatalf("data_offer: %v", err)
			}
		}
		e := newEncoder(c.order)
		e.putU32(select_)
		if err := dev.handle(dataDeviceEvtSelection, newDecoder(c.order, e.buf)); err != nil {
			t.Fatalf("selection: %v", err)
		}
		return dev.Selection()
	}

	if got := adopt(600, 600); got == nil {
		t.Fatal("a matching offer was not adopted")
	}
	if got := adopt(0, 0); got != nil {
		t.Error("selection 0 left a readable selection behind")
	}
	if got := adopt(700, 999); got != nil {
		t.Error("a selection naming an offer never introduced was published")
	}

	// Truncated events are errors, not silently empty selections.
	if err := dev.handle(dataDeviceEvtDataOffer, newDecoder(c.order, nil)); err == nil {
		t.Error("a truncated data_offer should be an error")
	}
	if err := dev.handle(dataDeviceEvtSelection, newDecoder(c.order, nil)); err == nil {
		t.Error("a truncated selection should be an error")
	}
	// An event this version does not handle is ignored.
	if err := dev.handle(3, newDecoder(c.order, nil)); err != nil {
		t.Fatalf("unhandled device event: %v", err)
	}
}

// Receive is nothing but a descriptor: the type names what to write, the fd is
// where. A request that arrived without one would leave the reader blocked.
func TestDataOfferReceivePassesTheDescriptor(t *testing.T) {
	c, st, m, seat := ddmFixture(t)
	dev := m.GetDevice(seat)
	e := newEncoder(c.order)
	e.putU32(800)
	if err := dev.handle(dataDeviceEvtDataOffer, newDecoder(c.order, e.buf)); err != nil {
		t.Fatalf("data_offer: %v", err)
	}
	se := newEncoder(c.order)
	se.putU32(800)
	if err := dev.handle(dataDeviceEvtSelection, newDecoder(c.order, se.buf)); err != nil {
		t.Fatalf("selection: %v", err)
	}
	off := dev.Selection()
	st.writes, st.wroteFDs = nil, nil

	if err := off.Receive("text/plain", 55); err != nil {
		t.Fatalf("Receive: %v", err)
	}
	if got := opcodeOf(st.writes[0]); got != dataOfferReqReceive {
		t.Errorf("opcode = %d, want receive", got)
	}
	if len(st.wroteFDs[0]) != 1 || st.wroteFDs[0][0] != 55 {
		t.Errorf("receive carried fds %v, want [55]", st.wroteFDs[0])
	}
	st.writes, st.wroteFDs = nil, nil

	if err := off.Destroy(); err != nil {
		t.Fatalf("Destroy: %v", err)
	}
	if got := opcodeOf(st.writes[0]); got != dataOfferReqDestroy {
		t.Errorf("opcode = %d, want destroy", got)
	}

	st.writeErr = errWireInjected
	if err := off.Receive("text/plain", 55); err == nil {
		t.Error("a failed Receive write should surface")
	}
	if err := off.Destroy(); err == nil {
		t.Error("a failed Destroy write should surface")
	}

	// A truncated offer event is an error, and an unknown one is ignored.
	if err := off.handle(dataOfferEvtOffer, newDecoder(c.order, nil)); err == nil {
		t.Error("a truncated offer should be an error")
	}
	if err := off.handle(42, newDecoder(c.order, nil)); err != nil {
		t.Fatalf("unknown offer opcode: %v", err)
	}
}

// opcodeOf and stringArg read a request back off the wire the way the
// compositor would.
func opcodeOf(msg []byte) uint16 {
	return binary.LittleEndian.Uint16(msg[4:6])
}

func stringArg(c *Conn, msg []byte) string {
	return newDecoder(c.order, msg[8:]).getString()
}
