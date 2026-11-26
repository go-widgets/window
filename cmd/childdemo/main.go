// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// Command childdemo shows a secondary window: the main window has a "+" button,
// and pressing it opens a second window, holding a small form, on the loop the
// first one already runs. Closing the second window leaves the first running;
// closing the first closes both. Backends without the capability say so.
package main

import (
	"fmt"
	"log"

	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/window"
)

func main() {
	w, err := window.Open(window.Config{Title: "childdemo: main", Class: "childdemo", Width: 360, Height: 200})
	if err == window.ErrUnsupported {
		fmt.Println("childdemo:", err)
	} else if err != nil {
		log.Fatal("childdemo: ", err)
	} else {
		defer w.Close()
		log.Print(w.Run(mainScene(w)))
	}
}

// mainScene is the main window: a status line and the "+" button.
func mainScene(parent window.Backend) toolkit.Widget {
	status := toolkit.NewLabel("no second window yet")
	var form window.Child
	box := toolkit.NewVBox()
	box.Append(toolkit.NewLabel("Servers"))
	box.Append(status)
	box.Append(toolkit.NewButton("+", func() {
		if form != nil {
			form.Raise()
		} else {
			open(parent, status, &form)
		}
	}))
	return box
}

// open opens the second window and keeps the handle in *form while it lives.
func open(parent window.Backend, status *toolkit.Label, form *window.Child) {
	c, err := window.OpenChild(parent, window.Config{Title: "Add server", Width: 320, Height: 160},
		formScene(func() { (*form).Close() }),
		func() { *form = nil; status.Text().Set("second window closed") })
	if err != nil {
		status.Text().Set("cannot open: " + err.Error())
	} else {
		*form = c
		status.Text().Set("second window open")
	}
}

func formScene(done func()) toolkit.Widget {
	box := toolkit.NewVBox()
	box.Append(toolkit.NewLabel("Add a server"))
	box.Append(toolkit.NewLabel("This is a second window."))
	box.Append(toolkit.NewButton("Close", done))
	return box
}
