// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package window

import (
	"errors"
	"fmt"
	"syscall"
	"testing"
	"time"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/window/internal/wayland"
)

// incRoot is an incremental root that paints its first frame and then reports
// that nothing changed, which is what a static scene does.
type incRoot struct {
	toolkit.Base
	calls int
}

func (r *incRoot) Draw(painter.Painter, *toolkit.Theme) {}
func (r *incRoot) OnEvent(toolkit.Event)                {}
func (r *incRoot) RenderDamaged(p painter.Painter, _ *toolkit.Theme) []toolkit.Rect {
	r.calls++
	if r.calls > 1 {
		return nil
	}
	b := r.Bounds()
	p.FillRect(b, painter.RGBA{G: 200, A: 255})
	return []toolkit.Rect{b}
}

// surfaceLog is what a compositor saw of the surface, in order: "attach:<id>"
// (0 is the null buffer), "commit", "ack:<serial>".
type surfaceLog chan string

// mappingCompositor behaves as xdg-shell says a compositor does about mapping:
// a commit with no buffer from a surface that is not mapped is an initial
// commit and gets a configure; a buffer committed after the ack maps the
// surface; a null buffer committed unmaps it, and it must start over.
func mappingCompositor(sc *srvConn, log surfaceLog) {
	var registryID, compID, shmID, wmID uint32
	var surfID, xdgSurfID, tlID uint32
	serial := uint32(1)
	var pending uint32 // the buffer attached since the last commit
	attached := false  // whether an attach happened since the last commit
	mapped := false

	for {
		obj, op, body, err := sc.read()
		if err != nil {
			close(log)
			return
		}
		switch {
		case obj == 1 && op == 1:
			registryID = no.Uint32(body[0:4])
			_ = sc.send(registryID, 0, cat(eU32(1), eStr("wl_compositor"), eU32(4)))
			_ = sc.send(registryID, 0, cat(eU32(2), eStr("wl_shm"), eU32(1)))
			_ = sc.send(registryID, 0, cat(eU32(3), eStr("xdg_wm_base"), eU32(4)))
		case obj == 1 && op == 0:
			_ = sc.send(no.Uint32(body[0:4]), 0, eU32(0))
		case obj == registryID && op == 0:
			iface, rest := decStr(body[4:])
			newid := no.Uint32(rest[4:8])
			switch iface {
			case "wl_compositor":
				compID = newid
			case "wl_shm":
				shmID = newid
				_ = sc.send(shmID, 0, eU32(wayland.ShmFormatARGB8888))
			case "xdg_wm_base":
				wmID = newid
			}
		case obj == compID && op == 0:
			surfID = no.Uint32(body[0:4])
		case obj == wmID && op == 2:
			xdgSurfID = no.Uint32(body[0:4])
		case obj == xdgSurfID && op == 1:
			tlID = no.Uint32(body[0:4])
		case obj == xdgSurfID && op == 4: // ack_configure
			log <- fmt.Sprintf("ack:%d", no.Uint32(body[0:4]))
		case obj == shmID && op == 0:
			if fd := sc.popFD(); fd >= 0 {
				_ = syscall.Close(fd)
			}
		case obj == surfID && op == 1: // attach
			pending, attached = no.Uint32(body[0:4]), true
			log <- fmt.Sprintf("attach:%d", pending)
		case obj == surfID && op == 6: // commit
			log <- "commit"
			switch {
			case attached && pending == 0:
				mapped = false
			case attached:
				mapped = true
			case !mapped:
				// An initial commit: configure the toplevel.
				_ = sc.send(tlID, 0, cat(eU32(160), eU32(120), eArr(nil)))
				_ = sc.send(xdgSurfID, 0, eU32(serial))
				serial++
			}
			attached = false
		}
	}
}

// expectSeq reads the log until it has seen want in order (other entries
// between are allowed), or fails.
func expectSeq(t *testing.T, log surfaceLog, what string, want ...string) {
	t.Helper()
	var seen []string
	deadline := time.After(5 * time.Second)
	for len(want) > 0 {
		select {
		case got, ok := <-log:
			if !ok {
				t.Fatalf("%s: the client hung up; saw %v, still want %v", what, seen, want)
			}
			seen = append(seen, got)
			if matches(got, want[0]) {
				want = want[1:]
			}
		case <-deadline:
			t.Fatalf("%s: saw %v, still waiting for %v", what, seen, want)
		}
	}
}

// matches is equality, except that "attach:+" stands for any real buffer.
func matches(got, want string) bool {
	if want == "attach:+" {
		return len(got) > len("attach:") && got[:len("attach:")] == "attach:" && got != "attach:0"
	}
	return got == want
}

// quiet asserts the client says nothing to the surface for a moment.
func quiet(t *testing.T, log surfaceLog, what string) {
	t.Helper()
	select {
	case got := <-log:
		t.Fatalf("%s: the client sent %q", what, got)
	case <-time.After(200 * time.Millisecond):
	}
}

// TestWaylandHideUnmapsAndShowMapsAgain (go-widgets/window#134).
//
// xdg-shell's way to take a toplevel off the screen is a null buffer, and the
// way back is the bring-up again: a commit with no buffer, the configure, the
// ack, then a buffer. A client that attaches a buffer before the new configure
// is a protocol error that kills the connection, so the order is the test.
func TestWaylandHideUnmapsAndShowMapsAgain(t *testing.T) {
	for _, tc := range []struct {
		name string
		root func() toolkit.Widget
	}{
		{"plain root", func() toolkit.Widget { return &countingWLRoot{} }},
		// The one that needs remap: after its first frame it reports no damage,
		// so without it nothing would ever be attached again after a Show.
		{"incremental root", func() toolkit.Widget { return &incRoot{} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cli, srv := socketPairWin(t)
			defer srv.Close()
			log := make(surfaceLog, 64)
			go mappingCompositor(&srvConn{c: srv}, log)

			w, err := newWaylandWindow(wayland.New(cli), Config{Title: "vis", Width: 160, Height: 120})
			if err != nil {
				t.Fatalf("newWaylandWindow: %v", err)
			}
			var b Backend = w
			done := make(chan error, 1)
			go func() { done <- w.Run(tc.root()) }()

			expectSeq(t, log, "bring-up", "commit", "ack:1", "attach:+", "commit")
			drainLog(log)

			if err := Hide(b); err != nil {
				t.Fatalf("Hide: %v", err)
			}
			expectSeq(t, log, "Hide", "attach:0", "commit")
			quiet(t, log, "while hidden")

			// A repaint while hidden must not put a buffer back: the surface is
			// unmapped, and attaching before a configure is a protocol error.
			w.Repaint()
			quiet(t, log, "a repaint while hidden")

			if err := Show(b); err != nil {
				t.Fatalf("Show: %v", err)
			}
			expectSeq(t, log, "Show", "commit", "ack:2", "attach:+", "commit")

			if err := Raise(b); !errors.Is(err, ErrNotSupported) {
				t.Errorf("Raise = %v, want ErrNotSupported", err)
			}

			_ = w.Close()
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("Run did not return after Close")
			}
			for name, call := range map[string]func() error{"Show": w.Show, "Hide": w.Hide} {
				if err := call(); !errors.Is(err, ErrClosed) {
					t.Errorf("%s after Close = %v, want ErrClosed", name, err)
				}
			}
		})
	}
}

// A Hide asked for twice unmaps once, and a Show of a window that is not
// hidden does nothing at all.
func TestWaylandVisibilityIsIdempotent(t *testing.T) {
	cli, srv := socketPairWin(t)
	defer srv.Close()
	log := make(surfaceLog, 64)
	go mappingCompositor(&srvConn{c: srv}, log)
	w, err := newWaylandWindow(wayland.New(cli), Config{Title: "vis", Width: 160, Height: 120})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	// An incremental root with nothing to draw: the wake every request carries
	// would make a plain root repaint, which is not what is being counted.
	go func() { done <- w.Run(&incRoot{}) }()
	expectSeq(t, log, "bring-up", "commit", "ack:1", "attach:+", "commit")
	drainLog(log)

	if err := w.Show(); err != nil {
		t.Fatal(err)
	}
	quiet(t, log, "Show of a visible window")

	_ = w.Hide()
	expectSeq(t, log, "Hide", "attach:0", "commit")
	_ = w.Hide()
	quiet(t, log, "a second Hide")

	_ = w.Close()
	<-done
}

// drainLog empties what the log holds now.
func drainLog(log surfaceLog) {
	for {
		select {
		case <-log:
		case <-time.After(100 * time.Millisecond):
			return
		}
	}
}
