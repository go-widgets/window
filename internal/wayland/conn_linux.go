// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package wayland

import "net"

// New builds a connection over a dialed UNIX-domain socket using the host's
// native wire byte order — the production entry point for the window layer.
// It is Linux-only: it wires the SCM_RIGHTS socket transport, which is the
// only way to pass wl_shm/keymap descriptors to the compositor, and the
// backend itself only runs on Linux. The transport-agnostic NewConn (used by
// the in-process tests) stays cross-platform in conn.go.
func New(c *net.UnixConn) *Conn {
	return NewConn(newUnixTransport(c, NativeOrder), NativeOrder)
}
