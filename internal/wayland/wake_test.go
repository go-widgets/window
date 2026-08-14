// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package wayland

import (
	"errors"
	"testing"
)

// A transport with no blocked read to interrupt says so, rather than reporting
// a wakeup nobody will ever see.
func TestWakeOnATransportThatCannotBeWoken(t *testing.T) {
	c := NewConn(&stubTransport{}, NativeOrder)
	if err := c.Wake(); err == nil {
		t.Fatal("waking an in-process transport reported success")
	}
	if c.wake.pending.Load() {
		t.Error("a wake that could not be armed left itself marked as pending, blocking every later one")
	}
}

// tookWake claims our own interruption and nothing else: a read error that is
// not a deadline is a real failure, and swallowing it would turn a broken
// connection into a silent repaint loop.
func TestTookWakeOnlyClaimsTheDeadline(t *testing.T) {
	c := NewConn(&stubTransport{}, NativeOrder)
	c.wake.pending.Store(true)
	if c.tookWake(errors.New("connection reset by peer")) {
		t.Error("a connection error was claimed as our own wakeup")
	}
	if !c.wake.pending.Load() {
		t.Error("an unrelated error consumed the armed wakeup")
	}
}
