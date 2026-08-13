// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import "image/color"

// Appearance is the host UI's look: what the user has told their system they
// want everything to look like.
//
// A go-widgets app picks its own theme, which is right for an app with a
// designed identity and wrong for one that should feel native. An app that
// wants to belong on the desktop it is running on needs to know that the user
// chose dark mode and picked purple as their accent, and no amount of theming
// inside the toolkit can discover that: it is a platform fact.
type Appearance struct {
	// Dark is the effective dark/light mode.
	Dark bool
	// Accent is the user's accent colour, meaningful only when HasAccent is
	// set. A system too old to have the notion, or one where the user made no
	// choice, reports HasAccent false rather than a made-up colour.
	Accent    color.RGBA
	HasAccent bool
}

// AppearanceReader is an optional [Backend] capability: reading the host look.
//
// Appearance is cheap enough to poll -- a handful of platform queries, no
// allocation -- so a back-end need not push changes and an app can simply ask
// each frame and act when the answer differs. That keeps the seam a plain
// question instead of a callback with a lifetime.
//
// SystemFontTTF is separate precisely because it is NOT cheap: the macOS system
// face is tens of megabytes on disk, so it is asked for once at startup, not on
// every poll. It returns the raw sfnt bytes, ready for
// toolkit.NewTrueTypeFont, and an error when the platform has no such file to
// offer.
//
// Implemented by macOS (Cocoa), Windows (the registry's personalisation keys)
// and both Linux back-ends, which read the same XDG desktop portal — the desktop
// look is not a property of the display server carrying the pixels.
type AppearanceReader interface {
	Appearance() Appearance
	SystemFontTTF() ([]byte, error)
}
