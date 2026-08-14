// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package window

import (
	"sync"
	"testing"

	"github.com/go-widgets/window/internal/wayland"
)

// The Wayland half of the same invariant: Close may be called from any
// goroutine, and it unmaps the wl_shm pool the paint path writes into.
//
// This is the exact crash that killed a CI lane — PackARGB8888 writing into a
// mapping that had just gone — reproduced small enough to run every time.
func TestWaylandCloseWhilePainting(t *testing.T) {
	cli, srv := socketPairWin(t)
	defer srv.Close()
	closeReq := make(chan struct{})
	go quietCompositor(t, &srvConn{c: srv}, closeReq)

	conn := wayland.New(cli)
	w, err := newWaylandWindow(conn, Config{Title: "close", Width: 96, Height: 64})
	if err != nil {
		t.Fatalf("newWaylandWindow: %v", err)
	}
	w.root = &countingWLRoot{}

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 500; i++ {
			_ = w.paintFrame() // the frame the run loop would be painting
		}
	}()

	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	wg.Wait()
	close(closeReq)

	if err := w.Close(); err != nil {
		t.Errorf("the second Close returned %v, want nil", err)
	}
	if w.pool != nil {
		t.Error("the shm pool survived Close")
	}
}

// Several goroutines closing at once tear the window down exactly once.
func TestWaylandCloseIsIdempotentUnderConcurrency(t *testing.T) {
	cli, srv := socketPairWin(t)
	defer srv.Close()
	closeReq := make(chan struct{})
	go quietCompositor(t, &srvConn{c: srv}, closeReq)

	conn := wayland.New(cli)
	w, err := newWaylandWindow(conn, Config{Title: "close", Width: 96, Height: 64})
	if err != nil {
		t.Fatalf("newWaylandWindow: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = w.Close()
		}()
	}
	wg.Wait()
	close(closeReq)

	if !w.closed {
		t.Error("after sixteen concurrent Closes the window does not consider itself closed")
	}
	if w.pool != nil {
		t.Error("the shm pool survived Close")
	}
}
