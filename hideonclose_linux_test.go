// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package window

import (
	"testing"

	"github.com/go-widgets/window/internal/wayland"
	"github.com/go-widgets/window/internal/x11"
)

// With HideOnClose, WM_DELETE_WINDOW withdraws the window and Run goes on; without
// it, it quits.
func TestX11DeleteWindowHidesWithHideOnClose(t *testing.T) {
	w, _ := dialFake(t, Config{Width: 100, Height: 80})
	del := x11.Event{Code: 33, Format: 32, Atom: atomWMProtocols, Data32: atomWMDeleteWindow}

	w.hideOnClose = true
	if out := w.mapEvent(del); out.quit {
		t.Fatal("HideOnClose: the delete message quit")
	}
	w.hideOnClose = false
	if out := w.mapEvent(del); !out.quit {
		t.Fatal("without HideOnClose: the delete message did not quit")
	}
}

// The compositor's close request unmaps the window with HideOnClose and ends the
// loop without it.
func TestWaylandCloseRequestHidesWithHideOnClose(t *testing.T) {
	cli, srv := socketPairWin(t)
	defer srv.Close()
	closeReq := make(chan struct{})
	go quietCompositor(t, &srvConn{c: srv}, closeReq)
	w, err := newWaylandWindow(wayland.New(cli), Config{Title: "hide", Width: 96, Height: 64})
	if err != nil {
		t.Fatalf("newWaylandWindow: %v", err)
	}
	defer func() { _ = w.Close(); close(closeReq) }()

	w.hideOnClose = true
	w.toplevel.OnClose()
	if w.quit {
		t.Fatal("HideOnClose: the close request ended the loop")
	}
	if got := w.vis.want.Load(); got != visHide {
		t.Fatalf("HideOnClose: pending visibility %d, want visHide", got)
	}

	w.hideOnClose = false
	w.toplevel.OnClose()
	if !w.quit {
		t.Fatal("without HideOnClose: the close request did not end the loop")
	}
}
