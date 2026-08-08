// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// Package x11 is a from-scratch, pure-Go (CGO-free, zero non-stdlib
// dependency) implementation of the X Window System core protocol,
// version 11.0, spoken directly over a byte stream (a unix-domain
// socket in practice).
//
// It mirrors the sovereign transport+codec approach of
// github.com/go-freedesktop/dbus: no Xlib, no XCB, no cgo — the wire
// format is encoded and decoded here, byte for byte, per the X11
// protocol specification.
//
// The package is deliberately transport-agnostic. A Conn wraps any
// io.ReadWriteCloser, so the whole request/reply/error/event machine
// is exercisable in-process over a net.Pipe against a scripted fake
// server, which is how the suite reaches full coverage on every
// platform without a running X server.
package x11

import (
	"encoding/binary"
	"io"
)

// ByteOrder is the wire byte order negotiated at connection setup. X11
// lets the client pick; the server then speaks the client's order for
// the whole session.
type ByteOrder = binary.ByteOrder

// The two byte-order sentinels sent as the first byte of the setup
// request: 'l' little-endian (LSB first), 'B' big-endian (MSB first).
const (
	orderLSB = 'l'
	orderMSB = 'B'
)

// pad4 returns n rounded up to the next multiple of four. X11 pads every
// variable-length field to a four-byte boundary.
func pad4(n int) int { return (n + 3) &^ 3 }

// padding returns the number of pad bytes needed after n data bytes.
func padding(n int) int { return pad4(n) - n }

// encoder builds a request body in a chosen byte order. All multi-byte
// integers go through the negotiated ByteOrder so the same code emits a
// correct little- or big-endian stream.
type encoder struct {
	order ByteOrder
	buf   []byte
}

// newEncoder starts an encoder in the given order.
func newEncoder(order ByteOrder) *encoder { return &encoder{order: order} }

func (e *encoder) put8(v byte) { e.buf = append(e.buf, v) }

func (e *encoder) put16(v uint16) {
	var b [2]byte
	e.order.PutUint16(b[:], v)
	e.buf = append(e.buf, b[:]...)
}

func (e *encoder) put32(v uint32) {
	var b [4]byte
	e.order.PutUint32(b[:], v)
	e.buf = append(e.buf, b[:]...)
}

// putBytes appends raw bytes verbatim (no padding).
func (e *encoder) putBytes(b []byte) { e.buf = append(e.buf, b...) }

// putString appends s then pads to a four-byte boundary with zero bytes.
func (e *encoder) putString(s string) {
	e.buf = append(e.buf, s...)
	e.pad(len(s))
}

// pad appends the padding that follows n written bytes.
func (e *encoder) pad(n int) {
	for i := 0; i < padding(n); i++ {
		e.buf = append(e.buf, 0)
	}
}

// skip appends n zero bytes (for "unused" fixed fields).
func (e *encoder) skip(n int) {
	for i := 0; i < n; i++ {
		e.buf = append(e.buf, 0)
	}
}

// decoder reads a fixed-order byte slice. Every read is bounds-checked;
// once ok goes false it stays false, so a truncated buffer degrades to a
// clean error at the call site rather than a panic.
type decoder struct {
	order ByteOrder
	buf   []byte
	off   int
	ok    bool
}

// newDecoder wraps b for reading in the given order.
func newDecoder(order ByteOrder, b []byte) *decoder {
	return &decoder{order: order, buf: b, ok: true}
}

// need reports whether n more bytes are available, clearing ok if not.
func (d *decoder) need(n int) bool {
	if !d.ok || d.off+n > len(d.buf) {
		d.ok = false
		return false
	}
	return true
}

func (d *decoder) get8() byte {
	if !d.need(1) {
		return 0
	}
	v := d.buf[d.off]
	d.off++
	return v
}

func (d *decoder) get16() uint16 {
	if !d.need(2) {
		return 0
	}
	v := d.order.Uint16(d.buf[d.off:])
	d.off += 2
	return v
}

func (d *decoder) get32() uint32 {
	if !d.need(4) {
		return 0
	}
	v := d.order.Uint32(d.buf[d.off:])
	d.off += 4
	return v
}

// getBytes returns the next n bytes (a copy) and advances.
func (d *decoder) getBytes(n int) []byte {
	if !d.need(n) {
		return nil
	}
	out := make([]byte, n)
	copy(out, d.buf[d.off:d.off+n])
	d.off += n
	return out
}

// getString reads an n-byte string and skips its four-byte padding.
func (d *decoder) getString(n int) string {
	b := d.getBytes(n)
	d.skip(padding(n))
	return string(b)
}

// skip advances over n bytes, clamping at the end.
func (d *decoder) skip(n int) {
	if !d.need(n) {
		return
	}
	d.off += n
}

// orderFor maps a byte-order sentinel to its binary.ByteOrder. The bool
// reports whether the sentinel was recognised.
func orderFor(sentinel byte) (ByteOrder, bool) {
	switch sentinel {
	case orderLSB:
		return binary.LittleEndian, true
	case orderMSB:
		return binary.BigEndian, true
	}
	return nil, false
}

// readFull reads exactly len(b) bytes or returns the first error. It is a
// thin alias kept local so the codec has no direct io dependency scattered
// through it.
func readFull(r io.Reader, b []byte) error {
	_, err := io.ReadFull(r, b)
	return err
}
