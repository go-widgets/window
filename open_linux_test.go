// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package window

import (
	"testing"
)

func TestOpenErrors(t *testing.T) {
	// With WAYLAND_DISPLAY unset, Open falls through to the X11 backend.
	t.Setenv("WAYLAND_DISPLAY", "")
	// DISPLAY unset (and none supplied) errors.
	t.Setenv("DISPLAY", "")
	if _, err := Open(Config{}); err == nil {
		t.Fatal("Open with no DISPLAY should error")
	}
	// A malformed DISPLAY errors at parse.
	if _, err := Open(Config{Display: "bogus"}); err == nil {
		t.Fatal("Open with malformed DISPLAY should error")
	}
	// A remote display is rejected (unix sockets only).
	if _, err := Open(Config{Display: "remotehost:0"}); err == nil {
		t.Fatal("Open with remote DISPLAY should error")
	}
	// A local display whose socket does not exist fails to connect.
	if _, err := Open(Config{Display: ":987"}); err == nil {
		t.Fatal("Open with nonexistent server should error")
	}
}

func TestOpenSelectsWayland(t *testing.T) {
	// When WAYLAND_DISPLAY is set, Open takes the Wayland path. Pointing it
	// at a nonexistent socket makes the dial fail (proving selection).
	t.Setenv("WAYLAND_DISPLAY", "gw-nonexistent-wl-0")
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	if _, err := Open(Config{}); err == nil {
		t.Fatal("Open with a dead Wayland socket should error")
	}
}

func TestWaylandSocketPath(t *testing.T) {
	// An absolute name is used verbatim.
	if p, err := waylandSocketPath("/run/user/1000/wayland-0"); err != nil || p != "/run/user/1000/wayland-0" {
		t.Fatalf("absolute path = %q err=%v", p, err)
	}
	// A bare name is joined onto XDG_RUNTIME_DIR.
	t.Setenv("XDG_RUNTIME_DIR", "/run/user/1000")
	if p, err := waylandSocketPath("wayland-1"); err != nil || p != "/run/user/1000/wayland-1" {
		t.Fatalf("joined path = %q err=%v", p, err)
	}
	// A bare name with no XDG_RUNTIME_DIR errors.
	t.Setenv("XDG_RUNTIME_DIR", "")
	if _, err := waylandSocketPath("wayland-0"); err == nil {
		t.Fatal("bare name with no XDG_RUNTIME_DIR should error")
	}
}

func TestOpenWaylandNoRuntimeDir(t *testing.T) {
	t.Setenv("WAYLAND_DISPLAY", "wayland-0")
	t.Setenv("XDG_RUNTIME_DIR", "")
	if _, err := Open(Config{}); err == nil {
		t.Fatal("Open with WAYLAND_DISPLAY but no XDG_RUNTIME_DIR should error")
	}
}

func TestDialDisplayRemote(t *testing.T) {
	if _, err := dialDisplay(display{host: "remote", number: "0"}); err == nil {
		t.Fatal("remote dial should be rejected")
	}
}

func TestAuthFilePathEnv(t *testing.T) {
	t.Setenv("XAUTHORITY", "/x/auth")
	if got := authFilePathEnv(); got != "/x/auth" {
		t.Fatalf("XAUTHORITY = %q", got)
	}
	t.Setenv("XAUTHORITY", "")
	// Falls back to $HOME/.Xauthority (or "" if no home).
	_ = authFilePathEnv()
}
