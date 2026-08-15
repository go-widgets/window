// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"bytes"
	"testing"

	"github.com/go-widgets/window/internal/x11"
)

// The X resource database is a text format written by somebody else's desktop,
// so every reading of it is a parsing question first.
func TestXftDPI(t *testing.T) {
	for _, tc := range []struct {
		name string
		db   string
		want float64
		ok   bool
	}{
		{"the ordinary case", "Xft.dpi:\t192\n", 192, true},
		{"among its neighbours",
			"*background:\t#000000\nXft.antialias:\t1\nXft.dpi:\t144\nXft.hinting:\t1\n", 144, true},
		{"spaces rather than a tab", "Xft.dpi:   96  \n", 96, true},
		{"fractional, as a desktop may well write it", "Xft.dpi:\t120.5\n", 120.5, true},
		{"no such resource", "Xft.antialias:\t1\n", 0, false},
		{"an empty database", "", 0, false},
		// A prefix match would read the wrong resource; these two exist and mean
		// entirely different things.
		{"a longer name that starts the same", "Xft.dpiScale:\t2\n", 0, false},
		{"a wildcard form we do not claim to handle", "*.Xft.dpi:\t192\n", 0, false},
		{"a value that is not a number", "Xft.dpi:\tlarge\n", 0, false},
		{"a value that is not positive", "Xft.dpi:\t0\n", 0, false},
		{"a line with no colon at all", "Xft.dpi 192\n", 0, false},
	} {
		got, ok := xftDPI(tc.db)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("%s: xftDPI(%q) = %v,%v want %v,%v", tc.name, tc.db, got, ok, tc.want, tc.ok)
		}
	}
}

// A fractional framebuffer is not what this back-end draws, so the dpi rounds
// to whole pixels per point -- the same arithmetic GTK does with the same
// number.
func TestScaleFromDPI(t *testing.T) {
	for _, tc := range []struct {
		dpi  float64
		want int
	}{
		{96, 1},
		{120, 1}, // 1.25 rounds down
		{144, 2}, // 1.5 rounds up, as GTK does
		{192, 2},
		{288, 3},
		{384, 4},
		{0.5, 1},  // below anything real: 1:1 rather than nothing
		{-100, 1}, // a value from another process, not to be trusted
		{9600, 4}, // a stray zero is an allocation failure, not a crisp window
	} {
		if got := scaleFromDPI(tc.dpi); got != tc.want {
			t.Errorf("scaleFromDPI(%v) = %d, want %d", tc.dpi, got, tc.want)
		}
	}
}

// Asking for the screen's own resolution makes the WINDOW bigger in pixels:
// there is no compositor on X11 to scale anything, so this is the whole
// mechanism.
func TestX11NativeScaleMakesABiggerFramebuffer(t *testing.T) {
	w, _ := dialFakeWithResources(t, Config{Width: 200, Height: 150, RenderScale: NativeScale},
		"Xft.dpi:\t192\n")

	if w.scale != 2 {
		t.Fatalf("scale = %d, want 2 for a 192 dpi desktop", w.scale)
	}
	if w.w != 400 || w.h != 300 {
		t.Errorf("framebuffer = %dx%d, want 400x300", w.w, w.h)
	}
	if len(w.buf) != 4*400*300 {
		t.Errorf("framebuffer buffer is %d bytes, want %d", len(w.buf), 4*400*300)
	}
	if gw, gh := w.Size(); gw != 400 || gh != 300 {
		t.Errorf("Size() = %dx%d, want the framebuffer 400x300", gw, gh)
	}
	if w.RenderScale() != 2 {
		t.Errorf("RenderScale() = %v, want 2", w.RenderScale())
	}
}

// A caller that did not ask keeps the window it asked for, whatever the desktop
// says about its screen.
func TestX11WithoutNativeScaleIgnoresTheDesktop(t *testing.T) {
	w, _ := dialFakeWithResources(t, Config{Width: 200, Height: 150}, "Xft.dpi:\t192\n")

	if w.scale != 1 || w.w != 200 || w.h != 150 {
		t.Errorf("scale=%d framebuffer=%dx%d, want 1 and 200x150", w.scale, w.w, w.h)
	}
	if w.RenderScale() != 1 {
		t.Errorf("RenderScale() = %v, want 1", w.RenderScale())
	}
}

// A desktop that publishes nothing has decided it is 96 dpi, which is the
// overwhelming majority of X11 sessions and must cost nothing.
func TestX11NativeScaleWithNoResources(t *testing.T) {
	w, _ := dialFakeWithResources(t, Config{Width: 200, Height: 150, RenderScale: NativeScale}, "")

	if w.scale != 1 || w.w != 200 || w.h != 150 {
		t.Errorf("scale=%d framebuffer=%dx%d, want 1 and 200x150", w.scale, w.w, w.h)
	}
}

// The zero Window is 1:1, so a RenderScale read off one before bring-up cannot
// report a scale of nothing.
func TestX11ZeroWindowRenderScale(t *testing.T) {
	if got := (&Window{}).RenderScale(); got != 1 {
		t.Errorf("the zero Window reports RenderScale %v, want 1", got)
	}
}

// A server that refuses the property read, or answers with something that is
// not a resource database, leaves the window at 1:1 rather than at a scale
// invented from an error.
func TestX11NativeScaleWhenTheServerWillNotSay(t *testing.T) {
	// The atom exists but the read fails: the script runs out after the intern,
	// so the reply never comes.
	script := setupReply()
	script = append(script, internReply(atomResourceManager)...)
	ft := &fakeTransport{in: bytes.NewReader(script)}
	conn, err := x11.Handshake(ft, le, "", nil)
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	if got := desktopScale(conn, 0x333); got != 1 {
		t.Errorf("scale from a failed property read = %d, want 1", got)
	}

	// The property is there and says nothing about dpi.
	w, _ := dialFakeWithResources(t, Config{Width: 120, Height: 90, RenderScale: NativeScale},
		"Xft.antialias:\t1\n")
	if w.scale != 1 || w.w != 120 || w.h != 90 {
		t.Errorf("scale=%d framebuffer=%dx%d for a database with no dpi, want 1 and 120x90",
			w.scale, w.w, w.h)
	}
}
