// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

// In-process proof for the Wayland incremental present path. A wlWindow is
// brought up over the scripted fake compositor, then driven frame by frame with
// a scene.HostRoot root. The critical Wayland-specific invariant is buffer age:
// because the two wl_shm pool buffers alternate, packing only this frame's
// damage into the freshly chosen buffer would leave it a frame stale outside the
// damage; presentDamaged packs everything the buffer OWES, so after every frame
// the just-attached pool buffer holds the WHOLE correct image — asserted here by
// comparing its bytes to a full pack of the live framebuffer.
package window

import (
	"bytes"
	"testing"

	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/toolkit/scene"
	"github.com/go-widgets/window/internal/wayland"
)

// packedFull returns w's framebuffer packed as ARGB8888 at the pool stride — the
// reference contents a fully-current pool buffer must hold.
func packedFull(w *wlWindow) []byte {
	ref := make([]byte, w.stride*w.h)
	wayland.PackARGB8888(ref, w.stride, w.buf, w.w*4, w.w, w.h)
	return ref
}

// assertAttachedBufferCurrent checks the pool buffer just attached (1-cur after
// the swap) holds the complete, correct image.
func assertAttachedBufferCurrent(t *testing.T, w *wlWindow, label string) {
	t.Helper()
	idx := 1 - w.cur // presentDamaged set cur = 1 - idx_used
	off := idx * w.stride * w.h
	got := w.poolData[off : off+w.stride*w.h]
	if !bytes.Equal(got, packedFull(w)) {
		t.Fatalf("%s: attached pool buffer %d is not fully current (stale pixels outside damage)", label, idx)
	}
}

func TestWaylandIncrementalBufferAge(t *testing.T) {
	const W, H, rows, cols = 160, 120, 6, 8
	cli, srv := socketPairWin(t)
	defer srv.Close()
	km := keymapFile(t)
	defer km.Close()
	sc := &srvConn{c: srv}
	go fakeCompositor(t, sc, int(km.Fd()), W, H)

	conn := wayland.New(cli)
	w, err := newWaylandWindow(conn, Config{Title: "wl-damage", Width: W, Height: H})
	if err != nil {
		t.Fatalf("newWaylandWindow: %v", err)
	}
	defer w.Close()

	app, cells := buildGridApp(W, H, rows, cols)
	hr := scene.NewHostRoot(app)
	for _, c := range cells {
		c.hr = hr
	}
	w.root = hr
	w.dmg, _ = toolkit.Widget(hr).(DamageRenderer)
	if w.dmg == nil {
		t.Fatal("HostRoot must satisfy DamageRenderer")
	}

	// Initial full frame.
	if err := w.paintInitial(); err != nil {
		t.Fatalf("paintInitial: %v", err)
	}
	assertAttachedBufferCurrent(t, w, "initial")

	// A run of single-cell changes: every frame alternates the pool buffer, so
	// each must be brought fully current from its (older) age.
	for step, idx := range []int{0, 23, 47, 11, 5, 40, 31, 18} {
		cells[idx].on = !cells[idx].on
		hr.Invalidate(cells[idx])
		if err := w.paintFrame(); err != nil {
			t.Fatalf("step %d: paintFrame: %v", step, err)
		}
		assertAttachedBufferCurrent(t, w, "incremental")
	}

	// A frame with no invalidation presents nothing (empty damage).
	curBefore := w.cur
	if err := w.paintFrame(); err != nil {
		t.Fatalf("no-op frame: %v", err)
	}
	if w.cur != curBefore {
		t.Fatal("a no-damage frame must not present (buffer must not swap)")
	}

	// A resize recreates the pool (both buffers owe the whole surface) and the
	// full repaint that follows must still land a fully-current buffer.
	w.pendingW, w.pendingH, w.needResize = 200, 150, true
	w.applyResize()
	if err := w.paintFrame(); err != nil {
		t.Fatalf("resize frame: %v", err)
	}
	if w.w != 200 || w.h != 150 {
		t.Fatalf("resize size = %dx%d", w.w, w.h)
	}
	assertAttachedBufferCurrent(t, w, "post-resize")
}

// TestWaylandPresentDamagedBeforeConfigure covers the early-out when the surface
// is not yet configured.
func TestWaylandPresentDamagedBeforeConfigure(t *testing.T) {
	w := &wlWindow{configured: false}
	if err := w.presentDamaged([]toolkit.Rect{{X: 0, Y: 0, W: 1, H: 1}}); err != nil {
		t.Fatalf("presentDamaged before configure should be a no-op: %v", err)
	}
}
