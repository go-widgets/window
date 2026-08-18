// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux && !android

package window

// Open returns a live window backed by this environment's display server. It
// auto-selects: Wayland when $WAYLAND_DISPLAY is set, X11 otherwise.
//
// Android is excluded here and answered by open_android.go, because a binary
// there may be running inside an APK, where there is no display server to dial
// at all.
func Open(cfg Config) (Backend, error) { return openDisplayServer(cfg) }
