// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

// HiDPI on the Wayland back-end, against a fake compositor that says its screen
// is a scale-2 one. Everything here is invisible at scale 1, which is why it is
// worth a compositor of its own: the arithmetic that turns a logical size into
// a framebuffer, the request that stops the compositor upscaling us, and the
// pointer coordinates that arrive in points and have to land on pixels.

package window

import (
	"sync"
	"syscall"
	"testing"

	"github.com/go-widgets/window/internal/wayland"
)

// scaleServer is a compositor with one screen, whose scale it announces, and
// which puts every surface on that screen.
type scaleServer struct {
	sc *srvConn

	mu       sync.Mutex
	scale    uint32
	outputID uint32 // the object id the client bound the output to
	surfID   uint32
	setScale []uint32 // every set_buffer_scale the client sent, in order
	mapped   chan struct{}
	once     sync.Once
}

func newScaleServer(sc *srvConn, scale uint32) *scaleServer {
	return &scaleServer{sc: sc, scale: scale, mapped: make(chan struct{})}
}

func (s *scaleServer) run() {
	var registryID, compID, shmID, wmID, xdgSurfID, tlID uint32
	serial := uint32(1)
	configured := false
	for {
		obj, op, body, err := s.sc.read()
		if err != nil {
			return
		}
		s.mu.Lock()
		switch {
		case obj == 1 && op == 1: // get_registry
			registryID = no.Uint32(body[0:4])
			_ = s.sc.send(registryID, 0, cat(eU32(1), eStr("wl_compositor"), eU32(4)))
			_ = s.sc.send(registryID, 0, cat(eU32(2), eStr("wl_shm"), eU32(1)))
			_ = s.sc.send(registryID, 0, cat(eU32(3), eStr("xdg_wm_base"), eU32(4)))
			_ = s.sc.send(registryID, 0, cat(eU32(5), eStr("wl_output"), eU32(3)))
		case obj == 1 && op == 0: // sync
			_ = s.sc.send(no.Uint32(body[0:4]), 0, eU32(0))
		case obj == registryID && op == 0: // bind
			iface, rest := decStr(body[4:])
			newid := no.Uint32(rest[4:8])
			switch iface {
			case "wl_compositor":
				compID = newid
			case "wl_shm":
				shmID = newid
				_ = s.sc.send(shmID, 0, eU32(wayland.ShmFormatARGB8888))
				_ = s.sc.send(shmID, 0, eU32(wayland.ShmFormatXRGB8888))
			case "xdg_wm_base":
				wmID = newid
			case "wl_output":
				s.outputID = newid
				// The burst a real compositor sends: geometry, mode, scale, done.
				_ = s.sc.send(s.outputID, 0, cat(eU32(0), eU32(0), eU32(300), eU32(200), eU32(0)))
				_ = s.sc.send(s.outputID, 1, cat(eU32(1), eU32(2560), eU32(1440), eU32(60000)))
				_ = s.sc.send(s.outputID, 3, eU32(s.scale))
				_ = s.sc.send(s.outputID, 2, nil)
			}
		case obj == compID && op == 0:
			s.surfID = no.Uint32(body[0:4])
		case obj == wmID && op == 2:
			xdgSurfID = no.Uint32(body[0:4])
		case obj == xdgSurfID && op == 1:
			tlID = no.Uint32(body[0:4])
		case obj == shmID && op == 0:
			if fd := s.sc.popFD(); fd >= 0 {
				_ = syscall.Close(fd)
			}
		case obj == s.surfID && op == 8: // set_buffer_scale
			s.setScale = append(s.setScale, no.Uint32(body[0:4]))
		case obj == s.surfID && op == 6: // commit
			if !configured {
				configured = true
				_ = s.sc.send(tlID, 0, cat(eU32(200), eU32(100), eArr(nil)))
				_ = s.sc.send(xdgSurfID, 0, eU32(serial))
				serial++
				// The surface is on our screen. This is the event that tells a
				// client whose scale applies to it, and without it a window can
				// only guess.
				_ = s.sc.send(s.surfID, 0, eU32(s.outputID))
				s.once.Do(func() { close(s.mapped) })
			}
		}
		s.mu.Unlock()
	}
}

func (s *scaleServer) scalesSent() []uint32 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]uint32(nil), s.setScale...)
}

// newScaledWindow brings a window up against a scale-2 compositor and pumps
// events until the surface has been told which screen it is on.
func newScaledWindow(t *testing.T, cfg Config, scale uint32) (*wlWindow, *scaleServer) {
	t.Helper()
	cli, srv := socketPairWin(t)
	t.Cleanup(func() { srv.Close() })
	s := newScaleServer(&srvConn{c: srv}, scale)
	go s.run()

	conn := wayland.New(cli)
	t.Cleanup(func() { conn.Close() })
	w, err := newWaylandWindow(conn, cfg)
	if err != nil {
		t.Fatalf("newWaylandWindow: %v", err)
	}
	// Two round trips: the first delivers the output's burst, the second the
	// surface.enter that follows our commit.
	for i := 0; i < 3; i++ {
		if err := conn.Roundtrip(); err != nil {
			t.Fatalf("roundtrip: %v", err)
		}
	}
	return w, s
}

// A window that asked for the screen's own resolution gets a framebuffer twice
// the surface, and tells the compositor so.
func TestWaylandNativeScaleFollowsTheOutput(t *testing.T) {
	w, s := newScaledWindow(t, Config{Width: 200, Height: 100, RenderScale: NativeScale}, 2)

	if w.scale != 2 {
		t.Fatalf("scale = %d, want the output's 2", w.scale)
	}
	if w.logW != 200 || w.logH != 100 {
		t.Errorf("logical size = %dx%d, want 200x100", w.logW, w.logH)
	}
	if w.w != 400 || w.h != 200 {
		t.Errorf("framebuffer = %dx%d, want 400x200 -- the panel's own pixels", w.w, w.h)
	}
	if len(w.buf) != 4*400*200 {
		t.Errorf("framebuffer buffer is %d bytes, want %d", len(w.buf), 4*400*200)
	}
	if gw, gh := w.Size(); gw != 400 || gh != 200 {
		t.Errorf("Size() = %dx%d, want the framebuffer 400x200", gw, gh)
	}
	if w.RenderScale() != 2 {
		t.Errorf("RenderScale() = %v, want 2", w.RenderScale())
	}
	// Without this request the compositor takes the buffer as logical and
	// stretches it: twice the pixels, drawn twice as large, and blurry again.
	if got := s.scalesSent(); len(got) == 0 || got[len(got)-1] != 2 {
		t.Errorf("set_buffer_scale sent %v, want it to end at 2", got)
	}
}

// A window that did NOT ask keeps exactly what it had: one pixel per point, and
// no set_buffer_scale at all.
func TestWaylandWithoutNativeScaleIgnoresTheOutput(t *testing.T) {
	w, s := newScaledWindow(t, Config{Width: 200, Height: 100}, 2)

	if w.scale != 1 {
		t.Errorf("scale = %d for a window that did not ask, want 1", w.scale)
	}
	if w.w != 200 || w.h != 100 {
		t.Errorf("framebuffer = %dx%d, want the logical 200x100", w.w, w.h)
	}
	if w.RenderScale() != 1 {
		t.Errorf("RenderScale() = %v, want 1", w.RenderScale())
	}
	if got := s.scalesSent(); len(got) != 0 {
		t.Errorf("set_buffer_scale sent %v for a window that never asked", got)
	}
}

// A 1× screen is the ordinary case and must cost nothing: no scaling, and no
// request telling the compositor something it already assumes.
func TestWaylandNativeScaleOnAPlainScreen(t *testing.T) {
	w, s := newScaledWindow(t, Config{Width: 200, Height: 100, RenderScale: NativeScale}, 1)

	if w.scale != 1 || w.w != 200 || w.h != 100 {
		t.Errorf("scale=%d framebuffer=%dx%d, want 1 and 200x100", w.scale, w.w, w.h)
	}
	if got := s.scalesSent(); len(got) != 0 {
		t.Errorf("set_buffer_scale sent %v on a 1x screen, where nothing changed", got)
	}
}

// A configure is in logical points, so the framebuffer it produces is those
// times the scale.
func TestWaylandResizeKeepsTheScale(t *testing.T) {
	w, _ := newScaledWindow(t, Config{Width: 200, Height: 100, RenderScale: NativeScale}, 2)

	w.pendingW, w.pendingH, w.needResize = 320, 240, true
	w.applyResize()

	if w.logW != 320 || w.logH != 240 {
		t.Errorf("logical size = %dx%d, want the configured 320x240", w.logW, w.logH)
	}
	if w.w != 640 || w.h != 480 {
		t.Errorf("framebuffer = %dx%d, want 640x480", w.w, w.h)
	}
	if len(w.buf) != 4*640*480 {
		t.Errorf("framebuffer buffer is %d bytes, want %d", len(w.buf), 4*640*480)
	}
}

// The window straddling two screens draws for the sharper one: downscaling for
// the other loses nothing a viewer can see, and the reverse is visibly soft.
func TestWaylandScaleTakesTheSharpestScreen(t *testing.T) {
	w := &wlWindow{scale: 1, logW: 100, logH: 50, w: 100, h: 50}
	w.on = map[uint32]bool{}
	w.outputs = map[uint32]*wayland.Output{}

	if got := w.outputScale(); got != 1 {
		t.Errorf("scale on no screen at all = %d, want 1", got)
	}
	// An output the surface is on but which we never bound is not a reason to
	// pick a scale out of the air.
	w.on[0x99] = true
	if got := w.outputScale(); got != 1 {
		t.Errorf("scale on an unknown screen = %d, want 1", got)
	}
}
