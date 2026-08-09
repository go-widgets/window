// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package x11

import (
	"io"
	"net"
	"syscall"
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

// SendFD writes msg with fd attached as a single SCM_RIGHTS control message.
func (u *unixRW) SendFD(msg []byte, fd int) error {
	oob := syscall.UnixRights(fd)
	_, _, err := u.c.WriteMsgUnix(msg, oob, nil)
	return err
}
