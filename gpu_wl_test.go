// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package window

import (
	"bytes"
	"errors"
	"testing"

	"github.com/go-gfx/gpu2d"
	"github.com/go-widgets/window/internal/wayland"
)

// gpuWaylandWindow brings a wlWindow up over the scripted fake compositor.
func gpuWlUp(t *testing.T, cfg Config) *wlWindow {
	t.Helper()
	cli, srv := socketPairWin(t)
	t.Cleanup(func() { srv.Close() })
	km := keymapFile(t)
	t.Cleanup(func() { km.Close() })
	sc := &srvConn{c: srv}
	go fakeCompositor(t, sc, int(km.Fd()), cfg.Width, cfg.Height)
	w, err := newWaylandWindow(wayland.New(cli), cfg)
	if err != nil {
		t.Fatalf("newWaylandWindow: %v", err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}

// attached is the pool buffer the window committed last.
func attached(w *wlWindow) []byte {
	off := (1 - w.cur) * w.stride * w.h
	return w.poolData[off : off+w.stride*w.h]
}

// A Wayland window on the GPU commits, in its wl_shm buffer, the CPU frame
// packed as ARGB8888, before and after a resize; a frame the device refuses
// is drawn on the CPU, committed all the same, and exposed through GPUError.
func TestWaylandWindowOnTheGPUCommitsTheCPUFrame(t *testing.T) {
	r := useRef(t, 3, gpu2d.ErrTooLarge) // the third frame is refused
	w := gpuWlUp(t, Config{Title: "wl-gpu", Width: 160, Height: 120, GPU: true})
	if w.GPU() || w.GPUError() != nil {
		t.Fatalf("GPU %v, GPUError %v before Run", w.GPU(), w.GPUError())
	}
	root := &sceneRoot{}
	w.root, w.dmg = root, root
	w.gpu.Start(w.w, w.h)
	if !w.GPU() {
		t.Fatalf("not on the GPU: %v", w.GPUError())
	}

	want := func(width, height int) []byte {
		ref := make([]byte, 4*width*height)
		wayland.PackARGB8888(ref, 4*width, sceneFrame(w.theme, width, height), 4*width, width, height)
		return ref
	}

	if err := w.paintInitial(); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(attached(w), want(160, 120)) {
		t.Fatal("the committed buffer is not the CPU frame packed as ARGB8888")
	}
	if root.damaged != 0 {
		t.Fatalf("RenderDamaged called %d times while the GPU drew", root.damaged)
	}

	w.pendingW, w.pendingH, w.needResize = 200, 150, true
	w.applyResize()
	if !w.GPU() {
		t.Fatalf("the resize dropped the GPU path: %v", w.GPUError())
	}
	if err := w.paintFrame(); err != nil { // the second draw, at the new size
		t.Fatal(err)
	}
	if !w.GPU() || !bytes.Equal(attached(w), want(200, 150)) {
		t.Fatalf("the frame after the resize is not the CPU frame (GPU %v, %v)", w.GPU(), w.GPUError())
	}
	if err := w.paintFrame(); err != nil { // the third draw: refused
		t.Fatal(err)
	}
	if r.draws != 3 {
		t.Fatalf("%d draws, want 3", r.draws)
	}
	if w.GPU() || !errors.Is(w.GPUError(), gpu2d.ErrTooLarge) {
		t.Fatalf("GPU %v, GPUError %v after the refusal", w.GPU(), w.GPUError())
	}
	if !bytes.Equal(attached(w), want(200, 150)) {
		t.Fatal("the frame drawn on the CPU after the refusal is not committed whole")
	}

	// An incremental root is whole once more after the drop, then incremental.
	root.drawn, root.damaged = 0, 0
	if err := w.paintFrame(); err != nil {
		t.Fatal(err)
	}
	if root.drawn != 1 || root.damaged != 0 {
		t.Fatalf("the frame after the drop: Draw %d, RenderDamaged %d", root.drawn, root.damaged)
	}
	root.drawn, root.damaged = 0, 0
	if err := w.paintFrame(); err != nil {
		t.Fatal(err)
	}
	if root.damaged != 1 {
		t.Fatalf("RenderDamaged %d once the CPU is back to incremental frames", root.damaged)
	}
}

// Without Vulkan a Wayland window says why and draws on the CPU.
func TestWaylandWindowWhoseGPUWillNotStartDrawsOnTheCPU(t *testing.T) {
	w := gpuWlUp(t, Config{Title: "wl-nogpu", Width: 100, Height: 80, GPU: true})
	refused := errors.New("no device")
	useRefusing(t, refused)
	w.root = &sceneRoot{}
	w.gpu.Start(w.w, w.h)
	if err := w.paintInitial(); err != nil {
		t.Fatal(err)
	}
	if w.GPU() || !errors.Is(w.GPUError(), refused) {
		t.Fatalf("GPU %v, GPUError %v", w.GPU(), w.GPUError())
	}
	ref := make([]byte, 4*100*80)
	wayland.PackARGB8888(ref, 4*100, sceneFrame(w.theme, 100, 80), 4*100, 100, 80)
	if !bytes.Equal(attached(w), ref) {
		t.Fatal("the CPU frame differs")
	}
}
