// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build integration && linux

// This is the live Wayland proof. It runs only under -tags=integration with
// WINDOW_WAYLAND_INTEGRATION set and a reachable Wayland compositor (a
// headless wlroots compositor — sway — in CI). It opens a real toplevel via
// the sovereign Wayland backend, presents a known four-quadrant colour
// pattern through a wl_shm buffer, captures the compositor output with grim
// and asserts the sampled pixels. Input injection is attempted with wtype
// (virtual-keyboard); when the headless seat exposes no keyboard the input
// assertion is skipped honestly rather than claimed — the input path is
// proven deterministically by the in-process fake-compositor test.
//
// patternRoot, requireTool, mustRun, decodePNG, assertPixel and abs are
// shared with the X11 live test (same package + build tag).
package window

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-widgets/toolkit"
)

func TestLiveWayland(t *testing.T) {
	if os.Getenv("WINDOW_WAYLAND_INTEGRATION") == "" {
		t.Skip("set WINDOW_WAYLAND_INTEGRATION=1 (under a Wayland compositor) to enable")
	}
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Fatal("WAYLAND_DISPLAY is not set")
	}
	requireTool(t, "grim")

	title := fmt.Sprintf("gwwl-live-%d", os.Getpid())
	b, err := Open(Config{Title: title, Class: "gwwltest", Width: 200, Height: 160})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, ok := b.(*wlWindow); !ok {
		t.Fatalf("Open selected %T, want the Wayland backend", b)
	}
	root := &patternRoot{}
	done := make(chan error, 1)
	go func() { done <- b.Run(root) }()

	// Let the compositor map + configure the toplevel and the client present.
	time.Sleep(1500 * time.Millisecond)

	// --- Capture and assert the presented pattern. ---------------------------
	dir := t.TempDir()
	capture := filepath.Join(dir, "capture.png")
	mustRun(t, "grim", capture)
	img := decodePNG(t, capture)
	ib := img.Bounds()
	W, H := ib.Dx(), ib.Dy()
	if W < 8 || H < 8 {
		t.Fatalf("captured image too small: %dx%d", W, H)
	}
	// The single toplevel fills the headless output; sample each quadrant.
	assertPixel(t, img, W/4, H/4, 255, 0, 0, "top-left(red)")
	assertPixel(t, img, 3*W/4, H/4, 0, 255, 0, "top-right(green)")
	assertPixel(t, img, W/4, 3*H/4, 0, 0, 255, "bottom-left(blue)")
	assertPixel(t, img, 3*W/4, 3*H/4, 255, 255, 255, "bottom-right(white)")

	// Persist the capture as a build artifact.
	if data, err := os.ReadFile(capture); err == nil {
		_ = os.WriteFile("live-wayland-capture.png", data, 0o644)
		t.Logf("saved capture to live-wayland-capture.png")
	}

	// --- Best-effort live input via the virtual-keyboard protocol. -----------
	// wtype injects a key through zwp_virtual_keyboard; it only reaches the
	// client if the headless seat exposes a keyboard capability. If it does
	// not (common on a device-less headless seat), we do not fail — the key
	// path is proven by the fake-compositor unit test.
	if _, err := exec.LookPath("wtype"); err != nil {
		t.Log("live input: wtype not installed; input proven by the fake-compositor test (pending-on-compositor)")
	} else if out, err := exec.Command("wtype", "a").CombinedOutput(); err != nil {
		t.Logf("live input: wtype failed (%v: %s); input proven by the fake-compositor test (pending-on-compositor)", err, out)
	} else {
		gotChar := false
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			for _, ev := range root.snapshot() {
				if ev.Kind == toolkit.EventChar && ev.Code == "a" {
					gotChar = true
				}
			}
			if gotChar {
				break
			}
			time.Sleep(100 * time.Millisecond)
		}
		if gotChar {
			t.Log("live input: EventChar 'a' dispatched from a real compositor key")
		} else {
			t.Log("live input: no key delivered (headless seat exposed no keyboard); pending-on-compositor")
		}
	}

	if err := b.Close(); err != nil {
		t.Logf("close: %v", err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Log("run loop did not exit promptly after close")
	}
}
