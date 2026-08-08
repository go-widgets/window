// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// This is the in-process scripted fake-compositor proof for the Wayland
// backend. A goroutine plays a minimal xdg-shell compositor over one end of
// a socket pair, speaking the sovereign wire format byte for byte, while the
// backend brings up a toplevel and runs its host loop over the other end.
// It deterministically exercises the whole handshake (registry, binds, shm
// formats, seat capabilities, xkb keymap over an fd, xdg configure/ack) and
// the input path (a click and a key press synthesised by the compositor and
// asserted as dispatched toolkit events), with no display server involved —
// the runtime analogue proven live by the CI headless-compositor lane.
package window

import (
	"encoding/binary"
	"net"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/window/internal/wayland"
)

var no = binary.NativeEndian

// --- server-side wire helpers ---------------------------------------------

type srvConn struct {
	c    *net.UnixConn
	rbuf []byte
	fds  []int
}

func (s *srvConn) read() (obj uint32, op uint16, body []byte, err error) {
	for {
		if len(s.rbuf) >= 8 {
			size := int(no.Uint32(s.rbuf[4:8]) >> 16)
			if size >= 8 && len(s.rbuf) >= size {
				msg := s.rbuf[:size]
				obj = no.Uint32(msg[0:4])
				op = uint16(no.Uint32(msg[4:8]) & 0xffff)
				body = append([]byte(nil), msg[8:size]...)
				s.rbuf = s.rbuf[size:]
				return obj, op, body, nil
			}
		}
		data := make([]byte, 4096)
		oob := make([]byte, 4096)
		n, oobn, _, _, e := s.c.ReadMsgUnix(data, oob)
		if e != nil {
			return 0, 0, nil, e
		}
		s.rbuf = append(s.rbuf, data[:n]...)
		if oobn > 0 {
			scms, _ := syscall.ParseSocketControlMessage(oob[:oobn])
			for i := range scms {
				if fds, e := syscall.ParseUnixRights(&scms[i]); e == nil {
					s.fds = append(s.fds, fds...)
				}
			}
		}
	}
}

func (s *srvConn) popFD() int {
	if len(s.fds) == 0 {
		return -1
	}
	fd := s.fds[0]
	s.fds = s.fds[1:]
	return fd
}

func (s *srvConn) send(obj uint32, op uint16, body []byte, fds ...int) error {
	m := make([]byte, 8+len(body))
	no.PutUint32(m[0:4], obj)
	no.PutUint32(m[4:8], uint32(op)|uint32(8+len(body))<<16)
	copy(m[8:], body)
	var oob []byte
	if len(fds) > 0 {
		oob = syscall.UnixRights(fds...)
	}
	_, _, err := s.c.WriteMsgUnix(m, oob, nil)
	return err
}

func eU32(vs ...uint32) []byte {
	b := make([]byte, 4*len(vs))
	for i, v := range vs {
		no.PutUint32(b[i*4:], v)
	}
	return b
}

func eStr(s string) []byte {
	n := len(s) + 1
	b := make([]byte, 4+(n+3)&^3)
	no.PutUint32(b[0:4], uint32(n))
	copy(b[4:], s)
	return b
}

func eArr(a []byte) []byte {
	b := make([]byte, 4+(len(a)+3)&^3)
	no.PutUint32(b[0:4], uint32(len(a)))
	copy(b[4:], a)
	return b
}

func eFixed(i int) []byte { return eU32(uint32(int32(i) << 8)) }

func cat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func decStr(b []byte) (string, []byte) {
	n := int(no.Uint32(b[0:4]))
	padded := (n + 3) &^ 3
	s := ""
	if n > 0 {
		s = string(b[4 : 4+n-1])
	}
	return s, b[4+padded:]
}

// keymapFile writes the test xkb keymap (NUL-terminated) to a temp file and
// returns it; the caller passes file.Fd() over SCM_RIGHTS.
func keymapFile(t *testing.T) *os.File {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "keymap")
	if err != nil {
		t.Fatalf("keymap temp: %v", err)
	}
	if _, err := f.WriteString(testKeymap + "\x00"); err != nil {
		t.Fatalf("keymap write: %v", err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatalf("keymap seek: %v", err)
	}
	return f
}

// fakeCompositor plays the scripted xdg-shell compositor until the client
// disconnects. cfgW/cfgH are the size it suggests via xdg_toplevel.configure.
func fakeCompositor(t *testing.T, sc *srvConn, kmFD int, cfgW, cfgH int) {
	var registryID, compID, shmID, wmID, seatID, ptrID, kbID uint32
	var surfID, xdgSurfID, tlID uint32
	serial := uint32(1)
	sawAttach := false
	configured := false
	injected := false

	for {
		obj, op, body, err := sc.read()
		if err != nil {
			return // client closed
		}
		switch {
		case obj == 1 && op == 1: // wl_display.get_registry
			registryID = no.Uint32(body[0:4])
			_ = sc.send(registryID, 0, cat(eU32(1), eStr("wl_compositor"), eU32(4)))
			_ = sc.send(registryID, 0, cat(eU32(2), eStr("wl_shm"), eU32(1)))
			_ = sc.send(registryID, 0, cat(eU32(3), eStr("xdg_wm_base"), eU32(4)))
			_ = sc.send(registryID, 0, cat(eU32(4), eStr("wl_seat"), eU32(5)))
		case obj == 1 && op == 0: // wl_display.sync
			_ = sc.send(no.Uint32(body[0:4]), 0, eU32(0)) // wl_callback.done
		case obj == registryID && op == 0: // wl_registry.bind
			iface, rest := decStr(body[4:])
			newid := no.Uint32(rest[4:8])
			switch iface {
			case "wl_compositor":
				compID = newid
			case "wl_shm":
				shmID = newid
				_ = sc.send(shmID, 0, eU32(wayland.ShmFormatARGB8888))
				_ = sc.send(shmID, 0, eU32(wayland.ShmFormatXRGB8888))
			case "xdg_wm_base":
				wmID = newid
			case "wl_seat":
				seatID = newid
				_ = sc.send(seatID, 0, eU32(wayland.SeatCapabilityPointer|wayland.SeatCapabilityKeyboard))
				_ = sc.send(seatID, 1, eStr("seat0"))
			}
		case obj == seatID && op == 0: // wl_seat.get_pointer
			ptrID = no.Uint32(body[0:4])
		case obj == seatID && op == 1: // wl_seat.get_keyboard
			kbID = no.Uint32(body[0:4])
			_ = sc.send(kbID, 0, cat(eU32(wayland.KeymapFormatXkbV1), eU32(uint32(len(testKeymap)+1))), kmFD)
			_ = sc.send(kbID, 5, cat(eU32(25), eU32(600))) // repeat_info
		case obj == compID && op == 0: // wl_compositor.create_surface
			surfID = no.Uint32(body[0:4])
		case obj == wmID && op == 2: // xdg_wm_base.get_xdg_surface
			xdgSurfID = no.Uint32(body[0:4])
		case obj == xdgSurfID && op == 1: // xdg_surface.get_toplevel
			tlID = no.Uint32(body[0:4])
		case obj == shmID && op == 0: // wl_shm.create_pool (fd passed)
			if fd := sc.popFD(); fd >= 0 {
				_ = syscall.Close(fd)
			}
		case obj == surfID && op == 1: // wl_surface.attach
			sawAttach = true
		case obj == surfID && op == 6: // wl_surface.commit
			switch {
			case !configured: // role commit -> first configure
				_ = sc.send(tlID, 0, cat(eU32(uint32(cfgW)), eU32(uint32(cfgH)), eArr(nil)))
				_ = sc.send(xdgSurfID, 0, eU32(serial))
				serial++
				configured = true
			case sawAttach && !injected: // first present -> synthesise input, then close
				injected = true
				_ = sc.send(ptrID, 0, cat(eU32(serial), eU32(surfID), eFixed(30), eFixed(40)))
				serial++
				_ = sc.send(ptrID, 3, cat(eU32(serial), eU32(0), eU32(wayland.BtnLeft), eU32(wayland.StatePressed)))
				serial++
				_ = sc.send(kbID, 3, cat(eU32(serial), eU32(0), eU32(evA), eU32(wayland.StatePressed)))
				serial++
				_ = sc.send(tlID, 1, nil) // xdg_toplevel.close
			}
			sawAttach = false
		}
	}
}

func TestWaylandBringupAndRun(t *testing.T) {
	cli, srv := socketPairWin(t)
	defer srv.Close()

	km := keymapFile(t)
	defer km.Close()

	sc := &srvConn{c: srv}
	go fakeCompositor(t, sc, int(km.Fd()), 200, 160)

	conn := wayland.New(cli)
	w, err := newWaylandWindow(conn, Config{Title: "wl-test", Class: "wltest", Width: 100, Height: 80})
	if err != nil {
		t.Fatalf("newWaylandWindow: %v", err)
	}
	// The compositor-suggested size is applied by the run loop, not at
	// bring-up, so it still reads the initial size here.
	if gw, gh := w.Size(); gw != 100 || gh != 80 {
		t.Fatalf("size at bring-up = %dx%d, want 100x80", gw, gh)
	}
	if w.String() == "" {
		t.Fatal("String empty")
	}

	root := &recWidget{}
	done := make(chan error, 1)
	go func() { done <- w.Run(root) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not exit after compositor close")
	}

	// The run loop applied the compositor's 200x160 configure.
	if gw, gh := w.Size(); gw != 200 || gh != 160 {
		t.Fatalf("size after run = %dx%d, want 200x160", gw, gh)
	}

	var gotClick, gotChar bool
	var cx, cy int
	for _, ev := range root.events {
		switch {
		case ev.Kind == toolkit.EventClick:
			gotClick, cx, cy = true, ev.X, ev.Y
		case ev.Kind == toolkit.EventChar && ev.Code == "a":
			gotChar = true
		}
	}
	if !gotClick {
		t.Errorf("no EventClick dispatched; events=%+v", root.events)
	} else if cx != 30 || cy != 40 {
		t.Errorf("click at (%d,%d), want (30,40)", cx, cy)
	}
	if !gotChar {
		t.Errorf("no EventChar 'a' dispatched; events=%+v", root.events)
	}
	if root.drawn == 0 {
		t.Error("root never drawn")
	}
	if err := w.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
	if err := w.Close(); err != nil { // idempotent
		t.Errorf("second Close: %v", err)
	}
}

// socketPairWin returns two connected *net.UnixConn endpoints (the window
// package's local copy of the socketpair helper).
func socketPairWin(t *testing.T) (*net.UnixConn, *net.UnixConn) {
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
