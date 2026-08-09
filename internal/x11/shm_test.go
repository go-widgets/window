// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package x11

import (
	"encoding/binary"
	"net"
	"os"
	"syscall"
	"testing"
)

// x11SocketPair returns two connected *net.UnixConn endpoints.
func x11SocketPair(t *testing.T) (*net.UnixConn, *net.UnixConn) {
	t.Helper()
	fds, err := syscall.Socketpair(syscall.AF_UNIX, syscall.SOCK_STREAM, 0)
	if err != nil {
		t.Fatalf("socketpair: %v", err)
	}
	mk := func(fd int) *net.UnixConn {
		f := os.NewFile(uintptr(fd), "sp")
		c, err := net.FileConn(f)
		_ = f.Close()
		if err != nil {
			t.Fatalf("FileConn: %v", err)
		}
		return c.(*net.UnixConn)
	}
	return mk(fds[0]), mk(fds[1])
}

// shmVersionReply builds a MIT-SHM QueryVersion reply: shared-pixmaps in
// byte 1, major/minor at bytes 8/10, pixmap-format at byte 16.
func shmVersionReply(order ByteOrder, sharedPix byte, maj, min uint16, fmt byte) []byte {
	var tail [24]byte
	order.PutUint16(tail[0:2], maj)
	order.PutUint16(tail[2:4], min)
	tail[8] = fmt // byte 16 overall
	return replyPacket(order, sharedPix, 1, tail, nil)
}

// queryExtReply builds a QueryExtension reply.
func queryExtReply(order ByteOrder, present, major, firstEvent, firstError byte) []byte {
	var tail [24]byte
	tail[0] = present // byte 8 overall
	tail[1] = major
	tail[2] = firstEvent
	tail[3] = firstError
	return replyPacket(order, 0, 1, tail, nil)
}

func TestQueryExtensionPresentAndAbsent(t *testing.T) {
	for _, order := range []ByteOrder{binary.LittleEndian, binary.BigEndian} {
		c, _ := dialFakeConn(t, order, queryExtReply(order, 1, 130, 65, 128))
		present, major, fe, ferr, err := c.QueryExtension("MIT-SHM")
		if err != nil {
			t.Fatalf("QueryExtension: %v", err)
		}
		if !present || major != 130 || fe != 65 || ferr != 128 {
			t.Errorf("present=%v major=%d fe=%d ferr=%d", present, major, fe, ferr)
		}

		c2, _ := dialFakeConn(t, order, queryExtReply(order, 0, 0, 0, 0))
		if present, _, _, _, err := c2.QueryExtension("NOPE"); err != nil || present {
			t.Errorf("absent extension: present=%v err=%v", present, err)
		}
	}
}

func TestQueryExtensionError(t *testing.T) {
	order := binary.LittleEndian
	c, _ := dialFakeConn(t, order, errorPacket(order, 17, 1, 0, opQueryExtension, 0))
	if _, _, _, _, err := c.QueryExtension("MIT-SHM"); err == nil {
		t.Error("server error should propagate")
	}
}

func TestQueryShm(t *testing.T) {
	for _, order := range []ByteOrder{binary.LittleEndian, binary.BigEndian} {
		extra := append(queryExtReply(order, 1, 130, 0, 0), shmVersionReply(order, 1, 1, 2, 2)...)
		c, _ := dialFakeConn(t, order, extra)
		shm, err := c.QueryShm()
		if err != nil {
			t.Fatalf("QueryShm: %v", err)
		}
		if shm == nil {
			t.Fatal("expected an Shm")
		}
		if shm.VerMajor != 1 || shm.VerMinor != 2 || !shm.SharedPix || shm.PixmapFmt != 2 {
			t.Errorf("shm = %+v", shm)
		}
		// fakeConn cannot pass fds, so FDCapable must be false despite v1.2.
		if shm.FDCapable {
			t.Error("FDCapable should be false without an fd-passing transport")
		}
	}
}

func TestQueryShmAbsent(t *testing.T) {
	order := binary.LittleEndian
	c, _ := dialFakeConn(t, order, queryExtReply(order, 0, 0, 0, 0))
	shm, err := c.QueryShm()
	if err != nil {
		t.Fatalf("QueryShm: %v", err)
	}
	if shm != nil {
		t.Error("absent extension should yield nil Shm")
	}
}

func TestQueryShmQueryExtensionError(t *testing.T) {
	order := binary.LittleEndian
	c, _ := dialFakeConn(t, order, errorPacket(order, 17, 1, 0, opQueryExtension, 0))
	if _, err := c.QueryShm(); err == nil {
		t.Error("QueryExtension error should propagate")
	}
}

func TestQueryShmVersionError(t *testing.T) {
	order := binary.LittleEndian
	extra := append(queryExtReply(order, 1, 130, 0, 0), errorPacket(order, 17, 2, 0, 130, 0)...)
	c, _ := dialFakeConn(t, order, extra)
	if _, err := c.QueryShm(); err == nil {
		t.Error("QueryVersion error should propagate")
	}
}

func TestShmAtLeast(t *testing.T) {
	cases := []struct {
		maj, min, wm, wn uint16
		want             bool
	}{
		{1, 2, 1, 2, true},
		{1, 3, 1, 2, true},
		{2, 0, 1, 2, true},
		{1, 1, 1, 2, false},
		{0, 9, 1, 0, false},
	}
	for _, c := range cases {
		if got := shmAtLeast(c.maj, c.min, c.wm, c.wn); got != c.want {
			t.Errorf("shmAtLeast(%d,%d,%d,%d)=%v want %v", c.maj, c.min, c.wm, c.wn, got, c.want)
		}
	}
}

// TestShmFDPathSuccess exercises the real SCM_RIGHTS fd-passing path: an Shm
// over a WrapUnix transport writing AttachFd/Detach/PutImage that a fake
// server reads back (with the descriptor), covering unixRW and sendRequestFD.
func TestShmFDPathSuccess(t *testing.T) {
	for _, order := range []ByteOrder{binary.LittleEndian, binary.BigEndian} {
		cli, srv := x11SocketPair(t)
		defer srv.Close()
		c := &Conn{rw: WrapUnix(cli), order: order}
		if !c.SupportsFDPassing() {
			t.Fatal("WrapUnix transport should support fd passing")
		}
		shm := &Shm{c: c, major: 130, VerMajor: 1, VerMinor: 2, FDCapable: true}

		// AttachFd passes a real descriptor.
		f, err := os.CreateTemp(t.TempDir(), "seg")
		if err != nil {
			t.Fatalf("temp: %v", err)
		}
		defer f.Close()
		if err := shm.AttachFd(0x600, int(f.Fd()), true); err != nil {
			t.Fatalf("AttachFd: %v", err)
		}
		obj, minor, body, gotFD := srvReadMsgFD(t, srv, order)
		if obj != 130 || minor != shmReqAttachFd {
			t.Errorf("attach major=%d minor=%d", obj, minor)
		}
		if seg := order.Uint32(body[0:4]); seg != 0x600 {
			t.Errorf("attach seg = %#x", seg)
		}
		if body[4] != 1 {
			t.Errorf("attach read-only = %d, want 1", body[4])
		}
		if gotFD < 0 {
			t.Error("no fd received by server")
		} else {
			syscall.Close(gotFD)
		}

		// PutImage: a single fixed-size request (no pixels on the wire).
		pres := &Presenter{depth: 24, bpp: 32, scanlinePad: 32}
		if err := shm.PutImage(pres, 0x111, 0x222, 0x600, 8, 800, 600, 1, 2, 3, 4, 5, 6); err != nil {
			t.Fatalf("PutImage: %v", err)
		}
		obj, minor, body, _ = srvReadMsgFD(t, srv, order)
		if obj != 130 || minor != shmReqPutImage {
			t.Errorf("putimage major=%d minor=%d", obj, minor)
		}
		if len(body) != 36 {
			t.Errorf("putimage body len = %d, want 36", len(body))
		}
		if dr := order.Uint32(body[0:4]); dr != 0x111 {
			t.Errorf("putimage drawable = %#x", dr)
		}

		// Detach.
		if err := shm.Detach(0x600); err != nil {
			t.Fatalf("Detach: %v", err)
		}
		obj, minor, body, _ = srvReadMsgFD(t, srv, order)
		if obj != 130 || minor != shmReqDetach || order.Uint32(body[0:4]) != 0x600 {
			t.Errorf("detach major=%d minor=%d seg=%#x", obj, minor, order.Uint32(body[0:4]))
		}
	}
}

// srvReadMsgFD reads one framed X request off srv, returning the major opcode
// (byte 0), the data byte (byte 1, i.e. the extension minor opcode), the body
// after the 4-byte header, and any received descriptor (-1 if none).
func srvReadMsgFD(t *testing.T, srv *net.UnixConn, order ByteOrder) (byte, byte, []byte, int) {
	t.Helper()
	data := make([]byte, 4096)
	oob := make([]byte, 256)
	n, oobn, _, _, err := srv.ReadMsgUnix(data, oob)
	if err != nil {
		t.Fatalf("ReadMsgUnix: %v", err)
	}
	fd := -1
	if oobn > 0 {
		if scms, e := syscall.ParseSocketControlMessage(oob[:oobn]); e == nil {
			for i := range scms {
				if fds, e := syscall.ParseUnixRights(&scms[i]); e == nil && len(fds) > 0 {
					fd = fds[0]
				}
			}
		}
	}
	msg := data[:n]
	length := int(order.Uint16(msg[2:4])) * 4
	return msg[0], msg[1], msg[4:length], fd
}

func TestShmFDPathNoTransportSupport(t *testing.T) {
	order := binary.LittleEndian
	c, _ := dialFakeConn(t, order, nil) // fakeConn cannot pass fds
	if c.SupportsFDPassing() {
		t.Fatal("fakeConn should not support fd passing")
	}
	shm := &Shm{c: c, major: 130}
	if err := shm.AttachFd(0x600, 3, false); err == nil {
		t.Error("AttachFd without fd support should error")
	}
}

func TestShmDetachAndPutImageWriteErrors(t *testing.T) {
	order := binary.LittleEndian
	c, fc := dialFakeConn(t, order, nil)
	fc.writeErr = errInjected
	shm := &Shm{c: c, major: 130}
	pres := &Presenter{depth: 24, bpp: 32, scanlinePad: 32}
	if err := shm.Detach(0x600); err == nil {
		t.Error("Detach write error should propagate")
	}
	if err := shm.PutImage(pres, 1, 2, 3, 0, 8, 8, 0, 0, 8, 8, 0, 0); err == nil {
		t.Error("PutImage write error should propagate")
	}
}

func TestSendRequestFDWriteError(t *testing.T) {
	order := binary.LittleEndian
	cli, srv := x11SocketPair(t)
	srv.Close() // force the write to fail
	c := &Conn{rw: WrapUnix(cli), order: order}
	shm := &Shm{c: c, major: 130}
	f, _ := os.CreateTemp(t.TempDir(), "seg")
	defer f.Close()
	if err := shm.AttachFd(0x600, int(f.Fd()), false); err == nil {
		t.Error("AttachFd over a closed socket should error")
	}
}

func TestUnixRWReadWriteClose(t *testing.T) {
	cli, srv := x11SocketPair(t)
	defer srv.Close()
	rw := WrapUnix(cli)
	// Write then read back through the peer.
	if _, err := rw.Write([]byte("ping")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	got := make([]byte, 4)
	if _, err := srv.Read(got); err != nil || string(got) != "ping" {
		t.Fatalf("peer read = %q err=%v", got, err)
	}
	if _, err := srv.Write([]byte("pong")); err != nil {
		t.Fatalf("peer Write: %v", err)
	}
	back := make([]byte, 4)
	if _, err := rw.Read(back); err != nil || string(back) != "pong" {
		t.Fatalf("Read = %q err=%v", back, err)
	}
	if err := rw.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

func TestEncodeRectIntoAndSegmentSize(t *testing.T) {
	pres := &Presenter{depth: 24, bpp: 32, scanlinePad: 32,
		rWidth: 8, gWidth: 8, bWidth: 8, rShift: 16, gShift: 8, bShift: 0}
	const W, H = 4, 3
	if got := pres.SegmentSize(W, H); got != W*4*H {
		t.Errorf("SegmentSize = %d, want %d", got, W*4*H)
	}
	src := make([]byte, 4*W*H)
	// pixel (1,1) = (10,20,30)
	off := (1*W + 1) * 4
	src[off], src[off+1], src[off+2], src[off+3] = 10, 20, 30, 255
	seg := make([]byte, pres.SegmentSize(W, H))
	if err := pres.EncodeRectInto(seg, W, src, W*4, 1, 1, 1, 1); err != nil {
		t.Fatalf("EncodeRectInto: %v", err)
	}
	line := pres.scanlineBytes(W)
	v := binary.LittleEndian.Uint32(seg[1*line+1*4:])
	if v != 0x000a141e {
		t.Errorf("encoded pixel = %#08x, want 0x000a141e", v)
	}
	// too-small destination.
	if err := pres.EncodeRectInto(make([]byte, 4), W, src, W*4, 0, 0, W, H); err == nil {
		t.Error("undersized segment should error")
	}
}
