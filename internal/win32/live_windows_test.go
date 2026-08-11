// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows && integration

// The live Windows proof, run on a real Windows 11 ARM64 machine.
//
// Both directions of the clipboard are witnessed by PowerShell rather than by
// our own reader: what Go writes is read back with Get-Clipboard, and what
// Set-Clipboard writes is read back by Go. Checking our writer against our own
// reader would only prove the two halves agree with each other, which they
// would even if both had the byte order wrong.
//
// The appearance is witnessed by the registry values themselves, read through
// PowerShell -- a different route to the same fact than the syscall path under
// test.
package win32

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// fmtSscan and psQuote keep the noise out of the tests above.
func fmtSscan(s string, v *uint32) (int, error) { return fmt.Sscanf(s, "%d", v) }

// psQuote wraps a string as a PowerShell single-quoted literal.
func psQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", "''") + "'" }

func skipUnlessLive(t *testing.T) {
	t.Helper()
	if os.Getenv("WINDOW_WIN32_INTEGRATION") == "" {
		t.Skip("set WINDOW_WIN32_INTEGRATION=1 to run the live Windows proof")
	}
}

// powershell runs a script and returns its output as UTF-8.
//
// The encoding line is not decoration. Without it PowerShell writes its output
// in the console's OEM code page, which turned "accentué, 日本語" into
// "accentu\x82, ???" on the way back -- and the first run of this test read
// that as a clipboard bug. The witness was mangling the evidence; the clipboard
// was fine.
func powershell(t *testing.T, script string) string {
	t.Helper()
	const utf8Out = "[Console]::OutputEncoding=[System.Text.Encoding]::UTF8; "
	out, err := exec.Command("powershell", "-NoProfile", "-NonInteractive",
		"-Command", utf8Out+script).Output()
	if err != nil {
		t.Fatalf("powershell %q: %v", script, err)
	}
	return strings.TrimRight(string(out), "\r\n")
}

func TestLiveWin32ClipboardRoundTrip(t *testing.T) {
	skipUnlessLive(t)
	w := &Window{}

	before := powershell(t, "Get-Clipboard -Raw")
	t.Cleanup(func() {
		// Set-Clipboard rejects an empty value, so an empty board is restored
		// by emptying it rather than by writing nothing to it.
		if before == "" {
			powershell(t, "Add-Type -AssemblyName System.Windows.Forms; [System.Windows.Forms.Clipboard]::Clear()")
			return
		}
		powershell(t, "Set-Clipboard -Value "+psQuote(before))
	})

	// Go -> clipboard, witnessed by Get-Clipboard.
	const written = "go-widgets win32 proof — accentué, 日本語, 🎯"
	w.SetClipboardText(written)
	if got := powershell(t, "Get-Clipboard -Raw"); got != written {
		t.Errorf("Get-Clipboard read %q, we wrote %q", got, written)
	}

	// clipboard -> Go, witnessed by Set-Clipboard.
	const external = "written by Set-Clipboard — 中文, ß, 🌍"
	powershell(t, "Set-Clipboard -Value "+psQuote(external))
	if got := w.ClipboardText(); got != external {
		t.Errorf("ClipboardText read %q, Set-Clipboard wrote %q", got, external)
	}

	// An empty string is a value, not an absence.
	w.SetClipboardText("")
	if got := w.ClipboardText(); got != "" {
		t.Errorf("after writing an empty string, ClipboardText read %q", got)
	}
}

// A clipboard holding something that is not text reads as "", not as stale
// text and not as an error.
func TestLiveWin32ClipboardNonText(t *testing.T) {
	skipUnlessLive(t)
	w := &Window{}

	w.SetClipboardText("sentinel that must not survive")
	powershell(t, "Add-Type -AssemblyName System.Windows.Forms; "+
		"[System.Windows.Forms.Clipboard]::SetImage((New-Object System.Drawing.Bitmap 4,4))")
	if got := w.ClipboardText(); got != "" {
		t.Errorf("an image-only clipboard read as text %q", got)
	}
}

func TestLiveWin32Appearance(t *testing.T) {
	skipUnlessLive(t)
	w := &Window{}
	dark, r, g, b, hasAccent := w.AppearanceRaw()

	// The registry is the setting itself: a different route to the same fact.
	light := powershell(t, `(Get-ItemProperty -Path 'HKCU:\Software\Microsoft\Windows\CurrentVersion\Themes\Personalize' -Name AppsUseLightTheme -ErrorAction SilentlyContinue).AppsUseLightTheme`)
	wantDark := light == "0"
	if dark != wantDark {
		t.Errorf("AppearanceRaw says dark=%v, AppsUseLightTheme=%q says dark=%v", dark, light, wantDark)
	}

	accent := powershell(t, `(Get-ItemProperty -Path 'HKCU:\Software\Microsoft\Windows\DWM' -Name AccentColor -ErrorAction SilentlyContinue).AccentColor`)
	if accent == "" {
		if hasAccent {
			t.Error("no AccentColor in the registry, yet an accent was reported")
		}
		// A machine with no accent never exercises the byte order, and the byte
		// order is the part that fails quietly: AccentColor is 0xAABBGGRR, so
		// reading it as RGBA swaps red and blue and still looks like a colour.
		// So one is set, read back, and removed again.
		t.Log("no accent on this machine; setting one to exercise the byte order")
		powershell(t, `New-ItemProperty -Path 'HKCU:\Software\Microsoft\Windows\DWM' -Name AccentColor -PropertyType DWord -Value 0xFF0080FF -Force | Out-Null`)
		t.Cleanup(func() {
			powershell(t, `Remove-ItemProperty -Path 'HKCU:\Software\Microsoft\Windows\DWM' -Name AccentColor -ErrorAction SilentlyContinue`)
		})
		_, r2, g2, b2, has2 := w.AppearanceRaw()
		if !has2 {
			t.Fatal("an accent was written to the registry and none was read back")
		}
		// 0xFF0080FF is A=FF B=00 G=80 R=FF: opaque, red 255, green 128, blue 0.
		// Read as RGBA it would come back as 255,128,0 with red and blue
		// swapped -- 0,128,255 -- which is why each component is named.
		if r2 != 255 || g2 != 128 || b2 != 0 {
			t.Errorf("accent = %d,%d,%d from 0xFF0080FF, want 255,128,0 (AABBGGRR, not RGBA)", r2, g2, b2)
		}
		t.Logf("byte order confirmed: 0xFF0080FF -> r=%d g=%d b=%d", r2, g2, b2)
		return
	}
	if !hasAccent {
		t.Fatalf("AccentColor=%s in the registry, yet no accent was reported", accent)
	}
	// AccentColor is 0xAABBGGRR: reading it as RGBA swaps red and blue, which
	// looks plausible and is wrong, so the components are checked by name.
	var raw uint32
	if _, err := fmtSscan(accent, &raw); err != nil {
		t.Fatalf("parsing %q: %v", accent, err)
	}
	wantR, wantG, wantB := uint8(raw&0xFF), uint8((raw>>8)&0xFF), uint8((raw>>16)&0xFF)
	if r != wantR || g != wantG || b != wantB {
		t.Errorf("accent = %d,%d,%d, registry 0x%08X says %d,%d,%d", r, g, b, raw, wantR, wantG, wantB)
	}
	t.Logf("dark=%v accent=#%02X%02X%02X (registry 0x%08X)", dark, r, g, b, raw)
}

func TestLiveWin32SystemFont(t *testing.T) {
	skipUnlessLive(t)
	ttf, err := (&Window{}).SystemFontTTF()
	if err != nil {
		t.Fatalf("SystemFontTTF: %v", err)
	}
	if len(ttf) < 1024 {
		t.Fatalf("the system font is %d bytes", len(ttf))
	}
	// A TrueType/OpenType file starts with one of these four signatures; bytes
	// alone would prove only that a file was read.
	sig := string(ttf[:4])
	if sig != "\x00\x01\x00\x00" && sig != "OTTO" && sig != "true" && sig != "ttcf" {
		t.Errorf("the system font does not start with an sfnt signature: % x", ttf[:4])
	}
	t.Logf("system font: %d bytes, signature % x", len(ttf), ttf[:4])
}
