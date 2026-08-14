// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package wayland

import (
	"errors"
	"os"
	"sync/atomic"
	"time"
)

// Waking a dispatch that is blocked on the socket.
//
// Unlike X11, a Wayland client cannot send itself an event: every message on
// the connection comes from the compositor, and there is no request that asks
// for one back. So the wait has to be interrupted rather than satisfied — and
// the read is a runtime-poller read on a UNIX socket, which is exactly what a
// READ DEADLINE interrupts. Setting one in the past makes the pending read
// return immediately, without a second file descriptor, a poll loop, or a
// thread to own either.
//
// Nothing is lost by interrupting: the transport keeps its partially-read bytes
// in its own buffer, so a message split across the interruption is completed by
// the next read.

// ErrWoken is what Dispatch returns when [Conn.Wake] interrupted the wait
// rather than an event arriving. It is not a failure: the caller has been asked
// to do something — repaint, usually — and should carry on dispatching.
var ErrWoken = errors.New("wayland: dispatch woken")

// interrupter is the part of a transport that can have its read interrupted.
// Only the real socket transport can; an in-process fake has no blocked read to
// interrupt, so [Conn.Wake] over one reports that it cannot wake.
type interrupter interface {
	// interruptRead makes any read in progress, and the next one, return
	// immediately.
	interruptRead() error
	// resumeReads restores blocking reads.
	resumeReads() error
}

// wakeState is the coalescing flag plus the wake a Roundtrip had to postpone.
type wakeState struct {
	pending  atomic.Bool // a wakeup is armed and not yet delivered
	deferred atomic.Bool // a wake arrived mid-Roundtrip; deliver at the next Dispatch
}

// Wake makes the next (or currently blocked) [Conn.Dispatch] return [ErrWoken].
//
// It is safe to call from any goroutine and returns without waiting. Calling it
// repeatedly before the dispatch loop gets there is not an error and costs
// nothing: one wakeup is armed at a time, because a second would only buy a
// second identical trip round the loop.
//
// A transport that cannot be interrupted — the in-process fake — returns an
// error rather than pretending to have woken anybody.
func (c *Conn) Wake() error {
	in, ok := c.t.(interrupter)
	if !ok {
		return errors.New("wayland: this transport cannot be woken")
	}
	if c.wake.pending.Swap(true) {
		return nil // already armed; the loop has not consumed it yet
	}
	if err := in.interruptRead(); err != nil {
		c.wake.pending.Store(false)
		return err
	}
	return nil
}

// tookWake reports whether err is our own interruption, and consumes it.
//
// Restoring blocking reads is part of consuming it: leaving the deadline in the
// past would turn every later read into an immediate error, which is a spin
// rather than an event loop.
func (c *Conn) tookWake(err error) bool {
	if !errors.Is(err, os.ErrDeadlineExceeded) {
		return false
	}
	if in, ok := c.t.(interrupter); ok {
		_ = in.resumeReads()
	}
	c.wake.pending.Store(false)
	return true
}

// deadlineNow is the instant a read deadline is set to in order to interrupt a
// read. Any past instant does, and time.Now() is the one that cannot be
// mistaken for a real timeout policy.
func deadlineNow() time.Time { return time.Now() }
