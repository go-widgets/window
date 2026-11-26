// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package window

import (
	"errors"
	"image/color"
	"runtime"

	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/tray"
	"github.com/go-widgets/window/internal/cocoa"
)

// newCocoaWindow creates the NSWindow; a variable so a test can make it fail.
var newCocoaWindow = cocoa.NewWithOptions

// Open returns the native macOS (Cocoa/AppKit) backend: it creates a real
// NSWindow with a flipped content NSView, ready for Run to present the
// go-widgets framebuffer and route NSEvent input. The backend is pure-Go
// (CGO-free) via github.com/go-macos/objc over purego, so a go-widgets app runs
// through Open→Run on macOS exactly as it does on X11/Wayland (open_linux.go)
// and inside wasmdesk (open_js.go).
//
// AppKit requires all window and run-loop work on the process main OS thread,
// so Open pins the calling goroutine with runtime.LockOSThread; the caller
// should therefore invoke Open+Run from main (as cmd/windowdemo does).
func Open(cfg Config) (Backend, error) {
	runtime.LockOSThread()
	opts := cocoaOptions(cfg)
	w, err := newCocoaWindow(opts)
	if err != nil {
		return nil, err
	}
	applyMetricScale(cfg.RenderScale, w.RenderScale())
	return darwinBackend{w}, nil
}

// cocoaOptions is cfg in the back-end's own vocabulary, for the main window and
// for a secondary one alike.
func cocoaOptions(cfg Config) cocoa.Options {
	opts := cocoa.Options{
		Title:       cfg.Title,
		Width:       cfg.Width,
		Height:      cfg.Height,
		Theme:       cfg.Theme,
		RenderScale: cfg.RenderScale,
		Fullscreen:  cfg.Fullscreen,
		Immersive:   cfg.Immersive,
		FixedSize:   cfg.FixedSize,
		Passive:     cfg.Passive,
		HideOnClose: cfg.HideOnClose,
		GPU:         cfg.GPU,
	}
	if cfg.Screen != nil {
		s := cfg.Screen.toCocoa()
		opts.Screen = &s
	}
	return opts
}

// darwinChild is a secondary Cocoa window wearing this package's vocabulary.
type darwinChild struct{ *cocoa.Window }

// OpenChild implements Windows: a second NSWindow on the loop this one runs.
func (b darwinBackend) OpenChild(cfg Config, root toolkit.Widget, onClose func()) (Child, error) {
	c, err := b.Window.OpenChild(cocoaOptions(cfg), root, onClose)
	if err != nil {
		return nil, cocoaErr(err)
	}
	return darwinChild{c}, nil
}

func (c darwinChild) Raise() error { return cocoaErr(c.Window.Raise()) }

// darwinBackend is the Cocoa window wearing this package's public vocabulary.
// The back-end lives in an internal package that THIS one imports, so it cannot
// name Appearance without a cycle; it reports primitive values and the wrapper
// puts them in the public shape. Everything else -- Run, Close, Size, String,
// the clipboard -- is promoted from the embedded window unchanged.
type darwinBackend struct{ *cocoa.Window }

// Appearance implements AppearanceReader over the back-end's raw reading.
func (b darwinBackend) Appearance() Appearance {
	dark, r, g, bl, has := b.Window.AppearanceRaw()
	return Appearance{
		Dark:      dark,
		Accent:    color.RGBA{R: r, G: g, B: bl, A: 255},
		HasAccent: has,
	}
}

// Show, Hide and Raise implement Visibility over the back-end's own, putting
// its closed-window error in this package's vocabulary.
func (b darwinBackend) Show() error  { return cocoaErr(b.Window.Show()) }
func (b darwinBackend) Hide() error  { return cocoaErr(b.Window.Hide()) }
func (b darwinBackend) Raise() error { return cocoaErr(b.Window.Raise()) }

// OpenFile, SaveFile and Alert implement Dialogs over the back-end's own queue.
func (b darwinBackend) OpenFile(o OpenOptions, done func(OpenResult)) error {
	return cocoaErr(b.Window.Dialog(cocoa.DialogRequest{
		Kind: cocoa.DialogOpen, Title: o.Title, Exts: flattenExts(o.Filters),
		Multiple: o.Multiple, Directories: o.Directories,
		Done: func(r cocoa.DialogResult) { done(openResult(r)) },
	}))
}

func openResult(r cocoa.DialogResult) OpenResult {
	if r.Cancelled {
		return OpenResult{Err: ErrCancelled}
	}
	return OpenResult{Paths: r.Paths}
}

func saveResult(r cocoa.DialogResult) SaveResult {
	if r.Cancelled || len(r.Paths) == 0 {
		return SaveResult{Err: ErrCancelled}
	}
	return SaveResult{Path: r.Paths[0]}
}

func (b darwinBackend) SaveFile(o SaveOptions, done func(SaveResult)) error {
	return cocoaErr(b.Window.Dialog(cocoa.DialogRequest{
		Kind: cocoa.DialogSave, Title: o.Title, Name: o.Name, Exts: flattenExts(o.Filters),
		Done: func(r cocoa.DialogResult) { done(saveResult(r)) },
	}))
}

func (b darwinBackend) Alert(o AlertOptions, done func(AlertResult)) error {
	return cocoaErr(b.Window.Dialog(cocoa.DialogRequest{
		Kind: cocoa.DialogAlert, Message: o.Message, Detail: o.Detail, Buttons: o.Buttons,
		Done: func(r cocoa.DialogResult) { done(AlertResult{Button: r.Button}) },
	}))
}

// flattenExts is every filter's extensions in one list: AppKit's panels take a
// single set of allowed types, not named groups.
func flattenExts(fs []Filter) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.Extensions...)
	}
	return out
}

// SetMenuBar implements MenuBar: the application's main menu, which tray owns
// (NSApp.mainMenu is per application, not per window), with the standard
// application menu first.
func (b darwinBackend) SetMenuBar(appName string, m *tray.Menu) error {
	return tray.SetMainMenu(appName, m)
}

func cocoaErr(err error) error {
	if errors.Is(err, cocoa.ErrClosed) {
		return ErrClosed
	}
	return err
}

// The Cocoa back-end carries the OS pasteboard. Asserting it here, where the
// capability is declared, is what stops a rename inside the back-end from
// silently turning `w.(window.Clipboard)` into a failed assertion and an app
// back on the in-process clipboard with nothing to show for it.
var (
	_ Clipboard        = (*cocoa.Window)(nil)
	_ AppearanceReader = darwinBackend{}
	_ Scaler           = (*cocoa.Window)(nil)
	_ Repainter        = (*cocoa.Window)(nil)
	_ Placement        = (*cocoa.Window)(nil)
	_ Backend          = darwinBackend{}
	_ Visibility       = darwinBackend{}
	_ Dialogs          = darwinBackend{}
	_ MenuBar          = darwinBackend{}
	_ Windows          = darwinBackend{}
	_ Child            = darwinChild{}
)
