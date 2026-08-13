// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package wayland

import "fmt"

// The wl_data_device family, which is what a clipboard is on Wayland.
//
// It is neither X11's selection ownership nor a system store. The compositor
// brokers: to copy, a client creates a DATA SOURCE, tells the compositor which
// MIME types it can produce, and hands it over with set_selection; when somebody
// pastes, the compositor asks the source to write its bytes into a PIPE it
// supplies. To paste, the client is handed a DATA OFFER, asks it for a type, and
// reads the other application's bytes out of a pipe of its own.
//
// So the data never passes through the compositor's memory, and both directions
// are file descriptors over the protocol socket. That also means a copy outlives
// the copying application only if the compositor keeps a proxy for it — which
// most do not, exactly as on X11.

const dataDeviceManagerIfaceVersion = 3

// wl_data_device_manager request opcodes.
const (
	ddmReqCreateDataSource = 0
	ddmReqGetDataDevice    = 1
)

// wl_data_source request opcodes.
const (
	dataSourceReqOffer   = 0
	dataSourceReqDestroy = 1
)

// wl_data_source event opcodes.
const (
	dataSourceEvtTarget    = 0
	dataSourceEvtSend      = 1
	dataSourceEvtCancelled = 2
)

// wl_data_device request opcodes.
const (
	dataDeviceReqStartDrag    = 0
	dataDeviceReqSetSelection = 1
	dataDeviceReqRelease      = 2
)

// wl_data_device event opcodes.
const (
	dataDeviceEvtDataOffer = 0
	dataDeviceEvtSelection = 5
)

// wl_data_offer request opcodes.
const (
	dataOfferReqAccept  = 0
	dataOfferReqReceive = 1
	dataOfferReqDestroy = 2
)

// wl_data_offer event opcode.
const dataOfferEvtOffer = 0

// DataDeviceManager is the wl_data_device_manager global: the factory for
// sources (what this client can give) and devices (what it can be given).
type DataDeviceManager struct {
	conn *Conn
	id   uint32
}

// DataDeviceManager finds and binds the wl_data_device_manager global.
//
// A compositor without one has no clipboard to offer, which is a fact about the
// session rather than an error in this client — a bare surface-only compositor
// is a legitimate thing to run.
func (r *Registry) DataDeviceManager() (*DataDeviceManager, error) {
	g, ok := r.Find("wl_data_device_manager")
	if !ok {
		return nil, fmt.Errorf("wayland: compositor advertises no wl_data_device_manager")
	}
	id, err := r.bind(g.Name, "wl_data_device_manager", min32(g.Version, dataDeviceManagerIfaceVersion))
	if err != nil {
		return nil, err
	}
	return &DataDeviceManager{conn: r.conn, id: id}, nil
}

// DataSource is something this client can hand out: a set of MIME types and the
// bytes behind them.
type DataSource struct {
	conn *Conn
	id   uint32

	// Send is called when somebody pastes: the compositor names the MIME type
	// it wants and supplies a file descriptor to write the bytes into. The
	// callback owns the descriptor and must close it — leaving it open leaves
	// the paster blocked on a read that will never end.
	Send func(mime string, fd int)
	// Cancelled fires when the selection is taken over by another client, or
	// when the compositor is done with the source. Nothing more will be asked
	// of it.
	Cancelled func()
}

// CreateSource makes a data source.
func (m *DataDeviceManager) CreateSource() *DataSource {
	id := m.conn.allocID()
	e := newEncoder(m.conn.order)
	e.putU32(id)
	// A failed write latches on the connection and surfaces on the next
	// dispatch; there is nothing useful a caller could do with it here that it
	// will not learn there.
	_ = m.conn.send(m.id, ddmReqCreateDataSource, e.buf, nil)
	s := &DataSource{conn: m.conn, id: id}
	m.conn.register(id, s.handle)
	return s
}

// Offer declares a MIME type this source can produce. Order is preference: the
// first type both sides understand is the one that gets used.
func (s *DataSource) Offer(mime string) error {
	e := newEncoder(s.conn.order)
	e.putString(mime)
	return s.conn.send(s.id, dataSourceReqOffer, e.buf, nil)
}

// Destroy releases the source. A source destroyed while it owns the selection
// takes the clipboard's contents with it.
func (s *DataSource) Destroy() error {
	return s.conn.send(s.id, dataSourceReqDestroy, nil, nil)
}

func (s *DataSource) handle(opcode uint16, d *decoder) error {
	switch opcode {
	case dataSourceEvtSend:
		mime := d.getString()
		if !d.ok {
			return fmt.Errorf("wayland: truncated wl_data_source.send")
		}
		fd, ok := s.conn.recvFD()
		if !ok {
			return fmt.Errorf("wayland: wl_data_source.send arrived without a descriptor")
		}
		if s.Send == nil {
			// Nobody to produce the bytes, but the descriptor is ours now and
			// the paster is waiting on it. Closing it is the honest answer:
			// an empty read ends, a leaked one does not.
			closeFD(fd)
			return nil
		}
		s.Send(mime, fd)
	case dataSourceEvtCancelled:
		if s.Cancelled != nil {
			s.Cancelled()
		}
	case dataSourceEvtTarget:
		d.getString() // drag-and-drop only: which type the pointer is over
	}
	return nil
}

// DataOffer is something another client is offering: the MIME types it can
// produce, and a way to ask for the bytes.
type DataOffer struct {
	conn  *Conn
	id    uint32
	mimes []string
}

// Mimes are the types the offer advertises, in the order it advertised them.
func (o *DataOffer) Mimes() []string { return o.mimes }

// Receive asks the offer for one MIME type, writing into fd. The descriptor is
// the COMPOSITOR's to write to and the caller's to read from and close: pass the
// write end of a pipe, close it locally, and read the other end.
func (o *DataOffer) Receive(mime string, fd int) error {
	e := newEncoder(o.conn.order)
	e.putString(mime)
	return o.conn.send(o.id, dataOfferReqReceive, e.buf, []int{fd})
}

// Destroy releases the offer.
func (o *DataOffer) Destroy() error {
	return o.conn.send(o.id, dataOfferReqDestroy, nil, nil)
}

func (o *DataOffer) handle(opcode uint16, d *decoder) error {
	if opcode == dataOfferEvtOffer {
		mime := d.getString()
		if !d.ok {
			return fmt.Errorf("wayland: truncated wl_data_offer.offer")
		}
		o.mimes = append(o.mimes, mime)
	}
	return nil
}

// DataDevice is one seat's view of the clipboard: what it can be given, and
// where a selection is announced.
type DataDevice struct {
	conn *Conn
	id   uint32
	seat *Seat

	// pending is the offer the compositor has introduced but not yet attached
	// to anything. It becomes the selection when the selection event names it.
	pending   *DataOffer
	selection *DataOffer
}

// GetDevice makes the data device for a seat.
func (m *DataDeviceManager) GetDevice(seat *Seat) *DataDevice {
	id := m.conn.allocID()
	e := newEncoder(m.conn.order)
	e.putU32(id)
	e.putU32(seat.id)
	_ = m.conn.send(m.id, ddmReqGetDataDevice, e.buf, nil)
	dev := &DataDevice{conn: m.conn, id: id, seat: seat}
	m.conn.register(id, dev.handle)
	return dev
}

// SetSelection puts source on the clipboard, quoting the seat's most recent
// input serial.
//
// The serial is not ceremony: a compositor grants the clipboard on the strength
// of a real user event, so a client that has never been interacted with is
// refused. That refusal is silent — there is no reply to this request — which is
// why a caller that has seen no input should expect nothing to happen.
func (d *DataDevice) SetSelection(source *DataSource) error {
	e := newEncoder(d.conn.order)
	if source == nil {
		e.putU32(0) // a null source clears the selection
	} else {
		e.putU32(source.id)
	}
	e.putU32(d.seat.LastSerial())
	return d.conn.send(d.id, dataDeviceReqSetSelection, e.buf, nil)
}

// Selection is the offer currently on the clipboard, or nil when it holds
// nothing this client can read.
func (d *DataDevice) Selection() *DataOffer { return d.selection }

func (d *DataDevice) handle(opcode uint16, dec *decoder) error {
	switch opcode {
	case dataDeviceEvtDataOffer:
		id := dec.getU32()
		if !dec.ok {
			return fmt.Errorf("wayland: truncated wl_data_device.data_offer")
		}
		o := &DataOffer{conn: d.conn, id: id}
		d.conn.register(id, o.handle)
		// The MIME types arrive as separate events on the new object BEFORE
		// the selection event that adopts it, so it is held aside rather than
		// published: an offer published early would be read while it still
		// claims to support nothing.
		d.pending = o
	case dataDeviceEvtSelection:
		id := dec.getU32()
		if !dec.ok {
			return fmt.Errorf("wayland: truncated wl_data_device.selection")
		}
		old := d.selection
		switch {
		case id == 0:
			// The clipboard now holds nothing this client can read.
			d.selection = nil
		case d.pending != nil && d.pending.id == id:
			d.selection = d.pending
		default:
			// A selection naming an offer we never saw introduced. Nothing can
			// be read from it, and pretending otherwise would hand out an
			// object with no MIME types.
			d.selection = nil
		}
		d.pending = nil
		if old != nil && old != d.selection {
			_ = old.Destroy()
		}
	}
	return nil
}
