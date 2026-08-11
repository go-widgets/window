// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import "time"

// portalConn is where the desktop appearance reading is kept between polls.
//
// It holds the bus as an untyped value and lives in an untagged file on
// purpose: the X11 Window is compiled on every target, including js/wasm, where
// the D-Bus package it would otherwise name does not exist. The typed use is in
// appearance_portal.go, which is built everywhere a session bus can exist.
type portalConn struct {
	bus    any
	tried  bool
	cached Appearance
	at     time.Time
}
