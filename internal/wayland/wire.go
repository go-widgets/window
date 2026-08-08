// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// Package wayland is a from-scratch, pure-Go (CGO-free, zero non-stdlib
// dependency) implementation of the Wayland wire protocol, spoken directly
// over a UNIX-domain stream socket.
//
// It mirrors the sovereign transport+codec approach of the sibling
// internal/x11 package and of github.com/go-freedesktop/dbus: no
// libwayland, no wayland-scanner, no cgo — the wire format is encoded and
// decoded here, byte for byte, per the Wayland protocol specification, and
// file descriptors are passed over the socket via SCM_RIGHTS ancillary
// control messages using only the Go standard library.
//
// The Wayland wire format is object-oriented. Every message is
//
//	uint32 object-id      (the target/sender object)
//	uint32 (size<<16 | opcode)   size in bytes incl. this 8-byte header
//	... typed arguments, each padded to a 4-byte boundary ...
//
// Arguments are int (i32), uint (u32), fixed (signed 24.8), string
// (length-prefixed, NUL-terminated, padded), array (length-prefixed,
// padded), object (u32 id), new_id (u32 id, optionally interface+version
// prefixed) and fd (carried out-of-band, occupying no bytes in the body).
//
// Integers travel in the host's native byte order (both peers share the
// machine), so the codec is parametrised by a binary.ByteOrder that
// defaults to binary.NativeEndian; tests drive both endian paths on any
// host, and the s390x CI lane exercises the big-endian path on real
// big-endian hardware.
package wayland

import (
	"encoding/binary"
	"math"
)

// ByteOrder is the wire byte order. Wayland uses the machine's native
// order; NativeOrder resolves it, and the codec is parametrised so both
// paths are testable on any host.
type ByteOrder = binary.ByteOrder

// NativeOrder is the byte order used on the wire in production: the host's
// native endianness, which both the client and the compositor share.
var NativeOrder ByteOrder = binary.NativeEndian

// pad4 rounds n up to the next multiple of four; Wayland pads every
// variable-length argument to a 32-bit boundary.
func pad4(n int) int { return (n + 3) &^ 3 }

// padding is the number of pad bytes following n data bytes.
func padding(n int) int { return pad4(n) - n }

// Fixed is a Wayland 24.8 signed fixed-point number as carried on the wire
// (the raw i32 value equal to the real number times 256).
type Fixed int32

// FixedFromInt builds a Fixed from a whole integer.
func FixedFromInt(i int) Fixed { return Fixed(i << 8) }

// FixedFromFloat builds a Fixed from a float64 (rounded to 1/256).
func FixedFromFloat(f float64) Fixed { return Fixed(math.Round(f * 256.0)) }

// Int returns the truncated integer part (toward zero via arithmetic shift
// for the fractional bits; matches wl_fixed_to_int).
func (f Fixed) Int() int { return int(int32(f) >> 8) }

// Float returns the value as a float64.
func (f Fixed) Float() float64 { return float64(int32(f)) / 256.0 }

// encoder builds a message body in a chosen byte order. Every multi-byte
// integer goes through the ByteOrder so the same code emits a correct
// little- or big-endian stream.
type encoder struct {
	order ByteOrder
	buf   []byte
}

// newEncoder starts an encoder in the given order.
func newEncoder(order ByteOrder) *encoder { return &encoder{order: order} }

func (e *encoder) putU32(v uint32) {
	var b [4]byte
	e.order.PutUint32(b[:], v)
	e.buf = append(e.buf, b[:]...)
}

// putI32 writes a signed 32-bit integer (two's complement).
func (e *encoder) putI32(v int32) { e.putU32(uint32(v)) }

// putFixed writes a 24.8 fixed-point number.
func (e *encoder) putFixed(f Fixed) { e.putI32(int32(f)) }

// putString writes a length-prefixed, NUL-terminated, 4-byte-padded
// string. The length prefix counts the trailing NUL. An empty string is
// encoded as length 1 with a single NUL, matching libwayland; a nil
// (absent) string is written by putNullString.
func (e *encoder) putString(s string) {
	n := len(s) + 1 // include the trailing NUL
	e.putU32(uint32(n))
	e.buf = append(e.buf, s...)
	e.buf = append(e.buf, 0) // NUL
	e.pad(n)
}

// putArray writes a length-prefixed, 4-byte-padded raw byte array.
func (e *encoder) putArray(b []byte) {
	e.putU32(uint32(len(b)))
	e.buf = append(e.buf, b...)
	e.pad(len(b))
}

// putBytes appends raw bytes verbatim (no length prefix, no padding).
func (e *encoder) putBytes(b []byte) { e.buf = append(e.buf, b...) }

// pad appends the padding that follows n written bytes.
func (e *encoder) pad(n int) {
	for i := 0; i < padding(n); i++ {
		e.buf = append(e.buf, 0)
	}
}

// decoder reads a fixed-order message body. Every read is bounds-checked;
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
	if !d.ok || n < 0 || d.off+n > len(d.buf) {
		d.ok = false
		return false
	}
	return true
}

func (d *decoder) getU32() uint32 {
	if !d.need(4) {
		return 0
	}
	v := d.order.Uint32(d.buf[d.off:])
	d.off += 4
	return v
}

// getI32 reads a signed 32-bit integer.
func (d *decoder) getI32() int32 { return int32(d.getU32()) }

// getFixed reads a 24.8 fixed-point number.
func (d *decoder) getFixed() Fixed { return Fixed(d.getI32()) }

// getString reads a length-prefixed, NUL-terminated, padded string. The
// returned value excludes the trailing NUL. A zero length yields "".
func (d *decoder) getString() string {
	n := int(d.getU32())
	if n == 0 {
		return ""
	}
	if !d.need(pad4(n)) {
		return ""
	}
	// n includes the trailing NUL; the text is the first n-1 bytes.
	s := string(d.buf[d.off : d.off+n-1])
	d.off += pad4(n)
	return s
}

// getArray reads a length-prefixed, padded raw byte array (a copy).
func (d *decoder) getArray() []byte {
	n := int(d.getU32())
	if n == 0 {
		return []byte{}
	}
	if !d.need(pad4(n)) {
		return nil
	}
	out := make([]byte, n)
	copy(out, d.buf[d.off:d.off+n])
	d.off += pad4(n)
	return out
}

// orderName returns a human label for an order (used in diagnostics/tests).
func orderName(o ByteOrder) string {
	if o == binary.BigEndian {
		return "big"
	}
	return "little"
}
