// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package x11

import (
	"io"
	"net"
	"syscall"
	"time"
)

// unixRW adapts a *net.UnixConn to the Conn transport, adding fd passing over
// SCM_RIGHTS. It is what lets the production connection attach a shared-memory
// descriptor to a MIT-SHM AttachFd request while every other request still
// travels as an ordinary socket write. It is Linux-only: fd passing over
// SCM_RIGHTS is how the X11 backend hands the server a shared-memory segment,
// and the backend itself only runs on Linux.
type unixRW struct {
	c *net.UnixConn
	// peek holds the byte WaitReadable took off the socket to prove something
	// had arrived. Read hands it back before touching the socket again, so the
	// packet it belongs to is never seen short.
	peek []byte
}

// WrapUnix wraps a dialed *net.UnixConn as an fd-passing transport for
// Handshake. A connection built over it reports SupportsFDPassing() == true.
func WrapUnix(c *net.UnixConn) io.ReadWriteCloser { return &unixRW{c: c} }

func (u *unixRW) Read(b []byte) (int, error) {
	if len(u.peek) > 0 {
		n := copy(b, u.peek)
		u.peek = u.peek[n:]
		return n, nil
	}
	return u.c.Read(b)
}
func (u *unixRW) Write(b []byte) (int, error) { return u.c.Write(b) }
func (u *unixRW) Close() error                { return u.c.Close() }

// WaitReadable reports whether the server sent anything within d.
//
// It reads ONE byte and keeps it, rather than asking the kernel whether the
// socket is readable. That is the difference between a wait and a broken
// connection: a read deadline that expires between a packet's header and its
// body leaves the protocol stream desynchronised, whereas a byte taken off the
// front and handed back by the next Read cannot cut a packet in half.
//
// It exists because the X11 selection protocol has no timeout of its own. A
// paste asks whoever owns the clipboard and waits for an event that arrives
// only if that owner is alive and still answering; one that died between
// claiming and being asked would otherwise block the window for ever.
func (u *unixRW) WaitReadable(d time.Duration) bool {
	if len(u.peek) > 0 {
		return true
	}
	if err := u.c.SetReadDeadline(time.Now().Add(d)); err != nil {
		return false // a closed connection is not going to become readable
	}
	defer func() { _ = u.c.SetReadDeadline(time.Time{}) }()
	var one [1]byte
	n, err := u.c.Read(one[:])
	if n > 0 {
		u.peek = append(u.peek, one[0])
	}
	return err == nil && n > 0
}

// SendFD writes msg with fd attached as a single SCM_RIGHTS control message.
func (u *unixRW) SendFD(msg []byte, fd int) error {
	oob := syscall.UnixRights(fd)
	_, _, err := u.c.WriteMsgUnix(msg, oob, nil)
	return err
}
