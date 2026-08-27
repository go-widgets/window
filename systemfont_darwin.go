// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package window

import "github.com/go-widgets/window/internal/cocoa"

// systemFontTTF reads the macOS system face. See [SystemFontTTF].
func systemFontTTF() ([]byte, error) { return cocoa.SystemFontTTF() }
