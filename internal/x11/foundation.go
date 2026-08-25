// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package x11

import (
	"io"
	"net"

	xproto "github.com/go-freedesktop/x11"
)

// The wire codec, the Xauthority parser, the connection-setup exchange, the
// shared-memory segment and the SCM_RIGHTS unix transport used to live in this
// package. They now live in github.com/go-freedesktop/x11, which
// github.com/go-freedesktop/screencast shares: they are the half of an X11
// client that has nothing to do with what the client is FOR, and two copies of
// a wire protocol parser drift silently until something fails on one back-end
// only.
//
// What stays here is the half that IS about a window: creating and mapping
// one, the request/reply/event demultiplexer that feeds the host loop,
// keysym and keyboard mapping, selections and the clipboard, MIT-SHM in the
// PutImage direction, and the RGBA→ZPixmap Presenter.
//
// These aliases are names, not copies — x11.Setup and xproto.Setup are the same
// type — so the back-end above reads as it always did.
type (
	// ByteOrder is the wire byte order negotiated at connection setup.
	ByteOrder = xproto.ByteOrder
	// Setup is the parsed server connection-setup reply.
	Setup = xproto.Setup
	// Screen is one root screen of a Setup.
	Screen = xproto.Screen
	// Depth groups the visuals available at a given colour depth.
	Depth = xproto.Depth
	// Format is one entry of the server's pixmap-format list.
	Format = xproto.Format
	// VisualType describes a visual and its RGB channel masks.
	VisualType = xproto.VisualType
	// FDSender is a transport that can pass a descriptor over SCM_RIGHTS.
	FDSender = xproto.FDSender
)

// Visual classes and image byte orders, re-exported so a caller needs only
// this package to read a Setup it got from this package.
const (
	VisualStaticGray  = xproto.VisualStaticGray
	VisualGrayScale   = xproto.VisualGrayScale
	VisualStaticColor = xproto.VisualStaticColor
	VisualPseudoColor = xproto.VisualPseudoColor
	VisualTrueColor   = xproto.VisualTrueColor
	VisualDirectColor = xproto.VisualDirectColor

	imageOrderLSB = xproto.ImageOrderLSB
	imageOrderMSB = xproto.ImageOrderMSB
)

// WrapUnix wraps a dialed *net.UnixConn as an fd-passing transport, which is
// what lets this back-end hand the server a shared-memory descriptor with
// MIT-SHM AttachFd. It is [xproto.WrapUnix].
func WrapUnix(c *net.UnixConn) io.ReadWriteCloser { return xproto.WrapUnix(c) }

// Handshake runs the connection setup over rw and returns a ready Conn.
//
// The setup exchange itself is [xproto.Handshake]; what this adds is the Conn
// around it — the sequence counter, the resource-id allocator and the queue
// that holds events arriving while a synchronous reply is outstanding. order
// selects the wire byte order; both are valid and the server adopts the
// client's choice.
func Handshake(rw io.ReadWriteCloser, order ByteOrder, authName string, authData []byte) (*Conn, error) {
	s, err := xproto.Handshake(rw, order, authName, authData)
	if err != nil {
		return nil, err
	}
	return &Conn{
		rw:      rw,
		order:   order,
		setup:   s,
		xidBase: s.ResourceIDBase,
		xidMask: s.ResourceIDMask,
	}, nil
}
