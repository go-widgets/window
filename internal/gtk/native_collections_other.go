// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux && !android && !(amd64 || arm64)

package gtk

import (
	"github.com/go-gtk/gtk4"
	"github.com/go-widgets/toolkit"
)

// Off amd64/arm64 there is no GTK4 binding (github.com/go-gtk/gtk4 is stubbed
// there), and importing purego's Dlopen would make the binary dynamically linked
// and unrunnable on a bare system. The three collection kinds are simply not
// hosted: makeControl returns nil for them and the painted fallback stays.

type collection struct{}

func newCollection(*liveControl, toolkit.NativeControl) *collection { return nil }
func (c *collection) widget() gtk4.Widget                          { return 0 }
func (c *collection) release()                                     {}
func (c *collection) apply(*liveControl, toolkit.NativeControl)    {}
