// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !darwin && !windows

package window

// systemFontTTF reports that there is no font file to hand over, with the same
// error and for the same reason as the [AppearanceReader] method: a Linux
// desktop names a font family rather than providing one, and a browser has no
// font file to read at all. See [SystemFontTTF].
func systemFontTTF() ([]byte, error) { return nil, errNoSystemFont }
