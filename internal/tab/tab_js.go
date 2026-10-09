// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build js && wasm

package tab

import (
	"fmt"
	"syscall/js"

	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/webcanvas"
)

// Backend draws into the <canvas> whose id it was opened with.
type Backend struct {
	canvas string
	w, h   int
	theme  *toolkit.Theme
}

// Open checks that the page has the canvas, and returns a Backend for it.
func Open(canvas string, w, h int, theme *toolkit.Theme) (*Backend, error) {
	if canvas == "" {
		canvas = "screen"
	}
	el := js.Global().Get("document").Call("getElementById", canvas)
	if el.IsNull() || el.IsUndefined() {
		return nil, fmt.Errorf("window: no <canvas id=%q> in the page", canvas)
	}
	if w <= 0 {
		w = 640
	}
	if h <= 0 {
		h = 480
	}
	return &Backend{canvas: canvas, w: w, h: h, theme: theme}, nil
}

// Run drives root until the page goes away.
func (b *Backend) Run(root toolkit.Widget) error {
	webcanvas.Run(b.canvas, NewTree(root, b.w, b.h, b.theme))
	return nil
}

// Close does nothing: a tab ends when the page does.
func (b *Backend) Close() error { return nil }

// Size is the surface in pixels.
func (b *Backend) Size() (int, int) { return b.w, b.h }

func (b *Backend) String() string { return "browser tab (canvas #" + b.canvas + ")" }
