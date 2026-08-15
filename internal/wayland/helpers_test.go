// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package wayland

import (
	"encoding/binary"
	"io"
	"testing"
)

// stubTransport is an in-memory transport for deterministic error-branch
// coverage: it replays canned reads/fds and can inject read/write errors,
// with no socket involved. Being socket-free it compiles on every GOOS, so
// the transport-agnostic protocol tests run on the whole build matrix; the
// SCM_RIGHTS socket harness (socketPair, fakeServer, newTestConn) is
// Linux-only and lives in helpers_linux_test.go.
type stubTransport struct {
	reads    [][]byte
	fds      []int
	readErr  error
	writeErr error
	writes   [][]byte
	// wroteFDs records the descriptors sent with each write, so a test can
	// check that a request which is meaningless without one actually carried
	// it — wl_data_offer.receive is nothing but a descriptor.
	wroteFDs [][]int
	closeErr error
	closed   bool
}

func (s *stubTransport) write(msg []byte, fds []int) error {
	if s.writeErr != nil {
		return s.writeErr
	}
	cp := append([]byte(nil), msg...)
	s.writes = append(s.writes, cp)
	s.wroteFDs = append(s.wroteFDs, append([]int(nil), fds...))
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

// decodeWrite parses a captured raw request (header + body) into its object
// id, opcode and a decoder positioned at the body.
func decodeWrite(order ByteOrder, msg []byte) (uint32, uint16, *decoder) {
	d := newDecoder(order, msg)
	obj := d.getU32()
	word := d.getU32()
	return obj, uint16(word & 0xffff), d
}

// lastWrite returns the decoded most-recent captured request on st.
func lastWrite(t *testing.T, st *stubTransport, order ByteOrder) (uint32, uint16, *decoder) {
	t.Helper()
	if len(st.writes) == 0 {
		t.Fatal("no request captured")
	}
	return decodeWrite(order, st.writes[len(st.writes)-1])
}

// decoderOf builds a decoder over the given 32-bit words, so a handler can be
// driven directly with the arguments an event would have carried.
func decoderOf(order ByteOrder, words ...uint32) *decoder {
	e := newEncoder(order)
	for _, w := range words {
		e.putU32(w)
	}
	return newDecoder(order, e.buf)
}
