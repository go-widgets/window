// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin && integration

// This is the live macOS proof for the pasteboard capability. It runs only
// under -tags=integration with WINDOW_COCOA_INTEGRATION set, because it touches
// the REAL pasteboard of the machine it runs on.
//
// Both directions are witnessed by something other than the code under test:
// what Go writes is read back by pbpaste, and what pbcopy writes is read back
// by Go. Reading back through our own reader would only prove the two halves
// agree with each other, which they would even if both spoke the wrong UTI.
//
// The pasteboard's previous contents are saved and restored, so running the
// suite does not cost the user their clipboard.
package cocoa

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func skipUnlessIntegration(t *testing.T) {
	t.Helper()
	if os.Getenv("WINDOW_COCOA_INTEGRATION") == "" {
		t.Skip("set WINDOW_COCOA_INTEGRATION=1 to run the live pasteboard proof")
	}
}

func pbpaste(t *testing.T) string {
	t.Helper()
	out, err := exec.Command("pbpaste").Output()
	if err != nil {
		t.Fatalf("pbpaste: %v", err)
	}
	return string(out)
}

func pbcopy(t *testing.T, s string) {
	t.Helper()
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(s)
	if err := cmd.Run(); err != nil {
		t.Fatalf("pbcopy: %v", err)
	}
}

func TestLiveClipboardRoundTripsThroughTheRealPasteboard(t *testing.T) {
	skipUnlessIntegration(t)

	w := &Window{}
	restore := pbpaste(t)
	t.Cleanup(func() { pbcopy(t, restore) })

	// Go -> pasteboard, witnessed by pbpaste.
	const written = "go-widgets/window clipboard proof — accentué, 日本語, 🎯"
	w.SetClipboardText(written)
	if got := pbpaste(t); got != written {
		t.Errorf("pbpaste read %q, we wrote %q", got, written)
	}

	// pasteboard -> Go, witnessed by pbcopy.
	const external = "written by pbcopy — 中文, ß, 🌍"
	pbcopy(t, external)
	if got := w.ClipboardText(); got != external {
		t.Errorf("ClipboardText read %q, pbcopy wrote %q", got, external)
	}

	// An empty string is a value, not an absence: it must land and read back.
	w.SetClipboardText("")
	if got := w.ClipboardText(); got != "" {
		t.Errorf("after writing an empty string, ClipboardText read %q", got)
	}

	// A board holding no text at all reads as "" rather than stale text. Copying
	// a PNG through NSPasteboard is the platform's own way of getting there.
	pbcopy(t, "sentinel that must not survive")
	if err := exec.Command("osascript", "-e",
		`set the clipboard to (read (POSIX file "/System/Library/CoreServices/DefaultDesktop.heic") as JPEG picture)`).Run(); err == nil {
		if got := w.ClipboardText(); got != "" {
			t.Errorf("a picture-only pasteboard read as text %q", got)
		}
	} else {
		t.Log("skipped the picture-only case: osascript could not set an image")
	}
}
