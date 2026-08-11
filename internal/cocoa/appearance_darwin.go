// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package cocoa

import (
	"os"
	"strings"

	"github.com/go-macos/objc"
)

// sfFontPath is the macOS system face. It is a variable so a test can point it
// somewhere else without a real system font.
var sfFontPath = "/System/Library/Fonts/SFNS.ttf"

var (
	selEffectiveAppearance = objc.RegisterName("effectiveAppearance")
	selApprName            = objc.RegisterName("name")
	selControlAccentColor  = objc.RegisterName("controlAccentColor")
	selRespondsToSelector  = objc.RegisterName("respondsToSelector:")
	selSRGBColorSpace      = objc.RegisterName("sRGBColorSpace")
	selColorUsingSpace     = objc.RegisterName("colorUsingColorSpace:")
	selRedComponent        = objc.RegisterName("redComponent")
	selGreenComponent      = objc.RegisterName("greenComponent")
	selBlueComponent       = objc.RegisterName("blueComponent")
)

// AppearanceRaw reports the live macOS look in primitive values: the effective
// dark/light mode from -[NSApp effectiveAppearance], and the user's accent from
// +[NSColor controlAccentColor] converted to sRGB.
//
// It hands back plain values rather than the window package's Appearance struct
// because that struct is declared where the capability is declared, and this
// package is imported BY that one -- naming it here would be a cycle. The
// parent wraps these into the public shape (see open_darwin.go).
//
// The accent is asked for through respondsToSelector: rather than assumed: the
// selector arrived in 10.14, and sending it blind to an older system would
// crash rather than degrade. hasAccent false means "the system did not say",
// which an app should read as "use your own", not as black.
func (w *Window) AppearanceRaw() (dark bool, r, g, b uint8, hasAccent bool) {
	if err := loadFrameworks(); err != nil {
		return false, 0, 0, 0, false
	}
	if appr := objc.ID(objc.GetClass("NSApplication")).Send(selSharedApplication).Send(selEffectiveAppearance); appr != 0 {
		dark = strings.Contains(objc.GoString(appr.Send(selApprName)), "Dark")
	}
	colorClass := objc.ID(objc.GetClass("NSColor"))
	if colorClass.Send(selRespondsToSelector, selControlAccentColor) == 0 {
		return dark, 0, 0, 0, false
	}
	c := colorClass.Send(selControlAccentColor)
	if c == 0 {
		return dark, 0, 0, 0, false
	}
	srgb := objc.ID(objc.GetClass("NSColorSpace")).Send(selSRGBColorSpace)
	if c = c.Send(selColorUsingSpace, srgb); c == 0 {
		return dark, 0, 0, 0, false
	}
	return dark,
		unitToByte(objc.Send[float64](c, selRedComponent)),
		unitToByte(objc.Send[float64](c, selGreenComponent)),
		unitToByte(objc.Send[float64](c, selBlueComponent)),
		true
}

// SystemFontTTF returns the raw bytes of the macOS system face. Implements the
// window.AppearanceReader capability.
func (w *Window) SystemFontTTF() ([]byte, error) { return os.ReadFile(sfFontPath) }
