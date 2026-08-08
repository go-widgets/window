// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package x11

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
)

// fakeConn is an in-memory io.ReadWriteCloser: reads drain a preloaded
// server→client script, writes accumulate for inspection. It lets the
// whole request/reply/event machine be tested with no real X server.
type fakeConn struct {
	in       *bytes.Reader // server -> client
	out      bytes.Buffer  // client -> server (captured)
	closed   bool
	writeErr error // when set, every Write fails with it
	readErr  error // when set, every Read fails with it (after in drains)
}

func newFakeConn(serverBytes []byte) *fakeConn {
	return &fakeConn{in: bytes.NewReader(serverBytes)}
}

func (f *fakeConn) Read(p []byte) (int, error) {
	if f.in.Len() == 0 && f.readErr != nil {
		return 0, f.readErr
	}
	return f.in.Read(p)
}

func (f *fakeConn) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return f.out.Write(p)
}

func (f *fakeConn) Close() error { f.closed = true; return nil }

var errInjected = errors.New("injected")

const (
	testVisualID = 0x21
	testRootWin  = 0x123
)

// setupReplyBody builds a well-formed success setup reply body (the
// additional-data block after the 8-byte header) in the given order: one
// screen, one depth-24 TrueColor visual, one depth-24/32bpp format.
func setupReplyBody(order ByteOrder, imageOrder uint8) []byte {
	e := newEncoder(order)
	e.put32(1)          // release
	e.put32(0x04000000) // resource-id-base
	e.put32(0x001fffff) // resource-id-mask
	e.put32(256)        // motion-buffer-size
	vendor := "Test"
	e.put16(uint16(len(vendor))) // vendor length
	e.put16(0xffff)              // maximum-request-length
	e.put8(1)                    // number of screens
	e.put8(1)                    // number of formats
	e.put8(imageOrder)           // image-byte-order
	e.put8(0)                    // bitmap-format-bit-order
	e.put8(32)                   // bitmap-format-scanline-unit
	e.put8(32)                   // bitmap-format-scanline-pad
	e.put8(8)                    // min-keycode
	e.put8(9)                    // max-keycode
	e.skip(4)                    // unused
	e.putString(vendor)          // vendor + pad

	// One FORMAT: depth 24, 32 bpp, 32-bit scanline pad.
	e.put8(24)
	e.put8(32)
	e.put8(32)
	e.skip(5)

	// One SCREEN.
	e.put32(testRootWin)  // root
	e.put32(0x22)         // default-colormap
	e.put32(0x00ffffff)   // white-pixel
	e.put32(0)            // black-pixel
	e.put32(0)            // current-input-masks
	e.put16(800)          // width
	e.put16(600)          // height
	e.put16(300)          // width-mm
	e.put16(200)          // height-mm
	e.put16(1)            // min-installed-maps
	e.put16(1)            // max-installed-maps
	e.put32(testVisualID) // root-visual
	e.put8(0)             // backing-stores
	e.put8(0)             // save-unders
	e.put8(24)            // root-depth
	e.put8(1)             // number of depths

	// One DEPTH with one VISUALTYPE.
	e.put8(24) // depth
	e.skip(1)
	e.put16(1) // number of visuals
	e.skip(4)
	e.put32(testVisualID)   // visual-id
	e.put8(VisualTrueColor) // class
	e.put8(8)               // bits-per-rgb
	e.put16(256)            // colormap-entries
	e.put32(0x00ff0000)     // red-mask
	e.put32(0x0000ff00)     // green-mask
	e.put32(0x000000ff)     // blue-mask
	e.skip(4)

	return e.buf
}

// setupSuccess frames a full success setup reply (8-byte header + body).
func setupSuccess(order ByteOrder, imageOrder uint8) []byte {
	body := setupReplyBody(order, imageOrder)
	hdr := make([]byte, 8)
	hdr[0] = 1 // Success
	order.PutUint16(hdr[2:4], 11)
	order.PutUint16(hdr[4:6], 0)
	order.PutUint16(hdr[6:8], uint16(len(body)/4))
	return append(hdr, body...)
}

// dialFakeConn returns a Conn already handshaken against a scripted server
// whose extra bytes (replies/events) follow the setup reply.
func dialFakeConn(t testingTB, order ByteOrder, extra []byte) (*Conn, *fakeConn) {
	server := append(setupSuccess(order, imageOrderLSB), extra...)
	fc := newFakeConn(server)
	c, err := Handshake(fc, order, "", nil)
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	return c, fc
}

// testingTB is the subset of *testing.T the helpers use.
type testingTB interface {
	Fatalf(format string, args ...any)
}

// replyPacket frames a 32-byte reply with the given byte-1 value and
// optional additional data (whose length in 4-byte units goes in the
// length field). tail fills the fixed 24-byte reply body region.
func replyPacket(order ByteOrder, b1 byte, seq uint16, tail [24]byte, additional []byte) []byte {
	pkt := make([]byte, 32+len(additional))
	pkt[0] = pktReply
	pkt[1] = b1
	order.PutUint16(pkt[2:4], seq)
	order.PutUint32(pkt[4:8], uint32(len(additional)/4))
	copy(pkt[8:32], tail[:])
	copy(pkt[32:], additional)
	return pkt
}

// errorPacket frames a 32-byte error packet.
func errorPacket(order ByteOrder, code byte, seq uint16, bad uint32, major byte, minor uint16) []byte {
	pkt := make([]byte, 32)
	pkt[0] = pktError
	pkt[1] = code
	order.PutUint16(pkt[2:4], seq)
	order.PutUint32(pkt[4:8], bad)
	order.PutUint16(pkt[8:10], minor)
	pkt[10] = major
	return pkt
}

// assert io.ReadWriteCloser is satisfied at compile time.
var _ io.ReadWriteCloser = (*fakeConn)(nil)
var _ = binary.LittleEndian
