// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package window

import "github.com/go-widgets/window/internal/win32"

// systemFontTTF reads the Windows UI face. See [SystemFontTTF].
func systemFontTTF() ([]byte, error) { return win32.SystemFontTTF() }
