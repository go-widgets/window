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

// The AppearanceReader capability for both Linux back-ends, forwarded to the
// shared portal client. The wrappers are here, untagged, so that the two windows
// answer identically on every target: the desktop's look does not depend on
// which display server is carrying the pixels.

// Appearance reports the desktop's colour scheme and accent colour. Implements
// the AppearanceReader capability.
func (w *Window) Appearance() Appearance { return w.portal.appearance() }

// SystemFontTTF reports that there is no font file to hand over. Implements the
// AppearanceReader capability.
func (w *Window) SystemFontTTF() ([]byte, error) { return nil, errNoSystemFont }

// Appearance reports the desktop's colour scheme and accent colour. Implements
// the AppearanceReader capability.
func (w *wlWindow) Appearance() Appearance { return w.portal.appearance() }

// SystemFontTTF reports that there is no font file to hand over. Implements the
// AppearanceReader capability.
func (w *wlWindow) SystemFontTTF() ([]byte, error) { return nil, errNoSystemFont }
