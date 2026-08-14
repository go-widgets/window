// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package wayland

import (
	"errors"
	"strings"
	"testing"
	"time"
)

// The interruption itself: a dispatch blocked on a silent socket returns when
// somebody else asks it to, and returns ErrWoken rather than something a caller
// would take for a broken connection.
func TestWakeInterruptsABlockedDispatch(t *testing.T) {
	c, _ := newTestConn(t, NativeOrder) // a server that says nothing: an idle compositor

	got := make(chan error, 1)
	go func() { got <- c.Dispatch() }()

	select {
	case err := <-got:
		t.Fatalf("Dispatch returned %v before anybody woke it", err)
	case <-time.After(200 * time.Millisecond):
	}

	if err := c.Wake(); err != nil {
		t.Fatalf("Wake: %v", err)
	}
	select {
	case err := <-got:
		if !errors.Is(err, ErrWoken) {
			t.Fatalf("Dispatch returned %v, want ErrWoken", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Dispatch stayed blocked after Wake")
	}
}

// After a wake the connection is a working connection again.
//
// This is the half a read deadline gets wrong when it is not cleared: every
// later read would fail instantly, turning the event loop into a spin that
// never delivers another event.
func TestWakeLeavesTheConnectionUsable(t *testing.T) {
	c, fs := newTestConn(t, NativeOrder)

	go func() { _ = c.Wake() }()
	if err := c.Dispatch(); !errors.Is(err, ErrWoken) {
		t.Fatalf("Dispatch = %v, want ErrWoken", err)
	}

	// A real event now, on the same connection: wl_display.error, which needs no
	// object of ours and surfaces as the returned error.
	got := make(chan error, 1)
	go func() { got <- c.Dispatch() }()
	select {
	case err := <-got:
		t.Fatalf("Dispatch returned %v with nothing sent -- the deadline was left armed", err)
	case <-time.After(200 * time.Millisecond):
	}

	e := newEncoder(NativeOrder)
	e.putU32(displayID) // offending object
	e.putU32(7)         // error code
	e.putString("after the wake")
	if err := fs.sendEvt(displayID, 0, e.buf); err != nil {
		t.Fatalf("send: %v", err)
	}
	select {
	case err := <-got:
		if err == nil || !strings.Contains(err.Error(), "after the wake") {
			t.Fatalf("Dispatch = %v, want the event the compositor actually sent", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the connection never delivered another event after being woken")
	}
}

// One wakeup at a time, and the next one still works.
func TestWakeCoalesces(t *testing.T) {
	c, _ := newTestConn(t, NativeOrder)

	for i := 0; i < 5; i++ {
		if err := c.Wake(); err != nil {
			t.Fatalf("Wake %d: %v", i, err)
		}
	}
	if err := c.Dispatch(); !errors.Is(err, ErrWoken) {
		t.Fatalf("Dispatch = %v, want ErrWoken", err)
	}

	// Five wakes, one interruption: the loop paints once and carries on. Had the
	// flag not been consumed, this next Dispatch would return ErrWoken again and
	// the loop would spin through four frames nobody asked for.
	done := make(chan error, 1)
	go func() { done <- c.Dispatch() }()
	select {
	case err := <-done:
		t.Fatalf("a second Dispatch returned %v, so five wakes bought five frames", err)
	case <-time.After(200 * time.Millisecond):
	}

	if err := c.Wake(); err != nil {
		t.Fatalf("Wake after the burst: %v", err)
	}
	select {
	case err := <-done:
		if !errors.Is(err, ErrWoken) {
			t.Fatalf("Dispatch = %v, want ErrWoken", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a wake after the first was consumed never arrived")
	}
}

// A wake during a Roundtrip is postponed, not lost.
//
// Returning it from the Roundtrip would tell the caller its requests had been
// processed when they had not; dropping it would lose the frame somebody asked
// for. So the round trip finishes, and the next Dispatch reports the wake.
func TestWakeDuringARoundtripIsDeferred(t *testing.T) {
	c, fs := newTestConn(t, NativeOrder)

	rt := make(chan error, 1)
	go func() { rt <- c.Roundtrip() }()

	// Read the sync request, which is also what tells us the round trip is under
	// way and there is something for the wake to interrupt.
	obj, op, d, err := fs.readReq()
	if err != nil {
		t.Fatalf("read sync: %v", err)
	}
	if obj != displayID || op != 0 {
		t.Fatalf("first request = object %d opcode %d, want the display's sync", obj, op)
	}
	cbID := d.getU32()

	if err := c.Wake(); err != nil {
		t.Fatalf("Wake: %v", err)
	}
	select {
	case err := <-rt:
		t.Fatalf("Roundtrip returned %v when it was woken -- the compositor still owed it a callback", err)
	case <-time.After(300 * time.Millisecond):
	}

	// Answer the sync. The round trip completes normally, as if nothing had
	// interrupted it.
	e := newEncoder(NativeOrder)
	e.putU32(0) // callback data
	if err := fs.sendEvt(cbID, 0, e.buf); err != nil {
		t.Fatalf("send done: %v", err)
	}
	select {
	case err := <-rt:
		if err != nil {
			t.Fatalf("Roundtrip: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Roundtrip never completed after its callback arrived")
	}

	// And the postponed wake surfaces at the next Dispatch, without going near
	// the socket — which is why this one cannot block.
	if err := c.Dispatch(); !errors.Is(err, ErrWoken) {
		t.Fatalf("Dispatch after the roundtrip = %v, want the deferred ErrWoken", err)
	}
}

// A connection that is already closed cannot be woken, and must not be left
// believing a wakeup is on its way: the flag would block every later one if the
// caller kept the object around.
func TestWakeOnAClosedConnection(t *testing.T) {
	c, _ := newTestConn(t, NativeOrder)
	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := c.Wake(); err == nil {
		t.Fatal("waking a closed connection reported success")
	}
	if c.wake.pending.Load() {
		t.Error("a wake that could not be armed stayed marked as pending")
	}
}
