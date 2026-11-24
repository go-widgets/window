// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package gtk

import (
	"strings"
	"testing"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

func TestPreeditFromBoundsWhatTheContextReports(t *testing.T) {
	for _, tc := range []struct {
		name    string
		text    string
		cursor  int
		wantTxt string
		wantCur int
	}{
		{"none", "", 3, "", -1},
		{"in range", "かんじ", 2, "かんじ", 2},
		{"past the end", "ab", 9, "ab", 2},
		{"negative", "ab", -5, "ab", -1},
	} {
		p := preeditFrom(tc.text, tc.cursor)
		if p.Text != tc.wantTxt || p.Cursor != tc.wantCur {
			t.Errorf("%s: got %q cursor %d, want %q cursor %d", tc.name, p.Text, p.Cursor, tc.wantTxt, tc.wantCur)
		}
	}
	long := strings.Repeat("あ", toolkit.MaxPreeditRunes+50)
	if n := len([]rune(preeditFrom(long, 0).Text)); n != toolkit.MaxPreeditRunes {
		t.Errorf("a %d-rune preedit kept %d runes, want the bound %d", len([]rune(long)), n, toolkit.MaxPreeditRunes)
	}
}

// TestComposeIntoFeedsTheFocusedField drives the toolkit side the GTK signals
// call: a preedit shows in the focused Entry without touching its value, a commit
// replaces it, an empty preedit cancels, and with nothing focused the fallback is
// the key events the commit stands for.
func TestComposeIntoFeedsTheFocusedField(t *testing.T) {
	entry := toolkit.NewEntry("")
	entry.SetFocused(true)
	root := toolkit.NewVBox()
	root.Append(entry)
	// A field with no bounds is hidden and composes nothing: lay the tree out.
	buf := make([]byte, 300*100*4)
	root.SetBounds(toolkit.Rect{W: 300, H: 100})
	root.Draw(painter.NewPixelPainter(buf, 300, 100), toolkit.DefaultDark())

	if !setPreedit(root, preeditFrom("^", 1)) {
		t.Fatal("no field took the preedit")
	}
	if entry.Value() != "" || entry.Preedit().Text != "^" {
		t.Fatalf("after a preedit: value %q preedit %q", entry.Value(), entry.Preedit().Text)
	}
	if !composeInto(root, "ê") || entry.Value() != "ê" || entry.Preedit().Text != "" {
		t.Fatalf("after the commit: value %q preedit %q", entry.Value(), entry.Preedit().Text)
	}
	setPreedit(root, preeditFrom("x", 1))
	setPreedit(root, preeditFrom("", 0))
	if entry.Preedit().Text != "" || entry.Value() != "ê" {
		t.Errorf("an empty preedit left value %q preedit %q", entry.Value(), entry.Preedit().Text)
	}

	entry.SetFocused(false)
	if composeInto(root, "z") || setPreedit(root, preeditFrom("z", 1)) {
		t.Error("an unfocused field took a composition")
	}
}

func TestCommitFallbackIsKeyDownAndCharPerRuneAndBounded(t *testing.T) {
	evs := commitFallback("éa")
	want := []toolkit.Event{
		{Kind: toolkit.EventKeyDown, Code: "é"}, {Kind: toolkit.EventChar, Code: "é"},
		{Kind: toolkit.EventKeyDown, Code: "a"}, {Kind: toolkit.EventChar, Code: "a"},
	}
	if len(evs) != len(want) {
		t.Fatalf("got %d events, want %d", len(evs), len(want))
	}
	for i := range want {
		if evs[i] != want[i] {
			t.Errorf("event %d = %+v, want %+v", i, evs[i], want[i])
		}
	}
	if n := len(commitFallback(strings.Repeat("a", toolkit.MaxCommitRunes*2))); n != 2*toolkit.MaxCommitRunes {
		t.Errorf("an oversized commit made %d events, want %d", n, 2*toolkit.MaxCommitRunes)
	}
}

func TestCaretRectConvertsPixelsToPoints(t *testing.T) {
	x, y, w, h := caretRect(toolkit.Rect{X: 40, Y: 20, W: 0, H: 32}, 2)
	if x != 20 || y != 10 || w != 1 || h != 16 {
		t.Errorf("got %d,%d %dx%d, want 20,10 1x16 (a collapsed caret is one point wide)", x, y, w, h)
	}
	if x, _, _, _ := caretRect(toolkit.Rect{X: 40}, 0); x != 40 {
		t.Errorf("scale 0 must act as 1, got x=%d", x)
	}
}
