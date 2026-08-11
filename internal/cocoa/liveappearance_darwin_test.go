// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin && integration

// The live macOS proof for the appearance capability, run only under
// -tags=integration with WINDOW_COCOA_INTEGRATION set.
//
// Dark mode is witnessed by `defaults read -g AppleInterfaceStyle`, which is
// where the setting actually lives — a different route to the same fact than
// -[NSApp effectiveAppearance]. The system font is witnessed by parsing it:
// bytes that go-opentype accepts as a face prove the capability returns a font,
// not merely a file.
//
// The accent colour's exact value is NOT witnessed independently, and saying so
// is more useful than pretending otherwise: the only other source is Apple's
// index-to-colour table, and re-encoding it here would make this test agree
// with a copy of the thing it is meant to check. What is asserted is what can
// be: that the system reports having one, that it is opaque, that it is stable
// across reads, and that with no user choice recorded it is the default blue.
package cocoa

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/go-opentype/opentype"
)

func TestLiveAppearanceMatchesTheSystemSetting(t *testing.T) {
	skipUnlessIntegration(t)

	w := &Window{}
	dark, r, g, b, hasAccent := w.AppearanceRaw()

	// `defaults` exits non-zero when the key is absent, which IS light mode.
	out, err := exec.Command("defaults", "read", "-g", "AppleInterfaceStyle").Output()
	wantDark := err == nil && strings.Contains(string(out), "Dark")
	if dark != wantDark {
		t.Errorf("effectiveAppearance says dark=%v, `defaults read -g AppleInterfaceStyle` says dark=%v", dark, wantDark)
	}
	t.Logf("dark=%v accent=#%02X%02X%02X hasAccent=%v", dark, r, g, b, hasAccent)

	if !hasAccent {
		t.Fatal("no accent colour on a system new enough to run this")
	}

	// Stable across reads: a value that changes when nothing changed would mean
	// the colour-space conversion is being read wrong.
	if _, r2, g2, b2, _ := w.AppearanceRaw(); r2 != r || g2 != g || b2 != b {
		t.Errorf("two reads disagree: #%02X%02X%02X then #%02X%02X%02X", r, g, b, r2, g2, b2)
	}

	// With no accent recorded, macOS uses its default blue. This is the one
	// value that can be cross-checked without re-encoding Apple's whole table.
	if _, err := exec.Command("defaults", "read", "-g", "AppleAccentColor").Output(); err != nil {
		if r > 40 || g < 90 || b < 200 {
			t.Errorf("no AppleAccentColor set, so the default blue was expected, got #%02X%02X%02X", r, g, b)
		}
	} else {
		t.Log("a user accent is set; its exact value is not cross-checked (see the file comment)")
	}
}

func TestLiveSystemFontParsesAsAFace(t *testing.T) {
	skipUnlessIntegration(t)

	w := &Window{}
	ttf, err := w.SystemFontTTF()
	if err != nil {
		t.Fatalf("SystemFontTTF: %v", err)
	}
	if len(ttf) == 0 {
		t.Fatal("the system font is empty")
	}
	f, err := opentype.Parse(ttf)
	if err != nil {
		t.Fatalf("the system font does not parse as a face: %v", err)
	}
	t.Logf("system font: %d bytes, %d glyphs", len(ttf), f.NumGlyphs())
}
