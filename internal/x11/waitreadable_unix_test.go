// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build unix

package x11

import (
	"net"
	"path/filepath"
	"testing"
	"time"
)

// The transport's own WaitReadable -- the peek-a-byte one that must not cut a
// packet in half -- is proved in github.com/go-freedesktop/x11, on a real
// socket, on every unix platform. What is proved HERE is the half-inch of glue
// this package owns: that a Conn built over that transport actually finds it
// and reports supported, rather than silently falling into the "cannot answer"
// branch and making every paste look like a dead clipboard owner.
//
// That branch is the one that matters. A Conn that answered (false, false) for
// a perfectly good socket would still pass every other test in this package,
// and would freeze a paste in production.
func TestConnWaitReadableOverARealSocket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = ln.Close() }()

	accepted := make(chan net.Conn, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			close(accepted)
			return
		}
		accepted <- c
	}()
	cli, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cli.Close() }()
	srv := <-accepted
	if srv == nil {
		t.Fatal("the listener never accepted")
	}
	defer func() { _ = srv.Close() }()

	c := &Conn{rw: WrapUnix(cli)}

	// Nothing sent: supported, and not ready.
	ready, supported := c.WaitReadable(80 * time.Millisecond)
	if !supported {
		t.Fatal("a Conn over a unix socket reported that it cannot wait")
	}
	if ready {
		t.Error("reported ready with nothing sent")
	}

	// Something sent: supported, and ready.
	if _, err := srv.Write([]byte("x")); err != nil {
		t.Fatalf("write: %v", err)
	}
	ready, supported = c.WaitReadable(2 * time.Second)
	if !supported || !ready {
		t.Errorf("ready=%v supported=%v with data waiting", ready, supported)
	}
}
