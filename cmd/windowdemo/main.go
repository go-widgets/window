// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// Command windowdemo opens a real native window showing a few go-widgets
// widgets, driven by the pure-Go github.com/go-widgets/window backend — an X11
// or Wayland window on Linux, an NSWindow on macOS, a Win32 window on Windows.
// On platforms without a native backend it prints the reason and exits cleanly,
// so the command cross-builds and runs everywhere.
package main

import (
	"fmt"
	"os"

	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/window"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "windowdemo:", err)
		os.Exit(1)
	}
}

// run builds a small widget scene and hands it to a window. It returns nil
// (not an error) when the backend is unsupported, so a cross-built binary
// simply reports and succeeds.
func run() error {
	w, err := window.Open(window.Config{
		Title: "go-widgets/window demo",
		Class: "windowdemo",
		// Width/Height left at 0 (logical points) so the backend picks a readable
		// default: on macOS a fraction of the main screen's visible frame, so the
		// window opens comfortably legible on a Retina display rather than tiny.
	})
	if err == window.ErrUnsupported {
		fmt.Println("windowdemo:", err)
		return nil
	}
	if err != nil {
		return err
	}
	defer w.Close()
	return w.Run(scene())
}

// scene composes the demonstration widget tree.
func scene() toolkit.Widget {
	box := toolkit.NewVBox()
	box.Append(toolkit.NewLabel("Hello from go-widgets/window"))
	box.Append(toolkit.NewLabel("Pure-Go X11 backend — no Xlib, no cgo"))
	clicks := 0
	label := toolkit.NewLabel("clicks: 0")
	btn := toolkit.NewButton("Click me", func() {
		clicks++
		label.Text = fmt.Sprintf("clicks: %d", clicks)
	})
	box.Append(btn)
	box.Append(label)
	return box
}
