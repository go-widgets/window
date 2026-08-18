// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build android

package window

import (
	"os"

	"github.com/go-widgets/android"
	"github.com/go-widgets/toolkit"
)

// Open returns a live window on Android, which is TWO environments wearing one
// GOOS.
//
// Inside an APK there is no display server to dial: the whole graphics API is
// behind JNI, so a CGO-free application cannot own a window at all. It runs
// instead as the application half of github.com/go-widgets/android — a thin
// Java host owns the Activity and the surface, and the Go side paints into a
// shared mapping and speaks a socket protocol. The host names that socket in
// $GW_ANDROID_SOCKET when it spawns the application, and its presence is
// therefore the exact question "am I inside an APK?".
//
// The same binary may equally be run from a shell under Termux, against
// Termux:X11 or a Wayland compositor, where the display-server path is right
// and available. So when no host announced itself, this falls through to it —
// one binary, both environments, chosen by what is actually there.
func Open(cfg Config) (Backend, error) {
	if os.Getenv(android.EnvSocket) == "" {
		return openDisplayServer(cfg)
	}
	theme := cfg.Theme
	if theme == nil {
		theme = toolkit.DefaultDark()
	}
	return android.Dial(cfg.Title, theme)
}
