// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build android

package window

import (
	"os"
	"strings"
	"testing"

	"github.com/go-widgets/android"
	"github.com/go-widgets/toolkit"
)

// The two paths Open can take on Android are told apart by WHICH failure they
// produce, since neither a host nor a display server exists under test. These
// are the two error texts, captured on a device:
//
//	no host: "window: neither WAYLAND_DISPLAY nor DISPLAY is set"
//	host:    "android: cannot reach the host: dial unix @...: connection refused"
//
// Asserting only that the no-host error does NOT mention $GW_ANDROID_SOCKET
// would be VACUOUS — the host error does not mention it either, so that check
// passes whichever path ran. Each test therefore asserts the positive marker of
// the path it wants, and TestOpenAndroidPathsAreDistinguishable is the control
// proving the two markers cannot both match one error.
const (
	markerDisplayServer = "WAYLAND_DISPLAY"
	markerHost          = "cannot reach the host"
)

// TestOpenAndroidWithoutHostFallsThroughToDisplayServer pins the branch that
// makes ONE binary serve both Androids: with no host socket announced, this is
// a shell under Termux, and Open must try the display server rather than
// assume an APK.
func TestOpenAndroidWithoutHostFallsThroughToDisplayServer(t *testing.T) {
	t.Setenv(android.EnvSocket, "")
	t.Setenv("WAYLAND_DISPLAY", "")
	w, err := Open(Config{Title: "x"})
	if err == nil {
		w.Close()
		t.Fatalf("Open with no host and no display server should fail")
	}
	if !strings.Contains(err.Error(), markerDisplayServer) {
		t.Fatalf("Open with no host should have tried the display server, got %v", err)
	}
}

// TestOpenAndroidWithHostDialsTheHost covers the APK branch: an announced
// socket must be dialled, never bypassed for a display server that an APK does
// not have.
func TestOpenAndroidWithHostDialsTheHost(t *testing.T) {
	t.Setenv(android.EnvSocket, "gw-window-test-no-such-host")
	t.Setenv("WAYLAND_DISPLAY", "")
	w, err := Open(Config{Title: "x", Theme: toolkit.DefaultLight()})
	if err == nil {
		w.Close()
		t.Fatalf("Open should fail when no host listens on the announced socket")
	}
	if !strings.Contains(err.Error(), markerHost) {
		t.Fatalf("Open with a host announced should have dialled it, got %v", err)
	}
}

// TestOpenAndroidDefaultsTheTheme covers the nil-Theme branch, which must not
// hand the client a nil theme to paint with.
func TestOpenAndroidDefaultsTheTheme(t *testing.T) {
	os.Setenv(android.EnvSocket, "gw-window-test-no-such-host")
	defer os.Unsetenv(android.EnvSocket)
	_, err := Open(Config{Title: "x"})
	if err == nil || !strings.Contains(err.Error(), markerHost) {
		t.Fatalf("Open with a host announced should have dialled it, got %v", err)
	}
}

// TestOpenAndroidPathsAreDistinguishable is the control for the two tests
// above: it fails if the markers stop telling the paths apart, which would
// silently turn both of them into tests that assert nothing.
func TestOpenAndroidPathsAreDistinguishable(t *testing.T) {
	t.Setenv(android.EnvSocket, "")
	t.Setenv("WAYLAND_DISPLAY", "")
	_, noHost := Open(Config{Title: "x"})
	t.Setenv(android.EnvSocket, "gw-window-test-no-such-host")
	_, host := Open(Config{Title: "x"})
	if noHost == nil || host == nil {
		t.Fatalf("both paths must fail under test: %v / %v", noHost, host)
	}
	if strings.Contains(noHost.Error(), markerHost) {
		t.Fatalf("the host marker also matches the display-server error: %v", noHost)
	}
	if strings.Contains(host.Error(), markerDisplayServer) {
		t.Fatalf("the display-server marker also matches the host error: %v", host)
	}
}
