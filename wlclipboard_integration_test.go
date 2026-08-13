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
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-widgets/window/internal/wayland"
)

// Two facts about a real wlroots compositor shape this whole test, and the first
// run found them both by failing.
//
// A COPY needs a real input serial. wlroots rejects set_selection whose serial
// does not postdate the seat's last selection, and an untouched client's serial
// is zero, so zero is refused — silently, since the request has no reply. A
// client that has never been interacted with therefore cannot take the
// clipboard, which is a deliberate anti-hijacking rule and not a bug to work
// around. So the writer is given a real pointer button through a virtual pointer
// on a THIRD connection, and copies only once its seat has recorded a serial.
//
// A PASTE is a privilege of the keyboard-FOCUSED client: the offer is sent to
// whoever has focus and to whoever gains it. So the reader is opened second and
// presents a frame, because an unmapped surface is not a view and is never
// focused — a reader that never gained focus would fail here for a reason that
// has nothing to do with the clipboard.
func TestLiveWaylandClipboardCrossesTwoClients(t *testing.T) {
	if os.Getenv("WINDOW_WAYLAND_INTEGRATION") == "" {
		t.Skip("set WINDOW_WAYLAND_INTEGRATION=1 (under a Wayland compositor) to enable")
	}
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Fatal("WAYLAND_DISPLAY is not set")
	}
	const text = "presse-papiers traversé par le compositeur"

	// Both clients report what happened to them here. A compositor that refuses a
	// request answers with a protocol error and nothing else, so a loop that just
	// returned on a dispatch error would turn the one message that explains the
	// failure into twenty seconds of silence.
	diag := make(chan string, 32)
	say := func(format string, args ...any) {
		select {
		case diag <- fmt.Sprintf(format, args...):
		default:
		}
	}

	// --- The writer, which owns itself from the first line. ------------------
	//
	// Everything it touches happens on its own goroutine, including the copy: a
	// source is asked for its bytes through an EVENT, so a client that stops
	// dispatching stops being able to answer a paste. This is the application's
	// event loop, reduced to what the proof needs.
	writer := openLiveWayland(t, "gw-clip-writer")
	copied := make(chan string, 1) // "" once it has copied, or the reason it could not
	writerDone := make(chan struct{})
	go func() {
		defer close(writerDone)
		writer.root = &patternRoot{}
		if _, ok := writer.clipboard(); !ok {
			copied <- "this compositor advertises no wl_data_device_manager, so the lane cannot prove the clipboard"
			return
		}
		if err := writer.paintFrame(); err != nil {
			copied <- "the writer could not present a frame: " + err.Error()
			return
		}
		done, echoed := false, false
		for {
			if err := writer.conn.Dispatch(); err != nil {
				say("the writer's connection ended: %v", err)
				return
			}
			if err := writer.flushAck(); err != nil {
				say("the writer could not ack a configure: %v", err)
				return
			}
			if writer.needResize {
				writer.applyResize()
			}
			if writer.repaint {
				writer.repaint = false
				if err := writer.paintFrame(); err != nil {
					say("the writer could not repaint: %v", err)
					return
				}
			}
			// The compositor announces the selection to the focused client, which
			// while the writer is alone IS the writer -- so its own device seeing an
			// offer appear is the compositor saying it accepted the copy. Nothing is
			// read from it: asking ourselves is the deadlock this design avoids.
			if done && !echoed && writer.dataDev.Selection() != nil {
				echoed = true
				say("the compositor announced our own selection back to us, so it accepted the copy")
			}
			if done || writer.seat == nil || writer.seat.LastSerial() == 0 {
				continue // not yet touched by the user, so not yet allowed to copy
			}
			writer.SetClipboardText(text)
			if !writer.clipOwned {
				copied <- "the writer does not believe it owns the selection it just set"
				return
			}
			done = true
			say("the writer copied quoting serial %d", writer.seat.LastSerial())
			copied <- ""
		}
	}()

	// The pointer button that earns the writer its serial. It is injected until
	// the writer reports having copied: the device is persistent, so a button
	// sent before the window bound its pointer is simply superseded by the next.
	injectUntilCopied(t, copied)

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
				say("the reader's connection ended: %v", err)
				return
			}
			if err := reader.flushAck(); err != nil {
				say("the reader could not ack a configure: %v", err)
				return
			}
			if reader.needResize {
				reader.applyResize()
			}
			if reader.repaint {
				reader.repaint = false
				if err := reader.paintFrame(); err != nil {
					say("the reader could not repaint: %v", err)
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
		reportDiagnostics(t, diag)
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
	reportDiagnostics(t, diag)
}

// reportDiagnostics prints what the two clients had to say, on the way past.
// Drained rather than closed, so it can be read once on failure and again at the
// end without either read losing the other's lines.
func reportDiagnostics(t *testing.T, diag <-chan string) {
	t.Helper()
	for {
		select {
		case line := <-diag:
			t.Logf("live clipboard: %s", line)
		default:
			return
		}
	}
}

// injectUntilCopied attaches a virtual pointer on its own connection and clicks
// until the writer reports what happened, then reports its verdict.
//
// The pointer is what makes an untouched client eligible for the clipboard at
// all. Attaching one also makes the otherwise device-less headless seat advertise
// a pointer, which the window picks up through the ordinary hot-plug path, so the
// serial arrives the same way a user's click would.
func injectUntilCopied(t *testing.T, copied <-chan string) {
	t.Helper()
	inj, err := dialCompositor()
	if err != nil {
		t.Fatalf("dial compositor (injector): %v", err)
	}
	defer inj.Close()
	reg, err := inj.Display().GetRegistry()
	if err != nil {
		t.Fatalf("injector get_registry: %v", err)
	}
	if err := inj.Roundtrip(); err != nil {
		t.Fatalf("injector roundtrip: %v", err)
	}
	for _, iface := range []string{"zwlr_virtual_pointer_manager_v1", "zwp_virtual_keyboard_manager_v1"} {
		if _, ok := reg.Find(iface); !ok {
			t.Fatalf("this compositor has no %s, so no client here can ever be granted the "+
				"clipboard: wlroots refuses a set_selection quoting serial 0", iface)
		}
	}
	seat, err := reg.Seat()
	if err != nil {
		t.Fatalf("injector seat: %v", err)
	}
	vpm, err := reg.VirtualPointerManager()
	if err != nil {
		t.Fatalf("virtual pointer manager: %v", err)
	}
	vkm, err := reg.VirtualKeyboardManager()
	if err != nil {
		t.Fatalf("virtual keyboard manager: %v", err)
	}
	if err := inj.Roundtrip(); err != nil {
		t.Fatalf("injector roundtrip: %v", err)
	}
	ptr, err := vpm.CreatePointer(seat)
	if err != nil {
		t.Fatalf("create virtual pointer: %v", err)
	}
	defer ptr.Destroy()
	// A keyboard as well as a pointer, because the seat's KEYBOARD focus is what
	// decides who is told about a selection, and a seat with no keyboard at all
	// has nobody to tell.
	kbd, err := vkm.CreateKeyboard(seat)
	if err != nil {
		t.Fatalf("create virtual keyboard: %v", err)
	}
	defer kbd.Destroy()
	fd, size := usKeymapFD(t)
	if err := kbd.Keymap(wayland.KeymapFormatXkbV1, fd, size); err != nil {
		t.Fatalf("upload keymap: %v", err)
	}
	if err := inj.Roundtrip(); err != nil {
		t.Fatalf("injector roundtrip after device setup: %v", err)
	}

	const evKeyA = 30 // Linux KEY_A (evdev keycode)
	tms := uint32(1)
	deadline := time.Now().Add(15 * time.Second)
	for {
		_ = kbd.Key(tms, evKeyA, wayland.StatePressed)
		_ = kbd.Key(tms+1, evKeyA, wayland.StateReleased)
		_ = ptr.MotionAbsolute(tms+2, 100, 100, 800, 600)
		_ = ptr.Frame()
		_ = ptr.Button(tms+3, wayland.BtnLeft, wayland.StatePressed)
		_ = ptr.Frame()
		_ = ptr.Button(tms+4, wayland.BtnLeft, wayland.StateReleased)
		_ = ptr.Frame()
		tms += 10
		if err := inj.Roundtrip(); err != nil {
			t.Fatalf("injector roundtrip during injection: %v", err)
		}
		select {
		case why := <-copied:
			if why != "" {
				t.Fatal(why)
			}
			t.Log("live clipboard: the writer took the selection quoting a real input serial")
			return
		case <-time.After(200 * time.Millisecond):
		}
		if time.Now().After(deadline) {
			t.Fatal("the writer never received an input serial, so it was never allowed to copy")
		}
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
