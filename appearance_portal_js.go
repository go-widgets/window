// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build js

package window

import "errors"

// In the browser there is no session bus and no desktop to ask. The X11 Window
// still compiles here -- it is untagged, so every target builds it -- and these
// keep it honest rather than pretending js/wasm has a desktop appearance. The
// wasmbox backend is what actually runs on this target.

var errNoSystemFont = errors.New("window: no system font on this platform")

// Appearance reports no preference. Implements the AppearanceReader capability.
func (w *Window) Appearance() Appearance { return Appearance{} }

// SystemFontTTF reports that there is none. Implements the AppearanceReader
// capability.
func (w *Window) SystemFontTTF() ([]byte, error) { return nil, errNoSystemFont }
