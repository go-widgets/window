// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux && !android

package window

import (
	"errors"
	"fmt"
	"os"

	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/tray"
	"github.com/go-widgets/window/internal/gtk"
)

// A GTK4-hosted backend satisfies the same Backend contract as the from-scratch
// X11/Wayland ones, the Repainter capability that lets the application present
// loop drive it on demand instead of blitting every tick, and Visibility.
var (
	_ Backend    = (*gtk.Window)(nil)
	_ Repainter  = (*gtk.Window)(nil)
	_ Visibility = gtkBackend{}
	_ MenuBar    = gtkBackend{}
	_ Backend    = gtkBackend{}
	_ Repainter  = gtkBackend{}
	_ Windows    = gtkBackend{}
	_ Child      = gtkChild{}
)

// gtkBackend is the GTK window wearing this package's vocabulary: the back-end
// lives in an internal package this one imports, so its closed-window error is
// mapped to [ErrClosed] here. Everything else is promoted unchanged.
type gtkBackend struct{ *gtk.Window }

// Show, Hide and Raise implement [Visibility] over the GTK window's own.
func (b gtkBackend) Show() error  { return gtkErr(b.Window.Show()) }
func (b gtkBackend) Hide() error  { return gtkErr(b.Window.Hide()) }
func (b gtkBackend) Raise() error { return gtkErr(b.Window.Raise()) }

// SetMenuBar implements [MenuBar]: a GtkPopoverMenuBar above the content (see
// internal/gtk). GTK has no application-name menu, so appName is unused.
func (b gtkBackend) SetMenuBar(_ string, m *tray.Menu) error {
	err := b.Window.SetMenuBar(m)
	if errors.Is(err, gtk.ErrNoMenuBar) {
		return fmt.Errorf("%w: %v", ErrNotSupported, err)
	}
	return gtkErr(err)
}

// gtkChild is a secondary GTK window wearing this package's vocabulary.
type gtkChild struct{ *gtk.Window }

// OpenChild implements [Windows]: a second GtkWindow on the loop this one runs.
// Only the GTK4-hosted back-end has it; the X11 and Wayland back-ends, which
// own their connection and their loop, do not.
func (b gtkBackend) OpenChild(cfg Config, root toolkit.Widget, onClose func()) (Child, error) {
	c, err := b.Window.OpenChild(cfg.Title, cfg.Width, cfg.Height, cfg.Theme, cfg.RenderScale, root, onClose)
	if err != nil {
		return nil, gtkErr(err)
	}
	return gtkChild{c}, nil
}

func (c gtkChild) Close() error { return gtkErr(c.Window.Close()) }
func (c gtkChild) Raise() error { return gtkErr(c.Window.Raise()) }

func gtkErr(err error) error {
	if errors.Is(err, gtk.ErrClosed) {
		return ErrClosed
	}
	return err
}

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
		w, err := gtk.Open(cfg.Title, cfg.Width, cfg.Height, cfg.Theme, cfg.RenderScale)
		if err != nil {
			return nil, err
		}
		w.SetHideOnClose(cfg.HideOnClose)
		return gtkBackend{w}, nil
	}
	return openDisplayServer(cfg)
}
