// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package wayland

import (
	"encoding/binary"
	"io"
	"net"
	"os"
	"syscall"
	"testing"
)

// stubTransport is an in-memory transport for deterministic error-branch
// coverage: it replays canned reads/fds and can inject read/write errors,
// with no socket involved.
type stubTransport struct {
	reads    [][]byte
	fds      []int
	readErr  error
	writeErr error
	writes   [][]byte
	closeErr error
	closed   bool
}

func (s *stubTransport) write(msg []byte, fds []int) error {
	if s.writeErr != nil {
		return s.writeErr
	}
	cp := append([]byte(nil), msg...)
	s.writes = append(s.writes, cp)
	return nil
}

func (s *stubTransport) read() ([]byte, error) {
	if len(s.reads) > 0 {
		m := s.reads[0]
		s.reads = s.reads[1:]
		return m, nil
	}
	if s.readErr != nil {
		return nil, s.readErr
	}
	return nil, io.EOF
}

func (s *stubTransport) popFD() (int, bool) {
	if len(s.fds) == 0 {
		return 0, false
	}
	fd := s.fds[0]
	s.fds = s.fds[1:]
	return fd, true
}

func (s *stubTransport) Close() error { s.closed = true; return s.closeErr }

// socketPair returns two connected *net.UnixConn endpoints backed by an
// AF_UNIX SOCK_STREAM socketpair. It works on Linux and macOS, so the
// SCM_RIGHTS transport is exercised for real on every developer platform.
func socketPair(t *testing.T) (*net.UnixConn, *net.UnixConn) {
	t.Helper()
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatalf("socketpair: %v", err)
	}
	mk := func(fd int) *net.UnixConn {
		f := os.NewFile(uintptr(fd), "socketpair")
		c, err := net.FileConn(f)
		_ = f.Close()
		if err != nil {
			t.Fatalf("FileConn: %v", err)
		}
		uc, ok := c.(*net.UnixConn)
		if !ok {
			t.Fatalf("FileConn returned %T, want *net.UnixConn", c)
		}
		return uc
	}
	return mk(fds[0]), mk(fds[1])
}

// fakeServer is the compositor side of a connection in tests: it reads
// client requests and sends events using the very same sovereign wire
// codec, so a scripted compositor drives the client end faithfully.
type fakeServer struct {
	tr    *unixTransport
	order ByteOrder
}

// newFakeServer wraps the compositor end of a socket pair.
func newFakeServer(c *net.UnixConn, order ByteOrder) *fakeServer {
	return &fakeServer{tr: newUnixTransport(c, order), order: order}
}

// readReq reads one client request, returning its object id, opcode and a
// decoder positioned at the argument body. It returns errors (rather than
// calling t.Fatal) so it is safe to use from a compositor goroutine.
func (s *fakeServer) readReq() (uint32, uint16, *decoder, error) {
	msg, err := s.tr.read()
	if err != nil {
		return 0, 0, nil, err
	}
	d := newDecoder(s.order, msg)
	obj := d.getU32()
	word := d.getU32()
	return obj, uint16(word & 0xffff), d, nil
}

// sendEvt sends one event to the client, carrying fds out-of-band.
func (s *fakeServer) sendEvt(obj uint32, opcode uint16, body []byte, fds ...int) error {
	total := 8 + len(body)
	e := newEncoder(s.order)
	e.putU32(obj)
	e.putU32(uint32(opcode) | uint32(total)<<16)
	e.putBytes(body)
	return s.tr.write(e.buf, fds)
}

// popFD drains a descriptor the server received from the client.
func (s *fakeServer) popFD() (int, bool) { return s.tr.popFD() }

// bodyOf builds an argument body in the given order via fn.
func bodyOf(order ByteOrder, fn func(e *encoder)) []byte {
	e := newEncoder(order)
	fn(e)
	return e.buf
}

// bothOrders runs fn for the little- and big-endian codec paths so every
// test covers both regardless of the host's native order.
func bothOrders(t *testing.T, fn func(t *testing.T, order ByteOrder)) {
	t.Helper()
	for _, order := range []ByteOrder{binary.LittleEndian, binary.BigEndian} {
		order := order
		t.Run(orderName(order), func(t *testing.T) { fn(t, order) })
	}
}

// newTestConn wires a client Conn to a fake server over a socket pair.
func newTestConn(t *testing.T, order ByteOrder) (*Conn, *fakeServer) {
	t.Helper()
	cli, srv := socketPair(t)
	conn := NewConn(newUnixTransport(cli, order), order)
	t.Cleanup(func() { _ = conn.Close() })
	fs := newFakeServer(srv, order)
	t.Cleanup(func() { _ = srv.Close() })
	return conn, fs
}
