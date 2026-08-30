// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// Command gowidgetsclient is a tiny go-widgets application built to js/wasm so a
// wasmdesk/wasmbox compositor can spawn it as an external client (via
// wasmboxSpawnExternal on clients/gowidgets/worker.js). It is deliberately the
// SAME shape as a native go-widgets program — build a widget tree, Open a
// backend, Run it — proving the point of the wasmbox backend: one unchanged app
// runs natively (X11/Wayland) AND inside wasmdesk.
//
// The tree is a VBox of a Label and a Button; each click increments a counter,
// updates the label and Invalidates it through the scene.HostRoot, so the
// backend commits only the damaged rectangle (the incremental-present path the
// X11/Wayland backends also use). Run blocks on compositor input until the
// window closes.
//
//go:build js && wasm

package main

import (
	"fmt"

	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/toolkit/scene"
	"github.com/go-widgets/window"
)

func main() {
	label := toolkit.NewLabel("Clicks: 0")
	label.Align = toolkit.AlignCenter

	var root *scene.HostRoot
	count := 0
	button := toolkit.NewButton("Click me", func() {
		count++
		label.Text().Set(fmt.Sprintf("Clicks: %d", count))
		if root != nil {
			root.Invalidate(label) // damage just the label → incremental commit
		}
	})

	box := toolkit.NewVBox()
	box.Append(label)
	box.Append(button)
	root = scene.NewHostRoot(box)

	backend, err := window.Open(window.Config{
		Title:  "go-widgets on wasmbox",
		Width:  260,
		Height: 160,
	})
	if err != nil {
		println("gowidgetsclient: open failed:", err.Error())
		return
	}
	if err := backend.Run(root); err != nil {
		println("gowidgetsclient: run failed:", err.Error())
	}
}
