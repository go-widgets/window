// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"encoding/binary"
	"runtime"
	"testing"
)

// TestSystemFontTTFNeedsNoWindow is the whole point of the package-level
// function: it is asked before anything is opened, on a machine with no display
// server at all, because an application has to install the face before it can
// know how tall its window must be.
func TestSystemFontTTFNeedsNoWindow(t *testing.T) {
	ttf, err := SystemFontTTF()

	switch runtime.GOOS {
	case "darwin", "windows":
		if err != nil {
			// A stripped image may genuinely not carry the face; that is a
			// property of the machine, not a defect in the forwarding.
			t.Skipf("this %s has no system font file: %v", runtime.GOOS, err)
		}
		if len(ttf) == 0 {
			t.Fatal("no error and no bytes either")
		}
		// Prove it is a font and not, say, a plist that happened to be there:
		// an sfnt begins with one of four documented version tags.
		tag := binary.BigEndian.Uint32(ttf[:4])
		switch tag {
		case 0x00010000, 0x74727565, 0x4F54544F, 0x74746366: // 1.0, true, OTTO, ttcf
		default:
			t.Fatalf("the first four bytes are %#08x, which is not an sfnt tag", tag)
		}
	default:
		if err == nil {
			t.Fatalf("%s has no font file to offer, yet %d bytes came back",
				runtime.GOOS, len(ttf))
		}
		if ttf != nil {
			t.Fatalf("an error AND %d bytes: a caller cannot tell which to trust", len(ttf))
		}
	}
}

// TestSystemFontTTFAnswersLikeTheCapability pins the two seams together. They
// are separate functions -- one takes a window, one does not -- and a person
// switching between them is entitled to the same answer from both, or the
// package has two notions of "the system font".
func TestSystemFontTTFAnswersLikeTheCapability(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "js" {
		// Elsewhere the method needs a real window, which lives in the live
		// tests; here the shared error value is what can be compared without
		// opening anything.
		t.Skipf("the capability needs an open window on %s", runtime.GOOS)
	}
	_, err := SystemFontTTF()
	if err != errNoSystemFont {
		t.Fatalf("package-level error is %v, the capability's is %v", err, errNoSystemFont)
	}
}
