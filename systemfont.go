// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

// SystemFontTTF returns the raw sfnt bytes of the host's UI face, ready for
// toolkit.NewTrueTypeFont, and an error when the platform has no such file to
// offer.
//
// It is the same answer as the [AppearanceReader] method of that name, asked
// WITHOUT a window — which is the whole reason it exists. An application that
// wants to be drawn in the system face has to install that font before it lays
// anything out, because the font decides how tall a line of text is and
// therefore how tall the window has to be. Reaching the method means opening
// the window first, so the size would have to be computed from the font the
// window is not going to use. Every one of these back-ends reads a file, which
// needs no window and no display server, so the question is answerable on the
// way in.
//
// It is NOT cheap -- the macOS system face is tens of megabytes -- so it is
// asked once at startup, unlike [Appearance], which is cheap enough to poll.
//
// macOS answers from /System/Library/Fonts, Windows from the Segoe UI file, and
// both Linux back-ends report an error: a Linux desktop names a font family and
// leaves finding it to fontconfig, which is a font library's job and not a
// window's.
func SystemFontTTF() ([]byte, error) { return systemFontTTF() }
