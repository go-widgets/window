// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux && !android

package gtk

import "github.com/go-gtk/gtk4"

// FileDialog shows a GtkFileDialog over the window. The request is queued onto
// the GTK main loop, so it is safe from any goroutine; done runs exactly once, on
// a goroutine of its own (never the UI thread, so it may block or open the next
// dialog), and never when an error is returned. gtk4.ErrDialogUnsupported comes
// back synchronously on a GTK older than 4.10.
func (w *Window) FileDialog(req gtk4.FileDialogRequest, done func(gtk4.FileDialogResult)) error {
	return gtk4.ShowFileDialog(w.win, req, func(r gtk4.FileDialogResult) { go done(r) })
}

// Alert shows a GtkAlertDialog over the window, with the same threading and error
// rules as [Window.FileDialog].
func (w *Window) Alert(req gtk4.AlertRequest, done func(gtk4.AlertResult)) error {
	return gtk4.ShowAlert(w.win, req, func(r gtk4.AlertResult) { go done(r) })
}
