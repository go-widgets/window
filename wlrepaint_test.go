// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// The Repainter capability on the Wayland back-end, against a fake compositor
// that says nothing after the first configure — which is the whole point. An
// idle window is one the compositor has stopped talking to, and this proves a
// frame can still be asked for from outside the loop.

//go:build linux

package window

import (
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/window/internal/wayland"
)

// quietCompositor configures the toplevel and then stays silent, so the client's
// dispatch is genuinely blocked on the socket. It closes the toplevel when
// close is closed, which is how the test ends the run loop.
func quietCompositor(t *testing.T, sc *srvConn, closeReq <-chan struct{}) {
	var registryID, compID, shmID, wmID uint32
	var surfID, xdgSurfID, tlID uint32
	serial := uint32(1)
	configured := false
	done := make(chan struct{})

	go func() { // the close request, once the test asks for it
		<-closeReq
		<-done // tlID is only known after the bring-up below
		_ = sc.send(tlID, 1, nil)
	}()

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
		case obj == 1 && op == 0: // wl_display.sync
			_ = sc.send(no.Uint32(body[0:4]), 0, eU32(0))
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
			}
		case obj == compID && op == 0:
			surfID = no.Uint32(body[0:4])
		case obj == wmID && op == 2:
			xdgSurfID = no.Uint32(body[0:4])
		case obj == xdgSurfID && op == 1:
			tlID = no.Uint32(body[0:4])
		case obj == shmID && op == 0: // create_pool carries the framebuffer fd
			if fd := sc.popFD(); fd >= 0 {
				_ = syscall.Close(fd)
			}
		case obj == surfID && op == 6: // commit
			if !configured {
				configured = true
				_ = sc.send(tlID, 0, cat(eU32(160), eU32(120), eArr(nil)))
				_ = sc.send(xdgSurfID, 0, eU32(serial))
				serial++
				close2(done) // the bring-up is over; tlID is known
			}
		}
	}
}

// close2 closes ch once, so a compositor that sees several commits does not
// panic on the second.
func close2(ch chan struct{}) {
	select {
	case <-ch:
	default:
		close(ch)
	}
}

// countingWLRoot counts its frames, which is what a repaint has to produce.
type countingWLRoot struct {
	toolkit.Base
	mu sync.Mutex
	n  int
}

func (r *countingWLRoot) Draw(p painter.Painter, _ *toolkit.Theme) {
	r.mu.Lock()
	r.n++
	r.mu.Unlock()
	p.FillRect(r.Bounds(), painter.RGBA{R: 20, G: 120, B: 200, A: 255})
}

func (r *countingWLRoot) OnEvent(toolkit.Event) {}

func (r *countingWLRoot) frames() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.n
}

// A window nobody is talking to still paints when Repaint asks it to.
//
// The compositor deliberately goes quiet after the configure, so the client is
// blocked in a socket read with nothing to deliver. That is the state an
// application spends most of its life in, and the one where a completed fetch
// used to be invisible.
func TestWaylandRepaintWakesAQuietLoop(t *testing.T) {
	cli, srv := socketPairWin(t)
	defer srv.Close()
	closeReq := make(chan struct{})
	go quietCompositor(t, &srvConn{c: srv}, closeReq)

	conn := wayland.New(cli)
	w, err := newWaylandWindow(conn, Config{Title: "repaint", Width: 160, Height: 120})
	if err != nil {
		t.Fatalf("newWaylandWindow: %v", err)
	}
	root := &countingWLRoot{}
	done := make(chan error, 1)
	go func() { done <- w.Run(root) }()

	// Wait until the loop is idle: it has painted and is blocked on the socket.
	idle := waitForFrames(t, root, 1)
	// Nothing arrives on its own -- the control for the assertion below.
	time.Sleep(300 * time.Millisecond)
	if now := root.frames(); now != idle {
		t.Fatalf("a quiet window drew %d frames on its own, so this proves nothing", now-idle)
	}

	w.Repaint()
	if got := waitForFrames(t, root, idle+1); got <= idle {
		t.Fatalf("after Repaint the window had drawn %d frames, want more than %d", got, idle)
	}

	close(closeReq)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the run loop never exited")
	}
}

// A second Repaint before the loop has consumed the first costs nothing and
// loses nothing: the frame that follows covers both.
func TestWaylandRepaintCoalescesAndKeepsGoing(t *testing.T) {
	cli, srv := socketPairWin(t)
	defer srv.Close()
	closeReq := make(chan struct{})
	go quietCompositor(t, &srvConn{c: srv}, closeReq)

	conn := wayland.New(cli)
	w, err := newWaylandWindow(conn, Config{Title: "repaint", Width: 160, Height: 120})
	if err != nil {
		t.Fatalf("newWaylandWindow: %v", err)
	}
	root := &countingWLRoot{}
	done := make(chan error, 1)
	go func() { done <- w.Run(root) }()
	idle := waitForFrames(t, root, 1)

	for i := 0; i < 5; i++ {
		w.Repaint()
	}
	waitForFrames(t, root, idle+1)

	// The important half: the loop is still able to wake AFTER the burst. A
	// deadline left in the past would have turned it into a spin, and one never
	// cleared would have frozen it here.
	//
	// Let the burst drain FIRST. waitForFrames returns at the first frame past
	// the threshold, so the other four Repaints may still be in flight, and one
	// landing inside the quiet window below is indistinguishable from a spin.
	// On a fast machine the burst is always drained by now; under qemu it is
	// not, which is why this failed on riscv64 and nowhere else.
	before := waitUntilQuiet(t, root)
	time.Sleep(200 * time.Millisecond)
	if spun := root.frames() - before; spun != 0 {
		t.Fatalf("the loop drew %d frames with nobody asking: the read deadline was left armed", spun)
	}
	w.Repaint()
	if got := waitForFrames(t, root, before+1); got <= before {
		t.Fatalf("a later Repaint drew nothing: frames %d, want more than %d", got, before)
	}

	close(closeReq)
	<-done
}

// waitUntilQuiet waits for the frame count to stop moving and returns it, so a
// caller can assert about an idle loop rather than about one still catching up.
// Two equal samples in a row is the signal: a single one only says no frame
// landed in that gap, which a slow emulator can produce mid-burst.
func waitUntilQuiet(t *testing.T, root *countingWLRoot) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	last := -1
	for time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		got := root.frames()
		if got == last {
			return got
		}
		last = got
	}
	t.Fatalf("the frame count never settled: still %d", root.frames())
	return 0
}

// waitForFrames waits until the root has drawn at least n frames, and returns
// how many it had drawn.
func waitForFrames(t *testing.T, root *countingWLRoot, n int) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if got := root.frames(); got >= n {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("the root drew %d frames, want at least %d", root.frames(), n)
	return 0
}
