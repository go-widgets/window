// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package x11

// Waking a blocked event loop, which on X11 means sending yourself an event.
//
// A client that is waiting for the next event is blocked on a socket read, and
// nothing but the server can end that wait. So the way to interrupt it is to
// make the server send you something: SendEvent addressed to your OWN window,
// with an empty event-mask, which the server delivers to the window's owner —
// us. Xlib applications have woken XNextEvent this way for thirty years.
//
// The alternative, polling the socket with a timeout, trades a wakeup for a
// permanent stream of them, and gets the latency wrong in both directions.

// SendClientMessage sends a 32-bit-format ClientMessage of type typeAtom to
// window, carrying data as its first data word.
//
// The event-mask is zero, which is what makes this a message to ourselves: the
// server delivers a masked SendEvent to the clients selecting for it, and an
// unmasked one to the window's owner. The delivered event has the SendEvent bit
// set, so a handler can tell it from anything the server generated on its own.
func (c *Conn) SendClientMessage(window, typeAtom, data uint32) error {
	// A ClientMessage is 32 bytes, laid out by hand: the client sends events
	// rarely enough that a general encoder would be more code than this.
	ev := newEncoder(c.order)
	ev.put8(evClientMessage)
	ev.put8(32) // format: 32-bit data words
	ev.put16(0) // sequence, filled in by the server
	ev.put32(window)
	ev.put32(typeAtom)
	ev.put32(data)
	for len(ev.buf) < 32 {
		ev.put8(0) // the four unused data words
	}

	e := newEncoder(c.order)
	e.put32(window)
	e.put32(0) // event-mask 0: deliver to the window's own client
	e.putBytes(ev.buf)
	return c.sendRequest(opSendEvent, 0, e.buf) // propagate = 0
}
