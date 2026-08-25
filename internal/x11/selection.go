// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package x11

import (
	"time"

	xproto "github.com/go-freedesktop/x11"
)

// The X11 selection protocol, which is what a clipboard is on this platform.
//
// There is no clipboard server to ask. A selection is OWNED by a window, and
// the owner is asked for the data whenever somebody wants it — so copying is
// claiming ownership and then answering questions, and pasting is asking the
// current owner and waiting for the answer to arrive as an event. That is why
// this needs the event loop and cannot be a pair of straight-line calls, and
// why text copied from an application that has since exited is simply gone.

// Selection request opcodes.
const (
	opGetProperty       = 20
	opSetSelectionOwner = 22
	opGetSelectionOwner = 23
	opConvertSelection  = 24
	opSendEvent         = 25
)

// Selection event codes.
const (
	evSelectionClear   = 29
	evSelectionRequest = 30
	evSelectionNotify  = 31
)

// CurrentTime asks the server to substitute its own clock, which is what a
// selection request should use when it has no user event to point at.
const CurrentTime = 0

// SetSelectionOwner claims (owner non-zero) or releases (owner zero) a
// selection. The server sends the previous owner a SelectionClear.
//
// Claiming is not the same as having copied: nothing is transferred here. The
// owner has to stay alive and answer SelectionRequest events for as long as the
// text is meant to remain pasteable.
func (c *Conn) SetSelectionOwner(owner, selection, time uint32) error {
	e := xproto.NewEncoder(c.order)
	e.Put32(owner)
	e.Put32(selection)
	e.Put32(time)
	return c.sendRequest(opSetSelectionOwner, 0, e.Bytes())
}

// GetSelectionOwner returns the window currently owning selection, or 0 when
// nobody does — which is the normal state of a fresh session, not an error.
func (c *Conn) GetSelectionOwner(selection uint32) (uint32, error) {
	e := xproto.NewEncoder(c.order)
	e.Put32(selection)
	reply, err := c.roundTrip(opGetSelectionOwner, 0, e.Bytes())
	if err != nil {
		return 0, err
	}
	return c.order.Uint32(reply[8:12]), nil
}

// ConvertSelection asks the current owner to write selection, converted to
// target, into property on requestor. The answer does not come back here: the
// owner replies with a SelectionNotify event, and the data is read from the
// property afterwards.
func (c *Conn) ConvertSelection(requestor, selection, target, property, time uint32) error {
	e := xproto.NewEncoder(c.order)
	e.Put32(requestor)
	e.Put32(selection)
	e.Put32(target)
	e.Put32(property)
	e.Put32(time)
	return c.sendRequest(opConvertSelection, 0, e.Bytes())
}

// GetProperty reads up to maxWords 32-bit words of a property, optionally
// deleting it. It returns the property's type (0 when the property does not
// exist), its format in bits, and the raw bytes.
//
// Deleting on read is what the requestor side wants: the property is a mailbox
// the owner wrote into, and leaving it behind would make the next paste read a
// stale answer if the owner failed to reply.
func (c *Conn) GetProperty(window, property, typ uint32, del bool, maxWords uint32) (retTyp uint32, format byte, data []byte, err error) {
	e := xproto.NewEncoder(c.order)
	e.Put32(window)
	e.Put32(property)
	e.Put32(typ)
	e.Put32(0) // long-offset
	e.Put32(maxWords)
	d := byte(0)
	if del {
		d = 1
	}
	reply, err := c.roundTrip(opGetProperty, d, e.Bytes())
	if err != nil {
		return 0, 0, nil, err
	}
	format = reply[1]
	retTyp = c.order.Uint32(reply[8:12])
	n := c.order.Uint32(reply[16:20]) // value length, in FORMAT units
	var size uint32
	switch format {
	case 8:
		size = n
	case 16:
		size = n * 2
	case 32:
		size = n * 4
	}
	if int(size) > len(reply)-32 {
		size = uint32(len(reply) - 32)
	}
	return retTyp, format, reply[32 : 32+size], nil
}

// SendSelectionNotify answers a SelectionRequest. property is the one the
// requestor named once the data has been written there, or 0 to refuse — which
// is the correct answer for a target we cannot produce, and much better than
// silence, since a requestor with no reply can only wait.
func (c *Conn) SendSelectionNotify(requestor, selection, target, property, time uint32) error {
	// A SelectionNotify event is 32 bytes, laid out by hand because this is the
	// only place the client sends an event rather than receiving one.
	ev := xproto.NewEncoder(c.order)
	ev.Put8(evSelectionNotify)
	ev.Put8(0) // unused
	ev.Put16(0)
	ev.Put32(time)
	ev.Put32(requestor)
	ev.Put32(selection)
	ev.Put32(target)
	ev.Put32(property)
	for len(ev.Bytes()) < 32 {
		ev.Put8(0)
	}

	e := xproto.NewEncoder(c.order)
	e.Put32(requestor)
	e.Put32(0) // event-mask: 0 delivers to the requestor itself
	e.PutBytes(ev.Bytes())
	return c.sendRequest(opSendEvent, 0, e.Bytes()) // propagate = 0
}

// PushEvent returns an event to the head of the queue, so it is delivered by
// the next NextEvent.
//
// A synchronous exchange -- asking for a selection and waiting for the reply --
// has to read events that are not the reply, and those belong to the
// application, not to the exchange. Dropping them loses a click; handling them
// there would re-enter the widget tree from inside a paste. Putting them back
// is the only option that does neither.
func (c *Conn) PushEvent(ev Event) {
	c.events = append([]Event{ev}, c.events...)
}

// WaitReadable reports whether the server sent something within d, and whether
// the transport could answer at all.
//
// It exists because the selection protocol has no timeout. A paste asks whoever
// owns the clipboard and waits for an event that only arrives if that owner is
// still alive and still answering; an owner that died between claiming and being
// asked leaves the asker blocked for ever -- a frozen window, on Ctrl+V, with
// nothing in any log to say why.
//
// It waits for READABILITY rather than putting a deadline on the read, and the
// difference matters: a deadline that expires between a packet's header and its
// body leaves the stream desynchronised, turning a slow paste into a broken
// connection. Waiting first and reading only when there is something to read
// cannot cut a packet in half.
func (c *Conn) WaitReadable(d time.Duration) (ready, supported bool) {
	w, ok := c.rw.(xproto.Waiter)
	if !ok {
		return false, false
	}
	return w.WaitReadable(d), true
}
