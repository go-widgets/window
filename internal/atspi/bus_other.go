// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux

package atspi

import "github.com/go-widgets/toolkit"

// Publish does nothing off Linux: AT-SPI is the Linux accessibility bus, and
// macOS and Windows have their own bridges in internal/cocoa and internal/win32.
//
// The stub exists because the X11 back-end is compiled on every platform — its
// protocol codec is unit-tested everywhere, which is what keeps cross-builds
// green — so the call site must resolve even where the bus cannot exist. Same
// reason open_other.go supplies an Open that returns ErrUnsupported.
func Publish(root toolkit.Widget, title string, originX, originY int, activate func(x, y int)) {
}
