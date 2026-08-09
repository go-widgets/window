// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package x11

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"syscall"
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

// Handshake runs the client connection setup over rw: it sends the
// byte-order sentinel, protocol 11.0 and the authorization name+data, then
// parses the reply. On success it returns a ready Conn. order selects the
// wire byte order (little- or big-endian); both are valid and the server
// adopts the client's choice.
func Handshake(rw io.ReadWriteCloser, order ByteOrder, authName string, authData []byte) (*Conn, error) {
	sentinel := byte(orderMSB)
	if order == binary.LittleEndian {
		sentinel = orderLSB
	}
	req := buildSetupRequest(order, sentinel, authName, authData)
	if _, err := rw.Write(req); err != nil {
		return nil, err
	}

	var hdr [8]byte
	if err := readFull(rw, hdr[:]); err != nil {
		return nil, err
	}
	status := hdr[0]
	// The additional-data length (in 4-byte units) sits at bytes 6..7 in the
	// client's chosen order.
	addLen := int(order.Uint16(hdr[6:8])) * 4
	body := make([]byte, addLen)
	if err := readFull(rw, body); err != nil {
		return nil, err
	}

	switch status {
	case 0: // Failed
		reasonLen := int(hdr[1])
		reason := ""
		if reasonLen <= len(body) {
			reason = string(body[:reasonLen])
		}
		return nil, fmt.Errorf("x11: setup refused: %s", reason)
	case 2: // Authenticate
		return nil, fmt.Errorf("x11: further authentication required: %s", trimNul(body))
	case 1: // Success
	default:
		return nil, fmt.Errorf("x11: unknown setup status %d", status)
	}

	s, err := parseSetupReply(order, body)
	if err != nil {
		return nil, err
	}
	c := &Conn{
		rw:      rw,
		order:   order,
		setup:   s,
		xidBase: s.ResourceIDBase,
		xidMask: s.ResourceIDMask,
	}
	return c, nil
}

// trimNul returns b up to its first NUL, as a string (for vendor reason
// text that may be zero-padded).
func trimNul(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
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
	e := newEncoder(c.order)
	e.put8(opcode)
	e.put8(data)
	e.put16(uint16(total / 4))
	e.putBytes(body)

	c.wmu.Lock()
	defer c.wmu.Unlock()
	if _, err := c.rw.Write(e.buf); err != nil {
		return err
	}
	c.seq++
	return nil
}

// Seq returns the sequence number of the most recently sent request.
func (c *Conn) Seq() uint16 { return c.seq }

// FDSender is implemented by a transport that can pass a file descriptor
// alongside a request over the same socket (a UNIX-domain stream, via
// SCM_RIGHTS). The production connection's transport (see WrapUnix)
// implements it; the in-process net.Pipe transport used by most tests does
// not, so the MIT-SHM fd-passing path degrades to plain PutImage when it is
// absent. The method is exported so an alternative transport (a measurement
// or test harness) can provide it too.
type FDSender interface {
	// SendFD writes one already-framed request with fd attached as a single
	// SCM_RIGHTS control message.
	SendFD(msg []byte, fd int) error
}

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
	e := newEncoder(c.order)
	e.put8(opcode)
	e.put8(data)
	e.put16(uint16(total / 4))
	e.putBytes(body)

	c.wmu.Lock()
	defer c.wmu.Unlock()
	if err := fw.SendFD(e.buf, fd); err != nil {
		return err
	}
	c.seq++
	return nil
}

// QueryExtension resolves an extension by name, returning whether the server
// implements it and, if so, its major opcode plus its first event and error
// codes. It is the standard gate before using any extension's requests.
func (c *Conn) QueryExtension(name string) (present bool, major, firstEvent, firstError byte, err error) {
	e := newEncoder(c.order)
	e.put16(uint16(len(name)))
	e.skip(2) // unused
	e.putString(name)
	reply, err := c.roundTrip(opQueryExtension, 0, e.buf)
	if err != nil {
		return false, 0, 0, 0, err
	}
	return reply[8] != 0, reply[9], reply[10], reply[11], nil
}

// unixRW adapts a *net.UnixConn to the Conn transport, adding fd passing over
// SCM_RIGHTS. It is what lets the production connection attach a shared-memory
// descriptor to a MIT-SHM AttachFd request while every other request still
// travels as an ordinary socket write.
type unixRW struct{ c *net.UnixConn }

// WrapUnix wraps a dialed *net.UnixConn as an fd-passing transport for
// Handshake. A connection built over it reports SupportsFDPassing() == true.
func WrapUnix(c *net.UnixConn) io.ReadWriteCloser { return &unixRW{c: c} }

func (u *unixRW) Read(b []byte) (int, error)  { return u.c.Read(b) }
func (u *unixRW) Write(b []byte) (int, error) { return u.c.Write(b) }
func (u *unixRW) Close() error                { return u.c.Close() }

// SendFD writes msg with fd attached as a single SCM_RIGHTS control message.
func (u *unixRW) SendFD(msg []byte, fd int) error {
	oob := syscall.UnixRights(fd)
	_, _, err := u.c.WriteMsgUnix(msg, oob, nil)
	return err
}

// readPacket reads one server packet: a fixed 32-byte error/event, or a
// reply (32-byte header plus its additional 4-byte-unit data block).
func (c *Conn) readPacket() ([]byte, error) {
	var head [32]byte
	if err := readFull(c.rw, head[:]); err != nil {
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
	if err := readFull(c.rw, full[32:]); err != nil {
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
	d := newDecoder(c.order, pkt)
	d.skip(1) // 0
	code := d.get8()
	seq := d.get16()
	bad := d.get32()
	minor := d.get16()
	major := d.get8()
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
	e := newEncoder(c.order)
	e.put32(wid)
	e.put32(parent)
	e.put16(uint16(x))
	e.put16(uint16(y))
	e.put16(w)
	e.put16(h)
	e.put16(0)                // border-width
	e.put16(classInputOutput) // class
	e.put32(CopyFromParent)   // visual
	e.put32(cwBackPixel | cwBorderPixel | cwEventMask)
	e.put32(backPixel)
	e.put32(borderPixel)
	e.put32(eventMask)
	return c.sendRequest(opCreateWindow, CopyFromParent, e.buf)
}

// MapWindow makes the window visible.
func (c *Conn) MapWindow(wid uint32) error {
	e := newEncoder(c.order)
	e.put32(wid)
	return c.sendRequest(opMapWindow, 0, e.buf)
}

// CreateGC creates a graphics context on drawable with default values.
func (c *Conn) CreateGC(gc, drawable uint32) error {
	e := newEncoder(c.order)
	e.put32(gc)
	e.put32(drawable)
	e.put32(0) // value-mask: no explicit values
	return c.sendRequest(opCreateGC, 0, e.buf)
}

// InternAtom resolves (or, when onlyIfExists is false, creates) an atom by
// name and returns its id.
func (c *Conn) InternAtom(name string, onlyIfExists bool) (uint32, error) {
	e := newEncoder(c.order)
	e.put16(uint16(len(name)))
	e.put16(0) // unused
	e.putString(name)
	data := byte(0)
	if onlyIfExists {
		data = 1
	}
	reply, err := c.roundTrip(opInternAtom, data, e.buf)
	if err != nil {
		return 0, err
	}
	return c.order.Uint32(reply[8:12]), nil
}

// ChangeProperty replaces property on window with data of the given type
// and format (8, 16 or 32 bits per element). count is the number of
// elements; data must already be laid out in the wire order.
func (c *Conn) ChangeProperty(window, property, typ uint32, format byte, count int, data []byte) error {
	e := newEncoder(c.order)
	e.put32(window)
	e.put32(property)
	e.put32(typ)
	e.put8(format)
	e.skip(3) // unused
	e.put32(uint32(count))
	e.putBytes(data)
	e.pad(len(data))
	return c.sendRequest(opChangeProperty, propModeReplace, e.buf)
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
	e := newEncoder(c.order)
	for _, a := range atoms {
		e.put32(a)
	}
	return c.ChangeProperty(window, wmProtocols, AtomAtom, 32, len(atoms), e.buf)
}

// GetKeyboardMapping fetches the keysym table for keycodes [first, first+count).
func (c *Conn) GetKeyboardMapping(first, count uint8) (*Keymap, error) {
	b1, b2 := keyboardMappingHeader(first, count)
	e := newEncoder(c.order)
	e.put8(b1)
	e.put8(b2)
	e.skip(2) // unused
	reply, err := c.roundTrip(opGetKeyboardMapping, 0, e.buf)
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
