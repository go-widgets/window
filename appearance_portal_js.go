// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build js

package window

import "errors"

// In the browser there is no session bus and no desktop to ask. Both Linux
// windows still compile here -- they are untagged, so every target builds them --
// and these keep them honest rather than pretending js/wasm has a desktop
// appearance. The wasmbox backend is what actually runs on this target.

var errNoSystemFont = errors.New("window: no system font on this platform")

// appearance reports no preference.
func (p *portalConn) appearance() Appearance { return Appearance{} }
