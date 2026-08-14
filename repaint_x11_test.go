// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"bytes"
	"testing"

	"github.com/go-widgets/window/internal/x11"
)

// repaintMessage is the ClientMessage our own Repaint puts on the wire, as the
// server would deliver it back to us.
func repaintMessage() []byte {
	pkt := make([]byte, 32)
	pkt[0] = 33 | 0x80 // ClientMessage, with the SendEvent bit the server sets
	pkt[1] = 32        // format
	le.PutUint32(pkt[4:8], 0x333)
	le.PutUint32(pkt[8:12], atomRepaint)
	return pkt
}

// foreignMessage is somebody else's ClientMessage: a window manager protocol we
// do not implement. It must not be mistaken for a repaint.
func foreignMessage() []byte {
	pkt := make([]byte, 32)
	pkt[0] = 33
	pkt[1] = 32
	le.PutUint32(pkt[4:8], 0x333)
	le.PutUint32(pkt[8:12], 0x777)
	return pkt
}

// countSendEvents counts SendEvent requests in what the client wrote. The
// opcode byte is enough: this window sends no other event, and a request's
// length is in its header, so a scan that respects it cannot match payload.
func countSendEvents(t *testing.T, b []byte) int {
	t.Helper()
	n := 0
	for len(b) >= 4 {
		length := int(le.Uint16(b[2:4])) * 4
		if length < 4 || length > len(b) {
			return n // a truncated tail, which the handshake bytes are not
		}
		if b[0] == 25 { // X_SendEvent
			n++
		}
		b = b[length:]
	}
	return n
}

// A repaint asked for from another goroutine reaches the loop as an event,
// because a loop blocked on the next event cannot be woken by anything else.
func TestRepaintWakesTheLoop(t *testing.T) {
	w, _ := dialFake(t, Config{Width: 40, Height: 30}, repaintMessage(), deleteMessage())
	root := &recWidget{}

	if err := w.Run(root); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// The first frame plus the one the wakeup asked for.
	if root.drawn != 2 {
		t.Errorf("the root was drawn %d times, want 2 (the first frame and the wakeup's)", root.drawn)
	}
	// A wakeup carries no input, so nothing reaches the widget tree.
	if len(root.events) != 0 {
		t.Errorf("the wakeup was delivered to the widget as %+v", root.events)
	}
}

// Somebody else's ClientMessage is not a repaint and not a close.
func TestForeignClientMessageIsIgnored(t *testing.T) {
	w, _ := dialFake(t, Config{Width: 40, Height: 30}, foreignMessage(), deleteMessage())
	root := &recWidget{}

	if err := w.Run(root); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if root.drawn != 1 {
		t.Errorf("the root was drawn %d times, want 1 (only the first frame)", root.drawn)
	}
}

// One wakeup in flight at a time.
//
// A caller repainting at 60 Hz against a loop busy with a slow frame would
// otherwise queue a message per tick, and spend its recovery redrawing once per
// queued message — frames nobody will ever see.
func TestRepaintCoalesces(t *testing.T) {
	w, ft := dialFake(t, Config{Width: 40, Height: 30})
	ft.out.Reset() // the bring-up requests are not what this is counting

	w.Repaint()
	w.Repaint()
	w.Repaint()
	if n := countSendEvents(t, ft.out.Bytes()); n != 1 {
		t.Fatalf("three Repaints put %d wakeups on the wire, want 1", n)
	}

	// Once the loop has taken the message, the next Repaint sends another: the
	// flag is a coalescer, not a latch that stops after the first frame.
	if !w.tookRepaint(w.repaintAtom, 32) {
		t.Fatal("the loop did not recognise our own wakeup")
	}
	w.Repaint()
	if n := countSendEvents(t, ft.out.Bytes()); n != 2 {
		t.Errorf("after the loop took the first, a second Repaint put %d total on the wire, want 2", n)
	}
}

// tookRepaint recognises our wakeup and nothing else. Getting this wrong either
// swallows a window manager's message or never clears the coalescing flag,
// which would leave the window frozen after exactly one repaint.
func TestTookRepaint(t *testing.T) {
	w, _ := dialFake(t, Config{Width: 40, Height: 30})

	for _, tc := range []struct {
		name   string
		atom   uint32
		format byte
		want   bool
	}{
		{"ours", atomRepaint, 32, true},
		{"another type", 0x777, 32, false},
		{"our type at the wrong format", atomRepaint, 8, false},
	} {
		w.repaint.pending.Store(true)
		if got := w.tookRepaint(tc.atom, tc.format); got != tc.want {
			t.Errorf("%s: tookRepaint = %v, want %v", tc.name, got, tc.want)
		}
		// The flag is cleared exactly when the message was ours. Clearing it for
		// somebody else's would let one lost wakeup silence the window; not
		// clearing it for ours would silence it after the very first frame.
		if pending := w.repaint.pending.Load(); pending == tc.want {
			t.Errorf("%s: pending = %v after a message that was ours = %v", tc.name, pending, tc.want)
		}
	}
}

// A server that would not intern the atom leaves the capability inert rather
// than the window broken: the application repaints on input, as it did before
// the capability existed.
func TestRepaintWithoutAnAtom(t *testing.T) {
	w, ft := dialFake(t, Config{Width: 40, Height: 30})
	w.repaintAtom = 0
	ft.out.Reset()

	w.Repaint()
	if n := countSendEvents(t, ft.out.Bytes()); n != 0 {
		t.Errorf("a window with no atom put %d wakeups on the wire", n)
	}
	if w.repaint.pending.Load() {
		t.Error("a Repaint that sent nothing left a wakeup marked as in flight")
	}
	if w.tookRepaint(0, 32) {
		t.Error("a window with no atom claimed a ClientMessage of type 0 as its own")
	}
}

// A write that fails must not leave the flag set: a stuck flag would silence
// every later repaint, long after the connection had recovered.
func TestRepaintWriteFailureClearsTheFlag(t *testing.T) {
	fw := &failWriter{in: bytes.NewReader(serverScript())}
	conn, err := x11.Handshake(fw, le, "", nil)
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	w, err := newWindow(conn, Config{Width: 40, Height: 30})
	if err != nil {
		t.Fatalf("newWindow: %v", err)
	}
	fw.fail = true

	w.Repaint()
	if w.repaint.pending.Load() {
		t.Error("a failed wakeup stayed marked as in flight, silencing every later repaint")
	}
}
