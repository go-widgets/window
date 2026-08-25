// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build integration && linux

// Live MIT-SHM proof + measurement. Runs only under -tags=integration with
// WINDOW_X11_INTEGRATION set and a reachable X server (Xvfb in CI).
//
// TestLiveX11SHMActive proves the shipped Open() path actually presents via
// MIT-SHM fd-passing on a server that supports it (the pixels asserted by
// TestLiveX11 are therefore drawn through ShmPutImage, not PutImage).
//
// TestLiveX11SHMMeasure reproduces, in CI, the measurement behind the decision
// to ship: present latency and bytes-over-socket for ShmPutImage vs plain
// PutImage, for a full-window and a small-damage update. The numbers are
// logged and written to shm-measure.txt as a build artifact.
package window

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/go-widgets/window/internal/x11"

	xproto "github.com/go-freedesktop/x11"
)

// countConn wraps a *net.UnixConn, counting bytes written and supporting fd
// passing (x11.FDSender) so the MIT-SHM AttachFd path works.
type countConn struct {
	c     *net.UnixConn
	wrote int64
}

func (u *countConn) Read(b []byte) (int, error) { return u.c.Read(b) }
func (u *countConn) Write(b []byte) (int, error) {
	n, err := u.c.Write(b)
	atomic.AddInt64(&u.wrote, int64(n))
	return n, err
}
func (u *countConn) Close() error { return u.c.Close() }
func (u *countConn) SendFD(msg []byte, fd int) error {
	n, _, err := u.c.WriteMsgUnix(msg, syscall.UnixRights(fd), nil)
	atomic.AddInt64(&u.wrote, int64(n))
	return err
}
func (u *countConn) delta(reset bool) int64 {
	v := atomic.LoadInt64(&u.wrote)
	if reset {
		atomic.StoreInt64(&u.wrote, 0)
	}
	return v
}

// dialX11 opens the local X server socket for $DISPLAY (":N" -> the unix
// socket /tmp/.X11-unix/XN), returning the connection and the display number.
func dialX11(t *testing.T) (*net.UnixConn, string) {
	t.Helper()
	disp := os.Getenv("DISPLAY")
	if disp == "" || disp[0] != ':' {
		t.Skipf("DISPLAY=%q is not a local server", disp)
	}
	num := disp[1:]
	if i := indexByte(num, '.'); i >= 0 {
		num = num[:i]
	}
	nc, err := net.Dial("unix", "/tmp/.X11-unix/X"+num)
	if err != nil {
		t.Fatalf("dial X server: %v", err)
	}
	return nc.(*net.UnixConn), num
}

func indexByte(s string, b byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == b {
			return i
		}
	}
	return -1
}

func TestLiveX11SHMActive(t *testing.T) {
	if os.Getenv("WINDOW_X11_INTEGRATION") == "" {
		t.Skip("set WINDOW_X11_INTEGRATION=1 (and run under an X server) to enable")
	}
	b, err := Open(Config{Title: "gwshm-active", Width: 120, Height: 90})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer b.Close()
	w, ok := b.(*Window)
	if !ok {
		t.Fatalf("Open selected %T, want the X11 backend", b)
	}
	if w.shm == nil || w.seg == nil {
		t.Fatalf("MIT-SHM not active on this server (shm=%v seg=%v); the "+
			"live pixel proof would not be exercising the SHM path", w.shm != nil, w.seg != nil)
	}
	t.Logf("MIT-SHM active: version %d.%d, segment %d bytes, resource %#x",
		w.shm.VerMajor, w.shm.VerMinor, w.seg.Size(), w.seg.Seg)
}

func TestLiveX11SHMMeasure(t *testing.T) {
	if os.Getenv("WINDOW_X11_INTEGRATION") == "" {
		t.Skip("set WINDOW_X11_INTEGRATION=1 (and run under an X server) to enable")
	}
	nc, dispNum := dialX11(t)
	cc := &countConn{c: nc}
	host, _ := os.Hostname()
	authName, authData, _ := xproto.LoadAuthCookie(authFilePathEnv(), host, dispNum)
	conn, err := x11.Handshake(cc, binary.LittleEndian, authName, authData)
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	defer conn.Close()

	setup := conn.Setup()
	screen := &setup.Screens[0]
	pres, err := x11.NewPresenter(setup, screen.RootVisualType(), screen.RootDepth)
	if err != nil {
		t.Fatalf("presenter: %v", err)
	}

	const W, H = 800, 600
	win, gc := conn.NewID(), conn.NewID()
	if err := conn.CreateWindow(win, screen.Root, 0, 0, W, H, screen.BlackPixel, screen.BlackPixel, x11.DefaultEventMask); err != nil {
		t.Fatalf("create window: %v", err)
	}
	if err := conn.CreateGC(gc, win); err != nil {
		t.Fatalf("create gc: %v", err)
	}
	if err := conn.MapWindow(win); err != nil {
		t.Fatalf("map: %v", err)
	}

	buf := make([]byte, 4*W*H)
	for i := range buf {
		buf[i] = byte(i)
	}

	shm, err := conn.QueryShm()
	if err != nil {
		t.Fatalf("query shm: %v", err)
	}
	if shm == nil || !shm.FDCapable {
		t.Fatalf("server lacks MIT-SHM 1.2 fd-passing (shm=%v); cannot measure the SHM path", shm != nil)
	}
	seg, err := xproto.NewSegment(conn.NewID(), pres.SegmentSize(W, H))
	if err != nil {
		t.Fatalf("new segment: %v", err)
	}
	defer seg.Close()
	if err := shm.AttachFd(seg.Seg, seg.FD, false); err != nil {
		t.Fatalf("attach fd: %v", err)
	}
	defer shm.Detach(seg.Seg)

	sync := func() { _, _ = conn.InternAtom("X", true) }
	const iters = 300
	report := fmt.Sprintf("MIT-SHM measurement (Xvfb, %dx%d depth %d, %d iters)\n", W, H, screen.RootDepth, iters)

	measure := func(name string, put func() error) {
		cc.delta(true)
		for i := 0; i < iters; i++ {
			if err := put(); err != nil {
				t.Fatalf("%s present: %v", name, err)
			}
		}
		bytes := cc.delta(true)
		start := time.Now()
		for i := 0; i < iters; i++ {
			_ = put()
			sync()
		}
		ms := float64(time.Since(start).Microseconds()) / iters / 1000
		line := fmt.Sprintf("  %-26s %10.1f bytes/present  %8.3f ms/present\n", name, float64(bytes)/iters, ms)
		report += line
		t.Log(line[:len(line)-1])
	}

	putFull := func() error { return conn.PutImage(pres, win, gc, buf, W*4, 0, 0, W, H, 0, 0) }
	shmFull := func() error {
		if err := pres.EncodeRectInto(seg.Data, W, buf, W*4, 0, 0, W, H); err != nil {
			return err
		}
		return shm.PutImage(pres, win, gc, seg.Seg, 0, W, H, 0, 0, W, H, 0, 0)
	}
	const sw, sh = 32, 32
	putSmall := func() error { return conn.PutImage(pres, win, gc, buf, W*4, 100, 100, sw, sh, 100, 100) }
	shmSmall := func() error {
		if err := pres.EncodeRectInto(seg.Data, W, buf, W*4, 100, 100, sw, sh); err != nil {
			return err
		}
		return shm.PutImage(pres, win, gc, seg.Seg, 0, W, H, 100, 100, sw, sh, 100, 100)
	}

	for i := 0; i < 20; i++ { // warm up
		_ = putFull()
		_ = shmFull()
	}
	sync()

	report += "full window:\n"
	measure("PutImage (socket)", putFull)
	measure("ShmPutImage (shared mem)", shmFull)
	report += "small damage (32x32):\n"
	measure("PutImage (socket)", putSmall)
	measure("ShmPutImage (shared mem)", shmSmall)

	if err := os.WriteFile("shm-measure.txt", []byte(report), 0o644); err == nil {
		t.Log("wrote shm-measure.txt")
	}
}
