// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build integration && linux

// The live Wayland clipboard proof, against a real compositor (headless sway in
// CI). It runs only under -tags=integration with WINDOW_WAYLAND_INTEGRATION set.
//
// A clipboard exchange needs two parties and it gets two REAL ones: two
// toplevels on two separate connections, which sway sees as two unrelated
// clients. One copies, the other pastes, and the text travels the whole
// mechanism — set_selection to the compositor, a data offer back out to the
// other client, and the bytes themselves through a pipe whose two ends are held
// by different connections. Nothing here is stubbed: a window checked against
// itself never goes near the protocol and would prove nothing.
//
// The name begins with TestLiveWayland because that is what the CI lane filters
// on. A test outside that filter never runs and only looks like proof.
package window

import (
	"os"
	"strings"
	"testing"
	"time"
)

// Reading the selection is a privilege of the KEYBOARD-FOCUSED client on a
// wlroots compositor: the offer is sent to whoever has focus, and to a client
// that gains it. So the reader is opened second and mapped, which is what makes
// sway focus it — an unmapped surface is not a view and is never focused, and a
// reader that never gained focus would fail here for a reason that has nothing to
// do with the clipboard.
func TestLiveWaylandClipboardCrossesTwoClients(t *testing.T) {
	if os.Getenv("WINDOW_WAYLAND_INTEGRATION") == "" {
		t.Skip("set WINDOW_WAYLAND_INTEGRATION=1 (under a Wayland compositor) to enable")
	}
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Fatal("WAYLAND_DISPLAY is not set")
	}
	const text = "presse-papiers traversé par le compositeur"

	// --- The writer. ---------------------------------------------------------
	writer := openLiveWayland(t, "gw-clip-writer")
	if _, ok := writer.clipboard(); !ok {
		t.Fatal("this compositor advertises no wl_data_device_manager, so the lane cannot prove the clipboard")
	}
	writer.SetClipboardText(text)
	if err := writer.conn.Roundtrip(); err != nil {
		t.Fatalf("the writer could not hand its source over: %v", err)
	}
	if !writer.clipOwned {
		t.Fatal("the writer does not believe it owns the selection it just set")
	}

	// From here the writer belongs to its own goroutine, which is the only thing
	// that touches it: a source is asked for its bytes through an EVENT, so a
	// client that stops dispatching stops being able to answer a paste. This is
	// the application's event loop, reduced to the part that matters.
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		for {
			if err := writer.conn.Dispatch(); err != nil {
				return // the connection went away with the test
			}
		}
	}()

	// --- The reader. ---------------------------------------------------------
	reader := openLiveWayland(t, "gw-clip-reader")

	type paste struct {
		text  string
		mimes []string
		err   string
	}
	got := make(chan paste, 1)
	go func() {
		// Everything the reader touches happens here -- painting, acking and
		// binding its data device -- so nothing about it is shared with the test
		// goroutine. This is the run loop, reduced to what the proof needs.
		reader.root = &patternRoot{}
		if _, ok := reader.clipboard(); !ok {
			got <- paste{err: "the reader's compositor advertises no wl_data_device_manager"}
			return
		}
		// A surface with no buffer is not a view and is never focused, and the
		// compositor sends the selection to the focused client. So the reader must
		// present before it can read anything at all.
		if err := reader.paintFrame(); err != nil {
			got <- paste{err: "the reader could not present a frame: " + err.Error()}
			return
		}
		for {
			if err := reader.conn.Dispatch(); err != nil {
				return // the connection went away with the test
			}
			if err := reader.flushAck(); err != nil {
				return
			}
			if reader.needResize {
				reader.applyResize()
			}
			if reader.repaint {
				reader.repaint = false
				if err := reader.paintFrame(); err != nil {
					return
				}
			}
			off := reader.dataDev.Selection()
			if off == nil {
				continue // not focused yet, or nothing on the clipboard yet
			}
			got <- paste{text: reader.ClipboardText(), mimes: off.Mimes()}
			return
		}
	}()

	select {
	case p := <-got:
		if p.err != "" {
			t.Fatal(p.err)
		}
		if p.text != text {
			t.Errorf("the other client pasted %q, want %q", p.text, text)
		} else {
			t.Logf("live clipboard: %q crossed two connections through the compositor", p.text)
		}
		// The types came back out of the compositor in the order they were
		// offered, which is the order of preference. Getting this wrong is how a
		// paster ends up asking for a type nobody promised.
		want := []string{clipMimeUTF8, clipMimePlain}
		if strings.Join(p.mimes, ",") != strings.Join(want, ",") {
			t.Errorf("the offer advertises %v, want %v", p.mimes, want)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the compositor never announced our selection to the other client")
	}

	if err := reader.Close(); err != nil {
		t.Logf("closing the reader: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Logf("closing the writer: %v", err)
	}
	select {
	case <-writerDone:
	case <-time.After(2 * time.Second):
		t.Log("the writer's dispatch loop did not exit promptly")
	}
}

// openLiveWayland opens one real toplevel and insists it is the Wayland
// back-end: under WAYLAND_DISPLAY it must be, and silently proving the X11
// clipboard here would be worse than failing.
func openLiveWayland(t *testing.T, title string) *wlWindow {
	t.Helper()
	b, err := Open(Config{Title: title, Class: "gwwltest", Width: 160, Height: 120})
	if err != nil {
		t.Fatalf("opening %s: %v", title, err)
	}
	w, ok := b.(*wlWindow)
	if !ok {
		_ = b.Close()
		t.Fatalf("Open selected %T for %s, want the Wayland backend", b, title)
	}
	return w
}

// The Wayland back-end's half of AppearanceReader, over a REAL session bus.
//
// The reading is shared with the X11 back-end, which is the point: the desktop's
// look is not a property of the display server carrying the pixels. What a
// forwarding mistake would look like is a Wayland window answering from a portal
// state that is never consulted -- always no-preference, always light, under a
// dark desktop -- and only a bus that really answers can tell the two apart. No
// compositor is involved on purpose: this asks a bus, not a display server.
func TestLiveWaylandPortalAppearanceOverARealBus(t *testing.T) {
	if os.Getenv("WINDOW_WAYLAND_INTEGRATION") == "" {
		t.Skip("set WINDOW_WAYLAND_INTEGRATION=1 to run the live portal proof")
	}
	conn := sessionBus(t)
	servePortal(t, conn, 1) // 1 = prefer dark

	var w AppearanceReader = &wlWindow{} // the capability, through the interface
	ap := w.Appearance()

	if !ap.Dark {
		t.Error("the portal said prefer-dark and the wayland back-end came back light")
	}
	if !ap.HasAccent {
		t.Fatal("the portal published an accent and the wayland back-end found none")
	}
	if ap.Accent.R != 255 || ap.Accent.G != 128 || ap.Accent.B != 0 || ap.Accent.A != 255 {
		t.Errorf("accent = %+v, want opaque 255,128,0", ap.Accent)
	}
	if _, err := w.SystemFontTTF(); err == nil {
		t.Error("a Linux desktop reported a system font file")
	}
	t.Logf("over a real bus: dark=%v accent=#%02X%02X%02X", ap.Dark, ap.Accent.R, ap.Accent.G, ap.Accent.B)
}
