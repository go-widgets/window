// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package x11

import (
	"fmt"
	"io"
	"sync"

	xproto "github.com/go-freedesktop/x11"
)

// Conn is a connection to an X11 server speaking the core protocol over an
// arbitrary byte stream. It is transport-agnostic: NewConn wraps any
// io.ReadWriteCloser (a dialed unix socket in production, one half of a
// net.Pipe in tests) after the setup handshake has completed.
type Conn struct {
	rw    io.ReadWriteCloser
	order ByteOrder
	setup *Setup

	wmu sync.Mutex // serialises writes
	seq uint16     // last sequence number sent (server counts from 1)

	xidBase uint32
	xidMask uint32
	xidNext uint32

	events []Event // events buffered while awaiting a synchronous reply
}

// XError is a decoded X11 error reply.
type XError struct {
	Code     byte
	Seq      uint16
	BadValue uint32
	Major    byte
	Minor    uint16
}

func (e *XError) Error() string {
	return fmt.Sprintf("x11: server error code=%d major=%d minor=%d bad-value=%#x seq=%d",
		e.Code, e.Major, e.Minor, e.BadValue, e.Seq)
}

// Setup returns the parsed server setup.
func (c *Conn) Setup() *Setup { return c.setup }

// Order returns the negotiated wire byte order.
func (c *Conn) Order() ByteOrder { return c.order }

// Close closes the underlying transport.
func (c *Conn) Close() error { return c.rw.Close() }

// NewID allocates a fresh resource identifier from the server-granted
// range (base | (n & mask)).
func (c *Conn) NewID() uint32 {
	id := c.xidBase | (c.xidNext & c.xidMask)
	c.xidNext++
	return id
}

// sendRequest frames body (already 4-byte padded) with the 4-byte request
// header — opcode, the per-request data byte, and the total length in
// 4-byte units — writes it and advances the sequence counter.
func (c *Conn) sendRequest(opcode, data byte, body []byte) error {
	total := 4 + len(body)
	e := xproto.NewEncoder(c.order)
	e.Put8(opcode)
	e.Put8(data)
	e.Put16(uint16(total / 4))
	e.PutBytes(body)

	c.wmu.Lock()
	defer c.wmu.Unlock()
	if _, err := c.rw.Write(e.Bytes()); err != nil {
		return err
	}
	c.seq++
	return nil
}

// Seq returns the sequence number of the most recently sent request.
func (c *Conn) Seq() uint16 { return c.seq }

// SupportsFDPassing reports whether the connection's transport can pass a
// file descriptor to the server (required for MIT-SHM AttachFd).
func (c *Conn) SupportsFDPassing() bool {
	_, ok := c.rw.(FDSender)
	return ok
}

// sendRequestFD frames body with the 4-byte request header and writes it in a
// single sendmsg carrying fd as SCM_RIGHTS ancillary data. It errors if the
// transport cannot pass descriptors.
func (c *Conn) sendRequestFD(opcode, data byte, body []byte, fd int) error {
	fw, ok := c.rw.(FDSender)
	if !ok {
		return fmt.Errorf("x11: transport does not support fd passing")
	}
	total := 4 + len(body)
	e := xproto.NewEncoder(c.order)
	e.Put8(opcode)
	e.Put8(data)
	e.Put16(uint16(total / 4))
	e.PutBytes(body)

	c.wmu.Lock()
	defer c.wmu.Unlock()
	if err := fw.SendFD(e.Bytes(), fd); err != nil {
		return err
	}
	c.seq++
	return nil
}

// QueryExtension resolves an extension by name, returning whether the server
// implements it and, if so, its major opcode plus its first event and error
// codes. It is the standard gate before using any extension's requests.
func (c *Conn) QueryExtension(name string) (present bool, major, firstEvent, firstError byte, err error) {
	e := xproto.NewEncoder(c.order)
	e.Put16(uint16(len(name)))
	e.Skip(2) // unused
	e.PutString(name)
	reply, err := c.roundTrip(opQueryExtension, 0, e.Bytes())
	if err != nil {
		return false, 0, 0, 0, err
	}
	return reply[8] != 0, reply[9], reply[10], reply[11], nil
}

// readPacket reads one server packet: a fixed 32-byte error/event, or a
// reply (32-byte header plus its additional 4-byte-unit data block).
func (c *Conn) readPacket() ([]byte, error) {
	var head [32]byte
	if err := xproto.ReadFull(c.rw, head[:]); err != nil {
		return nil, err
	}
	if head[0] != pktReply {
		return head[:], nil
	}
	extra := int(c.order.Uint32(head[4:8])) * 4
	if extra == 0 {
		return head[:], nil
	}
	full := make([]byte, 32+extra)
	copy(full, head[:])
	if err := xproto.ReadFull(c.rw, full[32:]); err != nil {
		return nil, err
	}
	return full, nil
}

// roundTrip writes a request and waits for its reply, buffering any events
// that arrive in the meantime for later delivery via NextEvent. An error
// packet is returned as an *XError.
func (c *Conn) roundTrip(opcode, data byte, body []byte) ([]byte, error) {
	if err := c.sendRequest(opcode, data, body); err != nil {
		return nil, err
	}
	for {
		pkt, err := c.readPacket()
		if err != nil {
			return nil, err
		}
		switch pkt[0] {
		case pktError:
			return nil, c.decodeError(pkt)
		case pktReply:
			return pkt, nil
		default:
			c.events = append(c.events, decodeEvent(c.order, pkt))
		}
	}
}

// decodeError parses a 32-byte error packet.
func (c *Conn) decodeError(pkt []byte) *XError {
	d := xproto.NewDecoder(c.order, pkt)
	d.Skip(1) // 0
	code := d.Get8()
	seq := d.Get16()
	bad := d.Get32()
	minor := d.Get16()
	major := d.Get8()
	return &XError{Code: code, Seq: seq, BadValue: bad, Minor: minor, Major: major}
}

// NextEvent returns the next input/notify event, blocking on the transport
// until one arrives. Buffered events (queued during a roundTrip) drain
// first. Error packets encountered on the stream are returned as *XError.
func (c *Conn) NextEvent() (Event, error) {
	if len(c.events) > 0 {
		ev := c.events[0]
		c.events = c.events[1:]
		return ev, nil
	}
	for {
		pkt, err := c.readPacket()
		if err != nil {
			return Event{}, err
		}
		switch pkt[0] {
		case pktError:
			return Event{}, c.decodeError(pkt)
		case pktReply:
			// An unsolicited reply is unexpected in the event loop; ignore it.
			continue
		default:
			return decodeEvent(c.order, pkt), nil
		}
	}
}

// --- Request builders -----------------------------------------------------

// CreateWindow creates an InputOutput child of parent that inherits the
// parent's (root's) TrueColor visual and depth via CopyFromParent, setting
// only the background pixel, border pixel and event mask. Inheriting the
// visual sidesteps the BadMatch a differing-visual/colormap window would
// raise, while still landing on the screen's TrueColor root visual.
func (c *Conn) CreateWindow(wid, parent uint32, x, y int16, w, h uint16, backPixel, borderPixel, eventMask uint32) error {
	e := xproto.NewEncoder(c.order)
	e.Put32(wid)
	e.Put32(parent)
	e.Put16(uint16(x))
	e.Put16(uint16(y))
	e.Put16(w)
	e.Put16(h)
	e.Put16(0)                // border-width
	e.Put16(classInputOutput) // class
	e.Put32(CopyFromParent)   // visual
	e.Put32(cwBackPixel | cwBorderPixel | cwEventMask)
	e.Put32(backPixel)
	e.Put32(borderPixel)
	e.Put32(eventMask)
	return c.sendRequest(opCreateWindow, CopyFromParent, e.Bytes())
}

// MapWindow makes the window visible.
func (c *Conn) MapWindow(wid uint32) error {
	e := xproto.NewEncoder(c.order)
	e.Put32(wid)
	return c.sendRequest(opMapWindow, 0, e.Bytes())
}

// CreateGC creates a graphics context on drawable with default values.
func (c *Conn) CreateGC(gc, drawable uint32) error {
	e := xproto.NewEncoder(c.order)
	e.Put32(gc)
	e.Put32(drawable)
	e.Put32(0) // value-mask: no explicit values
	return c.sendRequest(opCreateGC, 0, e.Bytes())
}

// InternAtom resolves (or, when onlyIfExists is false, creates) an atom by
// name and returns its id.
func (c *Conn) InternAtom(name string, onlyIfExists bool) (uint32, error) {
	e := xproto.NewEncoder(c.order)
	e.Put16(uint16(len(name)))
	e.Put16(0) // unused
	e.PutString(name)
	data := byte(0)
	if onlyIfExists {
		data = 1
	}
	reply, err := c.roundTrip(opInternAtom, data, e.Bytes())
	if err != nil {
		return 0, err
	}
	return c.order.Uint32(reply[8:12]), nil
}

// ChangeProperty replaces property on window with data of the given type
// and format (8, 16 or 32 bits per element). count is the number of
// elements; data must already be laid out in the wire order.
func (c *Conn) ChangeProperty(window, property, typ uint32, format byte, count int, data []byte) error {
	e := xproto.NewEncoder(c.order)
	e.Put32(window)
	e.Put32(property)
	e.Put32(typ)
	e.Put8(format)
	e.Skip(3) // unused
	e.Put32(uint32(count))
	e.PutBytes(data)
	e.Pad(len(data))
	return c.sendRequest(opChangeProperty, propModeReplace, e.Bytes())
}

// SetWMName sets the window's WM_NAME (an ISO-8859-1 STRING property).
func (c *Conn) SetWMName(window uint32, name string) error {
	return c.ChangeProperty(window, AtomWMName, AtomString, 8, len(name), []byte(name))
}

// SetWMClass sets WM_CLASS to the two NUL-separated (and NUL-terminated)
// instance/class strings.
func (c *Conn) SetWMClass(window uint32, instance, class string) error {
	blob := append([]byte(instance), 0)
	blob = append(blob, []byte(class)...)
	blob = append(blob, 0)
	return c.ChangeProperty(window, AtomWMClass, AtomString, 8, len(blob), blob)
}

// SetWMProtocols sets WM_PROTOCOLS to the given atom list (format 32).
func (c *Conn) SetWMProtocols(window, wmProtocols uint32, atoms ...uint32) error {
	e := xproto.NewEncoder(c.order)
	for _, a := range atoms {
		e.Put32(a)
	}
	return c.ChangeProperty(window, wmProtocols, AtomAtom, 32, len(atoms), e.Bytes())
}

// GetKeyboardMapping fetches the keysym table for keycodes [first, first+count).
func (c *Conn) GetKeyboardMapping(first, count uint8) (*Keymap, error) {
	b1, b2 := keyboardMappingHeader(first, count)
	e := xproto.NewEncoder(c.order)
	e.Put8(b1)
	e.Put8(b2)
	e.Skip(2) // unused
	reply, err := c.roundTrip(opGetKeyboardMapping, 0, e.Bytes())
	if err != nil {
		return nil, err
	}
	perCode := reply[1]
	body := reply[32:]
	return parseKeyboardMapping(c.order, first, count, perCode, body), nil
}

// FetchKeymap fetches the full keyboard mapping for the server's advertised
// keycode range.
func (c *Conn) FetchKeymap() (*Keymap, error) {
	first := c.setup.MinKeycode
	count := c.setup.MaxKeycode - c.setup.MinKeycode + 1
	return c.GetKeyboardMapping(first, count)
}
