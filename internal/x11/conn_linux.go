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
type unixRW struct{ c *net.UnixConn }

// WrapUnix wraps a dialed *net.UnixConn as an fd-passing transport for
// Handshake. A connection built over it reports SupportsFDPassing() == true.
func WrapUnix(c *net.UnixConn) io.ReadWriteCloser { return &unixRW{c: c} }

func (u *unixRW) Read(b []byte) (int, error)  { return u.c.Read(b) }
func (u *unixRW) Write(b []byte) (int, error) { return u.c.Write(b) }
func (u *unixRW) Close() error                { return u.c.Close() }

// WaitReadable blocks until the socket has something to read or d elapses.
//
// It polls rather than setting a read deadline because a deadline can expire
// between a packet's header and its body, leaving the protocol stream
// desynchronised; readability is a question about the socket, not an
// interruption of a read in progress.
//
// syscall.Select rather than a dependency on golang.org/x/sys: this package
// implements the X11 protocol from scratch precisely so that it needs nothing,
// and one timeout is not a reason to give that up.
func (u *unixRW) WaitReadable(d time.Duration) bool {
	raw, err := u.c.SyscallConn()
	if err != nil {
		return false
	}
	deadline := time.Now().Add(d)
	for {
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return false
		}
		var ready bool
		var serr error
		if cerr := raw.Control(func(fd uintptr) {
			var set syscall.FdSet
			set.Bits[fd/64] |= 1 << (fd % 64)
			tv := syscall.NsecToTimeval(int64(remaining))
			n, e := syscall.Select(int(fd)+1, &set, nil, nil, &tv)
			ready, serr = n > 0, e
		}); cerr != nil {
			return false
		}
		if ready {
			return true
		}
		// A signal interrupts the wait; ask again with whatever time is left
		// rather than reporting a timeout that did not happen.
		if serr != nil && serr != syscall.EINTR {
			return false
		}
	}
}

// SendFD writes msg with fd attached as a single SCM_RIGHTS control message.
func (u *unixRW) SendFD(msg []byte, fd int) error {
	oob := syscall.UnixRights(fd)
	_, _, err := u.c.WriteMsgUnix(msg, oob, nil)
	return err
}
