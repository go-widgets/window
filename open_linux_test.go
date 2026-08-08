// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package window

import (
	"testing"
)

func TestOpenErrors(t *testing.T) {
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
