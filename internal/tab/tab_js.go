// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build js && wasm

package tab

import (
	"fmt"
	"sync/atomic"
	"syscall/js"

	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/webcanvas"
)

// Backend draws into the <canvas> whose id it was opened with.
type Backend struct {
	canvas string
	w, h   int
	theme  *toolkit.Theme
	tree   atomic.Pointer[Tree]
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
	t := NewTree(root, b.w, b.h, b.theme)
	b.tree.Store(t)
	webcanvas.Run(b.canvas, t)
	return nil
}

// Repaint asks for a frame from any goroutine (window.Repainter): the next
// animation frame redraws the tree. Before Run it does nothing.
func (b *Backend) Repaint() {
	if t := b.tree.Load(); t != nil {
		t.Repaint()
	}
}

// Close does nothing: a tab ends when the page does.
func (b *Backend) Close() error { return nil }

// Size is the surface in pixels.
func (b *Backend) Size() (int, int) { return b.w, b.h }

func (b *Backend) String() string { return "browser tab (canvas #" + b.canvas + ")" }
