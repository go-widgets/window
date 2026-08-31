// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux && !android

package window

import (
	"os"

	"github.com/go-widgets/window/internal/gtk"
)

// A GTK4-hosted backend satisfies the same Backend contract as the from-scratch
// X11/Wayland ones.
var _ Backend = (*gtk.Window)(nil)

// Open returns a live window backed by this environment's display server. It
// auto-selects: Wayland when $WAYLAND_DISPLAY is set, X11 otherwise.
//
// Setting $GO_WIDGETS_GTK opts into the GTK4-hosted backend instead — GTK owns
// the window, the toolkit's framebuffer goes in a GtkPicture and native controls
// (toolkit.NativeControl) are real GTK widgets overlaid above it. It is opt-in so
// the default stays the dependency-free from-scratch backends; the GTK path needs
// the libgtk-4 runtime present.
//
// Android is excluded here and answered by open_android.go, because a binary
// there may be running inside an APK, where there is no display server to dial
// at all.
func Open(cfg Config) (Backend, error) {
	if os.Getenv("GO_WIDGETS_GTK") != "" {
		return gtk.Open(cfg.Title, cfg.Width, cfg.Height, cfg.Theme, cfg.RenderScale)
	}
	return openDisplayServer(cfg)
}
