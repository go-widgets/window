// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package x11

import (
	xproto "github.com/go-freedesktop/x11"
)

// Showing, hiding and raising a top-level window.
//
// The window manager owns all three. A client asks; it does not do. So each of
// these is what ICCCM and EWMH tell a client to send, and none of them waits
// for an answer: every request here is one-way, which is what lets a caller on
// another goroutine use them while the event loop owns the socket's read side.

const (
	opUnmapWindow     = 10
	opConfigureWindow = 12

	evUnmapNotify = 18

	// ConfigureWindow value-mask bit and value for the stacking order.
	configStackMode = 0x0040
	stackAbove      = 0

	// The mask a client message to the ROOT window travels with, so that the
	// window manager -- which selects SubstructureRedirect there -- gets it.
	eventMaskSubstructureNotify   = 0x00080000
	eventMaskSubstructureRedirect = 0x00100000
	rootMessageMask               = eventMaskSubstructureNotify | eventMaskSubstructureRedirect

	// NetActiveWindowAtom is the EWMH message a client sends to have one of its
	// windows activated.
	NetActiveWindowAtom = "_NET_ACTIVE_WINDOW"
	// netSourceApplication is _NET_ACTIVE_WINDOW's source indication for a
	// request made by an ordinary application (as opposed to a pager, 2).
	netSourceApplication = 1
)

// UnmapWindow takes the window off the screen.
func (c *Conn) UnmapWindow(wid uint32) error {
	e := xproto.NewEncoder(c.order)
	e.Put32(wid)
	return c.sendRequest(opUnmapWindow, 0, e.Bytes())
}

// Withdraw hides a top-level window the way ICCCM 4.1.4 says to: unmap it, then
// send the root a synthetic UnmapNotify. A plain unmap is enough for the X
// server, but a reparenting window manager distinguishes "withdrawn" from
// "iconified" by that second message, and a window that is only unmapped can
// come back as an icon in a taskbar instead of going away.
func (c *Conn) Withdraw(root, wid uint32) error {
	if err := c.UnmapWindow(wid); err != nil {
		return err
	}
	ev := xproto.NewEncoder(c.order)
	ev.Put8(evUnmapNotify)
	ev.Put8(0)  // unused
	ev.Put16(0) // sequence, filled in by the server
	ev.Put32(root)
	ev.Put32(wid)
	ev.Put8(0) // from-configure: false
	for len(ev.Bytes()) < 32 {
		ev.Put8(0)
	}
	return c.sendEvent(root, rootMessageMask, ev.Bytes())
}

// RaiseWindow puts the window at the top of its siblings' stacking order.
func (c *Conn) RaiseWindow(wid uint32) error {
	e := xproto.NewEncoder(c.order)
	e.Put32(wid)
	e.Put16(configStackMode)
	e.Put16(0) // pad
	e.Put32(stackAbove)
	return c.sendRequest(opConfigureWindow, 0, e.Bytes())
}

// RequestActivate asks the window manager to activate wid -- raise it, switch
// to its desktop, give it the focus -- with the EWMH _NET_ACTIVE_WINDOW client
// message, which netActive is the interned atom of.
//
// It is a REQUEST. A window manager with focus-stealing prevention may decide
// otherwise and mark the window as wanting attention instead, and one that does
// not speak EWMH ignores it; [Conn.RaiseWindow] is what still works there.
func (c *Conn) RequestActivate(root, wid, netActive uint32) error {
	ev := xproto.NewEncoder(c.order)
	ev.Put8(evClientMessage)
	ev.Put8(32) // format
	ev.Put16(0) // sequence
	ev.Put32(wid)
	ev.Put32(netActive)
	ev.Put32(netSourceApplication)
	ev.Put32(0) // timestamp: CurrentTime
	ev.Put32(0) // the requestor's currently active window: none known
	for len(ev.Bytes()) < 32 {
		ev.Put8(0)
	}
	return c.sendEvent(root, rootMessageMask, ev.Bytes())
}

// sendEvent is the SendEvent request: event (32 bytes) delivered to dest's
// clients selecting mask, without propagation.
func (c *Conn) sendEvent(dest, mask uint32, event []byte) error {
	e := xproto.NewEncoder(c.order)
	e.Put32(dest)
	e.Put32(mask)
	e.PutBytes(event)
	return c.sendRequest(opSendEvent, 0, e.Bytes())
}
