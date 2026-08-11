// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package x11

import (
	"testing"
	"time"
)

// WaitReadable is the only thing standing between a paste from a dead owner and
// a window that never comes back, so it is exercised on a REAL socket: an
// in-memory fake cannot tell "nothing arrived" from "nothing was ever going to".
func TestWaitReadableOnARealSocket(t *testing.T) {
	cli, srv := x11SocketPair(t)
	defer srv.Close()
	rw := WrapUnix(cli)
	c := &Conn{rw: rw}

	// Silence: it must report not-ready, and must do so promptly rather than
	// blocking for the whole timeout of whoever called it.
	start := time.Now()
	ready, supported := c.WaitReadable(120 * time.Millisecond)
	elapsed := time.Since(start)
	if !supported {
		t.Fatal("a unix socket should support waiting")
	}
	if ready {
		t.Error("reported ready with nothing sent")
	}
	if elapsed < 100*time.Millisecond {
		t.Errorf("gave up after %v, before the timeout", elapsed)
	}
	if elapsed > time.Second {
		t.Errorf("waited %v for a 120ms timeout", elapsed)
	}

	// Something to read: ready, and quickly.
	if _, err := srv.Write([]byte("hello")); err != nil {
		t.Fatalf("write: %v", err)
	}
	start = time.Now()
	ready, supported = c.WaitReadable(2 * time.Second)
	if !supported || !ready {
		t.Errorf("ready=%v supported=%v with data waiting", ready, supported)
	}
	if d := time.Since(start); d > time.Second {
		t.Errorf("took %v to notice data that was already there", d)
	}
}

// A timeout that has already passed is not a reason to block.
func TestWaitReadableZeroTimeout(t *testing.T) {
	cli, srv := x11SocketPair(t)
	defer srv.Close()
	c := &Conn{rw: WrapUnix(cli)}

	start := time.Now()
	if ready, _ := c.WaitReadable(0); ready {
		t.Error("reported ready on a zero timeout with nothing sent")
	}
	if d := time.Since(start); d > 500*time.Millisecond {
		t.Errorf("a zero timeout blocked for %v", d)
	}
}

// A closed socket cannot be waited on, and says so instead of spinning.
func TestWaitReadableClosedSocket(t *testing.T) {
	cli, srv := x11SocketPair(t)
	srv.Close()
	rw := WrapUnix(cli)
	cli.Close()
	c := &Conn{rw: rw}

	start := time.Now()
	c.WaitReadable(200 * time.Millisecond)
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("waiting on a closed socket took %v", d)
	}
}
