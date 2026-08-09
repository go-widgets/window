// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package wayland

import (
	"encoding/binary"
	"os"
	"syscall"
	"testing"
	"unsafe"
)

// frame builds a raw Wayland message (header + body) in the given order.
func frame(order ByteOrder, obj uint32, opcode uint16, body []byte) []byte {
	total := 8 + len(body)
	e := newEncoder(order)
	e.putU32(obj)
	e.putU32(uint32(opcode) | uint32(total)<<16)
	e.putBytes(body)
	return e.buf
}

func TestUnixTransportRoundTrip(t *testing.T) {
	bothOrders(t, func(t *testing.T, order ByteOrder) {
		a, b := socketPair(t)
		ta := newUnixTransport(a, order)
		tb := newUnixTransport(b, order)
		defer ta.Close()
		defer tb.Close()

		msg := frame(order, 3, 5, bodyOf(order, func(e *encoder) { e.putU32(42) }))
		if err := ta.write(msg, nil); err != nil {
			t.Fatalf("write: %v", err)
		}
		got, err := tb.read()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if len(got) != len(msg) {
			t.Fatalf("read %d bytes, want %d", len(got), len(msg))
		}
		d := newDecoder(order, got)
		if d.getU32() != 3 {
			t.Error("object id mismatch")
		}
	})
}

func TestUnixTransportFDPassing(t *testing.T) {
	a, b := socketPair(t)
	order := binary.LittleEndian
	ta := newUnixTransport(a, order)
	tb := newUnixTransport(b, order)
	defer ta.Close()
	defer tb.Close()

	// Send a real descriptor (the read end of a pipe) as SCM_RIGHTS, then
	// prove the received fd refers to the same open file by writing to the
	// pipe's write end and reading it back through the passed fd.
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()

	msg := frame(order, 7, 1, bodyOf(order, func(e *encoder) { e.putU32(1) }))
	if err := ta.write(msg, []int{int(r.Fd())}); err != nil {
		t.Fatalf("write with fd: %v", err)
	}
	if _, err := tb.read(); err != nil {
		t.Fatalf("read: %v", err)
	}
	fd, ok := tb.popFD()
	if !ok {
		t.Fatal("no fd received")
	}
	defer syscall.Close(fd)
	// A second pop yields nothing.
	if _, ok := tb.popFD(); ok {
		t.Error("popFD should be empty after draining")
	}

	const payload = "wayland-scm-rights"
	if _, err := w.WriteString(payload); err != nil {
		t.Fatalf("pipe write: %v", err)
	}
	buf := make([]byte, len(payload))
	n, err := syscall.Read(fd, buf)
	if err != nil {
		t.Fatalf("read passed fd: %v", err)
	}
	if string(buf[:n]) != payload {
		t.Fatalf("passed fd read %q, want %q", buf[:n], payload)
	}
}

func TestUnixTransportReassembly(t *testing.T) {
	// Two messages written back-to-back, delivered as one stream, must be
	// split into two by the reassembler.
	a, b := socketPair(t)
	order := binary.LittleEndian
	ta := newUnixTransport(a, order)
	tb := newUnixTransport(b, order)
	defer ta.Close()
	defer tb.Close()

	m1 := frame(order, 1, 0, bodyOf(order, func(e *encoder) { e.putU32(11) }))
	m2 := frame(order, 2, 0, bodyOf(order, func(e *encoder) { e.putU32(22) }))
	combined := append(append([]byte{}, m1...), m2...)
	if err := ta.write(combined, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	for _, want := range []uint32{1, 2} {
		got, err := tb.read()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if id := newDecoder(order, got).getU32(); id != want {
			t.Errorf("message id = %d, want %d", id, want)
		}
	}
}

func TestUnixTransportReadError(t *testing.T) {
	a, b := socketPair(t)
	order := binary.LittleEndian
	ta := newUnixTransport(a, order)
	tb := newUnixTransport(b, order)
	// Closing the writer makes the reader hit EOF.
	_ = ta.Close()
	if _, err := tb.read(); err == nil {
		t.Error("read after peer close should error")
	}
	_ = tb.Close()
}

func TestUnixTransportWriteError(t *testing.T) {
	a, b := socketPair(t)
	order := binary.LittleEndian
	ta := newUnixTransport(a, order)
	_ = b.Close()
	_ = a.Close()
	// Writing on a closed socket errors.
	if err := ta.write(frame(order, 1, 0, nil), nil); err == nil {
		t.Error("write on closed socket should error")
	}
}

func TestTakeMessageInvalidSize(t *testing.T) {
	order := binary.LittleEndian
	tr := &unixTransport{order: order}
	// A header claiming size 4 (< the 8-byte minimum) is a protocol error.
	e := newEncoder(order)
	e.putU32(1)
	e.putU32(uint32(0) | uint32(4)<<16)
	tr.rbuf = e.buf
	if _, _, err := tr.takeMessage(); err == nil {
		t.Error("takeMessage should reject size < 8")
	}
}

func TestTakeMessagePartialHeader(t *testing.T) {
	tr := &unixTransport{order: binary.LittleEndian, rbuf: []byte{1, 2, 3}}
	if _, ok, err := tr.takeMessage(); ok || err != nil {
		t.Errorf("partial header: ok=%v err=%v, want false,nil", ok, err)
	}
}

func TestTakeMessagePartialBody(t *testing.T) {
	order := binary.LittleEndian
	full := frame(order, 1, 0, bodyOf(order, func(e *encoder) { e.putU32(1) }))
	tr := &unixTransport{order: order, rbuf: full[:len(full)-2]} // drop tail
	if _, ok, err := tr.takeMessage(); ok || err != nil {
		t.Errorf("partial body: ok=%v err=%v, want false,nil", ok, err)
	}
}

func TestReadPropagatesInvalidSize(t *testing.T) {
	// read() must surface takeMessage's protocol error.
	a, b := socketPair(t)
	order := binary.LittleEndian
	defer a.Close()
	defer b.Close()
	ta := newUnixTransport(a, order)
	tb := newUnixTransport(b, order)
	bad := frame(order, 1, 0, nil)
	binary.LittleEndian.PutUint32(bad[4:8], uint32(0)|uint32(4)<<16) // size=4
	if err := ta.write(bad, nil); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := tb.read(); err == nil {
		t.Error("read should surface invalid size")
	}
}

func TestPopFDEmpty(t *testing.T) {
	tr := &unixTransport{}
	if _, ok := tr.popFD(); ok {
		t.Error("popFD on empty queue should be false")
	}
}

func TestParseControlFDsOversizedLen(t *testing.T) {
	// A cmsghdr claiming more data than the buffer holds makes
	// ParseSocketControlMessage fail.
	oob := make([]byte, syscall.CmsgSpace(4))
	h := (*syscall.Cmsghdr)(unsafe.Pointer(&oob[0]))
	h.Level = syscall.SOL_SOCKET
	h.Type = 1
	h.SetLen(syscall.CmsgLen(4096)) // far beyond len(oob)
	if _, err := defaultParseControlFDs(oob); err == nil {
		t.Error("oversized cmsg len should be rejected")
	}
}

func TestParseControlFDsWrongType(t *testing.T) {
	// A well-formed control message whose type is NOT SCM_RIGHTS makes
	// ParseUnixRights fail.
	oob := make([]byte, syscall.CmsgSpace(4))
	h := (*syscall.Cmsghdr)(unsafe.Pointer(&oob[0]))
	h.Level = syscall.SOL_SOCKET
	h.Type = 0 // not SCM_RIGHTS
	h.SetLen(syscall.CmsgLen(4))
	if _, err := defaultParseControlFDs(oob); err == nil {
		t.Error("non-SCM_RIGHTS control message should be rejected")
	}
}

func TestFillControlParseError(t *testing.T) {
	// When ancillary data arrives but the parser rejects it, fill (and thus
	// read) must surface the error. Force it via the injectable parser.
	a, b := socketPair(t)
	order := binary.LittleEndian
	ta := newUnixTransport(a, order)
	tb := newUnixTransport(b, order)
	defer ta.Close()
	defer tb.Close()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()
	if err := ta.write(frame(order, 1, 0, nil), []int{int(r.Fd())}); err != nil {
		t.Fatalf("write: %v", err)
	}

	orig := parseControlFDs
	parseControlFDs = func([]byte) ([]int, error) { return nil, errForcedParse }
	defer func() { parseControlFDs = orig }()
	if err := tb.fill(); err == nil {
		t.Error("fill should surface a control-parse error")
	}
}

var errForcedParse = errorsNew("forced parse failure")

// errorsNew is a tiny local errors.New to avoid an extra import here.
func errorsNew(s string) error { return &simpleErr{s} }

type simpleErr struct{ s string }

func (e *simpleErr) Error() string { return e.s }

func TestCloseDrainsFDs(t *testing.T) {
	a, b := socketPair(t)
	order := binary.LittleEndian
	ta := newUnixTransport(a, order)
	tb := newUnixTransport(b, order)
	defer ta.Close()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	defer r.Close()
	defer w.Close()
	if err := ta.write(frame(order, 1, 0, nil), []int{int(r.Fd())}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := tb.read(); err != nil {
		t.Fatalf("read: %v", err)
	}
	// Close must close the still-queued received fd and the socket.
	if err := tb.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if tb.fds != nil {
		t.Error("Close should clear the fd queue")
	}
}
