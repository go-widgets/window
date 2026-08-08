// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux

package window

// Open is unavailable off Linux: there is no X11 server to dial, so it
// returns ErrUnsupported. The window-construction, presentation and
// event-translation logic remains compiled and unit-tested on every
// platform via the transport-agnostic internal/x11 connection; only this
// environment-driven entry point is gated, keeping cross-builds green.
func Open(cfg Config) (*Window, error) {
	_ = cfg
	return nil, ErrUnsupported
}
