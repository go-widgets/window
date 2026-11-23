// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux && !android

package gtk

import (
	"bytes"
	"os"
	"testing"

	"github.com/go-gtk/gtk4"
	"github.com/go-widgets/toolkit/scene"
)

// TestGTKDamagedFramesReadBackAsAFullRepaint drives the real window: after every
// frame the GtkPicture, read back, equals a damage-unaware full repaint. On a
// libgtk-4 older than 4.16 the whole upload carries every frame; on 4.16+ the
// damaged frames go through the texture update (GTK_REQUIRE_UPDATE=1 makes a
// runtime without it a failure, for the lane that must prove that path).
func TestGTKDamagedFramesReadBackAsAFullRepaint(t *testing.T) {
	win, err := Open("gtk damage test", gw, gh, nil, 1)
	if err != nil {
		t.Skipf("no GTK display: %v", err)
	}
	defer win.Close()
	t.Logf("libgtk-4 minor %d, texture update: %v", gtk4.MinorVersion(), gtk4.TextureUpdateSupported())
	if os.Getenv("GTK_REQUIRE_UPDATE") != "" && !gtk4.TextureUpdateSupported() {
		t.Fatalf("GTK_REQUIRE_UPDATE is set but libgtk-4 4.%d lacks the update API", gtk4.MinorVersion())
	}

	app, cells := grid()
	hr := scene.NewHostRoot(app)
	win.root = hr
	win.fr.bind(hr)

	got := make([]byte, gw*gh*4)
	check := func(label string) {
		t.Helper()
		if !win.pic.ReadRGBA(got, gw, gh) {
			t.Fatalf("%s: the picture shows no texture of %dx%d", label, gw, gh)
		}
		if !bytes.Equal(got, reference(app, win.theme)) {
			t.Fatalf("%s: the picture differs from a full repaint", label)
		}
	}

	win.frame()
	check("first frame")
	for step, idx := range []int{0, 37, 5, 79, 44, 12, 63, 20, 0, 51} {
		cells[idx].on = !cells[idx].on
		hr.Invalidate(cells[idx])
		win.frame()
		check("step " + string(rune('0'+step)))
	}

	// The window shares its framebuffer with GTK, so a partial present copies no
	// byte (the copying path moved all 128000 of them per frame).
	if win.sf == nil {
		t.Fatal("the window has no shared frame although the GTK libraries loaded")
	}
	cells[7].on = !cells[7].on
	hr.Invalidate(cells[7])
	res := win.fr.present(win.root, win.theme, win.buf, win.w, win.h, pictureTarget{win.pic, win.sf})
	if res.Copied != 0 {
		t.Fatalf("a present copied %d bytes, want 0", res.Copied)
	}
	if gtk4.TextureUpdateSupported() && res.Whole {
		t.Fatal("a one-cell change was presented whole although the update API is present")
	}
	check("shared partial present")
}

// TestGTKResizeReallocatesAndResumesDamage: after the framebuffer is reallocated
// (what the frame clock does when the window's allocation changed) the next frame
// presents whole at the new size and reads back as a full repaint there, the old
// frame is released, and partial presents resume.
func TestGTKResizeReallocatesAndResumesDamage(t *testing.T) {
	win, err := Open("gtk resize test", gw, gh, nil, 1)
	if err != nil {
		t.Skipf("no GTK display: %v", err)
	}
	defer win.Close()
	app, cells := grid()
	hr := scene.NewHostRoot(app)
	win.root = hr
	win.fr.bind(hr)
	win.frame()
	first := win.sf

	win.realloc(gh, gw) // transposed: the same byte count, a different frame
	if win.sf == nil || win.sf == first || win.w != gh || win.h != gw || len(win.buf) != gh*gw*4 {
		t.Fatalf("after realloc: sf=%v (was %v) size %dx%d len %d", win.sf, first, win.w, win.h, len(win.buf))
	}
	check := func(label string) {
		t.Helper()
		got := make([]byte, gh*gw*4)
		if !win.pic.ReadRGBA(got, gh, gw) {
			t.Fatalf("%s: the picture shows no texture of %dx%d", label, gh, gw)
		}
		if !bytes.Equal(got, refAt(app, win.theme, gh, gw)) {
			t.Fatalf("%s: the picture differs from a full repaint at the new size", label)
		}
	}
	res := win.fr.present(win.root, win.theme, win.buf, win.w, win.h, pictureTarget{win.pic, win.sf})
	if !res.Whole || res.Copied != 0 {
		t.Fatalf("first frame after the resize: %+v, want whole and 0 bytes copied", res)
	}
	check("whole present after the resize")

	cells[9].on = !cells[9].on
	hr.Invalidate(cells[9])
	res = win.fr.present(win.root, win.theme, win.buf, win.w, win.h, pictureTarget{win.pic, win.sf})
	if gtk4.TextureUpdateSupported() && res.Whole {
		t.Fatal("damage did not resume after the resize")
	}
	check("partial present after the resize")
}
