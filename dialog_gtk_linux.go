// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux && !android

package window

import (
	"errors"
	"fmt"

	"github.com/go-gtk/gtk4"
)

// gtkBackend (open_linux_only.go) is the GTK4 backend; these methods give it the
// [Dialogs] capability: GtkFileDialog and GtkAlertDialog, which need GTK 4.10. On
// an older GTK every request answers [ErrNotSupported].
var _ Dialogs = gtkBackend{}

func (b gtkBackend) OpenFile(o OpenOptions, done func(OpenResult)) error {
	kind := gtk4.FileOpen
	switch {
	case o.Directories:
		kind = gtk4.FileSelectFolder
	case o.Multiple:
		kind = gtk4.FileOpenMultiple
	}
	return gtkUnsupported(b.FileDialog(gtk4.FileDialogRequest{
		Kind: kind, Title: o.Title, Filters: gtkFilters(o.Filters),
	}, func(r gtk4.FileDialogResult) {
		done(OpenResult{Paths: r.Paths, Err: gtkFileErr(r)})
	}))
}

func (b gtkBackend) SaveFile(o SaveOptions, done func(SaveResult)) error {
	return gtkUnsupported(b.FileDialog(gtk4.FileDialogRequest{
		Kind: gtk4.FileSave, Title: o.Title, InitialName: o.Name, Filters: gtkFilters(o.Filters),
	}, func(r gtk4.FileDialogResult) {
		res := SaveResult{Err: gtkFileErr(r)}
		if res.Err == nil {
			res.Path = r.Paths[0]
		}
		done(res)
	}))
}

func (b gtkBackend) Alert(o AlertOptions, done func(AlertResult)) error {
	buttons := o.Buttons
	if len(buttons) == 0 {
		buttons = []string{"OK"}
	}
	return gtkUnsupported(b.Window.Alert(gtk4.AlertRequest{
		Message: o.Message, Detail: o.Detail, Buttons: buttons,
	}, func(r gtk4.AlertResult) {
		res := AlertResult{Button: r.Button}
		switch {
		case r.Cancelled:
			res = AlertResult{Err: ErrCancelled}
		case r.Err != nil:
			res = AlertResult{Err: r.Err}
		}
		done(res)
	}))
}

// gtkFilters turns the public filters into GTK's: the extensions are suffixes.
func gtkFilters(fs []Filter) []gtk4.FileFilter {
	out := make([]gtk4.FileFilter, len(fs))
	for i, f := range fs {
		out[i] = gtk4.FileFilter{Name: f.Name, Suffixes: f.Extensions}
	}
	return out
}

// gtkFileErr is the callback error of a file dialog's answer: cancel is
// [ErrCancelled], a failure is itself, and an answer with no path is a failure
// too, since a success holds at least one.
func gtkFileErr(r gtk4.FileDialogResult) error {
	switch {
	case r.Cancelled:
		return ErrCancelled
	case r.Err != nil:
		return r.Err
	case len(r.Paths) == 0:
		return errors.New("window: GTK file dialog returned no path")
	}
	return nil
}

// gtkUnsupported maps the binding's "GTK older than 4.10" to [ErrNotSupported].
func gtkUnsupported(err error) error {
	if errors.Is(err, gtk4.ErrDialogUnsupported) {
		return fmt.Errorf("%w: %v", ErrNotSupported, err)
	}
	return err
}
