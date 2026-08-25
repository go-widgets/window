// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package x11

import (
	"fmt"

	xproto "github.com/go-freedesktop/x11"
)

// Enumerating the DISPLAYS an X screen is made of.
//
// An X screen is one coordinate space; the physical panels are laid out inside
// it. RANDR 1.5 is what says where, and the enumeration is not here: it is in
// github.com/go-freedesktop/x11, which this package already shares with
// github.com/go-freedesktop/screencast. Which rectangle is where does not
// depend on whether a client is capturing the pixels or putting a window on
// them, and two copies of a protocol parser drift silently until something
// fails on one back-end only.
//
// What is here is the adapter — [Conn.Request], the whole of what the shared
// enumeration asks of a connection — and the one piece that IS a windowing
// question: _NET_WORKAREA, the area a window manager leaves for windows once
// its panels and docks have taken theirs.

// Monitor is one physical display's rectangle inside an X screen. It is a name
// for xproto.Monitor, not a copy.
type Monitor = xproto.Monitor

// Request sends one request and returns its reply — the 32-byte fixed part
// followed by its additional data — which is what [xproto.Requester] asks of a
// connection. op names the request, so a failure says which one rather than
// only which opcode.
func (c *Conn) Request(op string, opcode, data byte, body []byte) ([]byte, error) {
	reply, err := c.roundTrip(opcode, data, body)
	if err != nil {
		return nil, fmt.Errorf("x11: %s: %w", op, err)
	}
	return reply, nil
}

// Monitors lists the displays of the given screen, over RANDR 1.5 with
// XINERAMA and the whole screen as fallbacks. It never returns an empty list
// without an error.
func (c *Conn) Monitors(screen int) ([]Monitor, error) {
	return xproto.Monitors(c, c.setup.ScreenOf(screen))
}

// Conn must satisfy the interface the shared enumeration asks through;
// asserting it here turns a signature change upstream into a build failure
// rather than a runtime one.
var _ xproto.Requester = (*Conn)(nil)

// The EWMH properties that describe what a window manager has left for
// windows. Both are on the root, both are CARDINAL lists, and both are absent
// under a bare X server with no window manager — which is not an error, just a
// desktop with nothing reserved on it.
const (
	netWorkAreaAtom       = "_NET_WORKAREA"
	netCurrentDesktopAtom = "_NET_CURRENT_DESKTOP"
)

// workAreaWords caps the read. _NET_WORKAREA is four CARDINALs per virtual
// desktop; 256 desktops is far past any desktop environment's limit and keeps
// a malformed property from asking for an unbounded reply.
const workAreaWords = 4 * 256

// WorkArea returns the area of the screen rooted at root that a window manager
// has left for windows: the full screen minus whatever its panels, docks and
// task bars reserved through _NET_WM_STRUT.
//
// ok is false when nothing publishes the property — a bare X server, an Xvfb,
// a session whose window manager is not EWMH-compliant — in which case the
// caller should treat the whole screen as usable, which it is.
//
// It is stated per VIRTUAL DESKTOP, not per monitor: EWMH has no per-monitor
// work area at all, and a caller with several monitors has to intersect this
// rectangle with each one. That is the best the protocol offers, and it is
// what every toolkit on X11 does.
func (c *Conn) WorkArea(root uint32) (x, y, w, h int, ok bool) {
	atom, err := c.InternAtom(netWorkAreaAtom, true)
	if err != nil || atom == AtomNone {
		return 0, 0, 0, 0, false
	}
	_, format, data, err := c.GetProperty(root, atom, AtomCardinal, false, workAreaWords)
	if err != nil || format != 32 || len(data) < 16 {
		return 0, 0, 0, 0, false
	}
	// One rectangle per virtual desktop, and the current one is the only one
	// whose reserved edges describe the screen right now. Asking which costs
	// two more round trips, so it is only asked when there is a choice.
	off := 0
	if len(data) >= 32 {
		if d := c.currentDesktop(root); d*16+16 <= len(data) {
			off = d * 16
		}
	}
	return int(int32(c.order.Uint32(data[off:]))),
		int(int32(c.order.Uint32(data[off+4:]))),
		int(c.order.Uint32(data[off+8:])),
		int(c.order.Uint32(data[off+12:])),
		true
}

// currentDesktop reads _NET_CURRENT_DESKTOP, defaulting to 0 — which is the
// right answer for a desktop that publishes a work area but no desktop index,
// and for one that has only ever had a single desktop.
func (c *Conn) currentDesktop(root uint32) int {
	atom, err := c.InternAtom(netCurrentDesktopAtom, true)
	if err != nil || atom == AtomNone {
		return 0
	}
	_, format, data, err := c.GetProperty(root, atom, AtomCardinal, false, 1)
	if err != nil || format != 32 || len(data) < 4 {
		return 0
	}
	return int(c.order.Uint32(data))
}
