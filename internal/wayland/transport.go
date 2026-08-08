// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package wayland

import (
	"fmt"
	"net"
	"syscall"
)

// transport moves whole Wayland messages (already framed byte blobs) plus
// the file descriptors they carry. It is the seam that lets the protocol
// machine run either over a real UNIX socket (fds via SCM_RIGHTS) or over
// an in-process fake compositor in tests.
type transport interface {
	// write sends one framed message and, out-of-band, its fds.
	write(msg []byte, fds []int) error
	// read returns exactly one framed message. File descriptors that
	// arrive with it (or earlier) are queued and drained via popFD.
	read() ([]byte, error)
	// popFD returns the next received file descriptor, oldest first.
	popFD() (int, bool)
	// Close releases the transport.
	Close() error
}

// oobSize is the ancillary-data buffer size: room for many SCM_RIGHTS fds
// per recvmsg (Wayland passes one or two, but a burst may batch several).
var oobSize = syscall.CmsgSpace(4 * 253)

// dataChunk is the per-recvmsg data buffer. libwayland caps a single
// message at 4096 bytes; reassembly stitches messages that span reads.
const dataChunk = 4096

// unixTransport is the production transport over a connected UNIX-domain
// stream socket. It reassembles the byte stream into whole messages and
// collects passed file descriptors from SCM_RIGHTS control messages.
type unixTransport struct {
	c     *net.UnixConn
	order ByteOrder

	rbuf []byte // buffered, not-yet-consumed stream bytes
	fds  []int  // received file descriptors, oldest first
}

// newUnixTransport wraps a connected *net.UnixConn.
func newUnixTransport(c *net.UnixConn, order ByteOrder) *unixTransport {
	return &unixTransport{c: c, order: order}
}

// write sends msg in a single sendmsg, attaching fds as SCM_RIGHTS. A
// blocking UNIX stream socket writes the whole datagram or fails, so a
// successful call has delivered every byte and every descriptor.
func (t *unixTransport) write(msg []byte, fds []int) error {
	var oob []byte
	if len(fds) > 0 {
		oob = syscall.UnixRights(fds...)
	}
	_, _, err := t.c.WriteMsgUnix(msg, oob, nil)
	return err
}

// read returns the next complete message, pulling more bytes (and fds)
// from the socket as needed.
func (t *unixTransport) read() ([]byte, error) {
	for {
		if msg, ok, err := t.takeMessage(); err != nil {
			return nil, err
		} else if ok {
			return msg, nil
		}
		if err := t.fill(); err != nil {
			return nil, err
		}
	}
}

// takeMessage slices one complete message off the front of rbuf, reporting
// whether a whole message was available. A size field below the 8-byte
// header minimum is a fatal protocol error.
func (t *unixTransport) takeMessage() ([]byte, bool, error) {
	if len(t.rbuf) < 8 {
		return nil, false, nil
	}
	size := int(t.order.Uint32(t.rbuf[4:8]) >> 16)
	if size < 8 {
		return nil, false, fmt.Errorf("wayland: invalid message size %d", size)
	}
	if len(t.rbuf) < size {
		return nil, false, nil
	}
	msg := make([]byte, size)
	copy(msg, t.rbuf[:size])
	t.rbuf = t.rbuf[size:]
	return msg, true, nil
}

// fill reads one recvmsg worth of data and control bytes into the buffers.
func (t *unixTransport) fill() error {
	data := make([]byte, dataChunk)
	oob := make([]byte, oobSize)
	n, oobn, _, _, err := t.c.ReadMsgUnix(data, oob)
	if err != nil {
		return err
	}
	t.rbuf = append(t.rbuf, data[:n]...)
	if oobn > 0 {
		fds, err := parseControlFDs(oob[:oobn])
		if err != nil {
			return err
		}
		t.fds = append(t.fds, fds...)
	}
	return nil
}

// parseControlFDs extracts SCM_RIGHTS file descriptors from a block of
// ancillary control data. It is a package variable so a test can force the
// (otherwise kernel-guaranteed-valid) parse to fail on the fill path.
var parseControlFDs = defaultParseControlFDs

// defaultParseControlFDs is the real SCM_RIGHTS parser.
func defaultParseControlFDs(oob []byte) ([]int, error) {
	scms, err := syscall.ParseSocketControlMessage(oob)
	if err != nil {
		return nil, err
	}
	var out []int
	for i := range scms {
		fds, err := syscall.ParseUnixRights(&scms[i])
		if err != nil {
			return nil, err
		}
		out = append(out, fds...)
	}
	return out, nil
}

// popFD returns the oldest received file descriptor.
func (t *unixTransport) popFD() (int, bool) {
	if len(t.fds) == 0 {
		return 0, false
	}
	fd := t.fds[0]
	t.fds = t.fds[1:]
	return fd, true
}

// Close closes the socket and any still-queued received descriptors so no
// fd leaks when a session ends mid-stream.
func (t *unixTransport) Close() error {
	for _, fd := range t.fds {
		_ = syscall.Close(fd)
	}
	t.fds = nil
	return t.c.Close()
}
