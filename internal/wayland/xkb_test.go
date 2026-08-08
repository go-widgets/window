// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package wayland

import "testing"

const kmText = `
// leading line comment
xkb_keymap {
xkb_keycodes "evdev" {
    minimum = 8;
    maximum = 255;
    <AC01> = 38;
    <RTRN> = 36;
    <LFSH> = 50;
    <SPCE> = 65;
    <AE01> = 10;
    <HUGE> = 99999999999;
};
xkb_types "x" { /* block comment */ };
xkb_symbols "pc" {
    key <AC01> { [ a, A ] };
    key <RTRN> { [ Return ] };
    key <LFSH> { [ Shift_L ] };
    key <SPCE> { [ space ] };
    key <AE01> { type[Group1]="FOUR", symbols[Group1] = [ 1, exclam ] };
    key <NOPE> { [ b ] };
    key <NIL> { type[Group1]="ONE" };
};
};
`

func TestParseKeymapLookup(t *testing.T) {
	km := ParseKeymap(kmText)
	cases := []struct {
		evdev      uint32
		shift      bool
		wantRune   rune
		wantName   string
		wantMod    bool
		wantNothin bool
	}{
		{30, false, 'a', "", false, false},   // <AC01> level 0
		{30, true, 'A', "", false, false},    // <AC01> level 1
		{28, false, 0, "Enter", false, false}, // <RTRN> named
		{28, true, 0, "Enter", false, false},  // single level: shift ignored
		{42, false, 0, "", true, false},       // <LFSH> Shift_L modifier
		{57, false, ' ', "", false, false},    // <SPCE> space rune
		{2, false, '1', "", false, false},     // <AE01> level 0
		{2, true, '!', "", false, false},      // <AE01> exclam
		{200, false, 0, "", false, true},      // unknown keycode
	}
	for _, c := range cases {
		k := km.Lookup(c.evdev, c.shift)
		if c.wantNothin {
			if k.HasRune || k.Name != "" || k.IsModifier {
				t.Errorf("Lookup(%d) = %+v, want nothing", c.evdev, k)
			}
			continue
		}
		if c.wantMod {
			if !k.IsModifier {
				t.Errorf("Lookup(%d) not modifier", c.evdev)
			}
			continue
		}
		if c.wantName != "" {
			if k.Name != c.wantName {
				t.Errorf("Lookup(%d) name = %q, want %q", c.evdev, k.Name, c.wantName)
			}
			continue
		}
		if !k.HasRune || k.Rune != c.wantRune {
			t.Errorf("Lookup(%d,shift=%v) rune = %q, want %q", c.evdev, c.shift, k.Rune, c.wantRune)
		}
	}
}

func TestParseKeymapEmpty(t *testing.T) {
	km := ParseKeymap("")
	if k := km.Lookup(30, false); k.HasRune || k.Name != "" || k.IsModifier {
		t.Errorf("empty keymap Lookup = %+v, want nothing", k)
	}
}

func TestResolveKeysym(t *testing.T) {
	if k := resolveKeysym("Shift_L"); !k.IsModifier {
		t.Error("Shift_L should be modifier")
	}
	if k := resolveKeysym("BackSpace"); k.Name != "Backspace" {
		t.Errorf("BackSpace -> %q", k.Name)
	}
	if k := resolveKeysym("period"); !k.HasRune || k.Rune != '.' {
		t.Errorf("period -> %q", k.Rune)
	}
	if k := resolveKeysym("Z"); !k.HasRune || k.Rune != 'Z' {
		t.Errorf("Z -> %q", k.Rune)
	}
	if k := resolveKeysym("7"); !k.HasRune || k.Rune != '7' {
		t.Errorf("7 -> %q", k.Rune)
	}
	// A single non-alphanumeric char name is not resolvable to a rune here.
	if k := resolveKeysym("$"); k.HasRune {
		t.Errorf("$ single-char should not resolve, got %q", k.Rune)
	}
	// An unknown multi-char symbol resolves to nothing.
	if k := resolveKeysym("Foobar"); k.HasRune || k.Name != "" || k.IsModifier {
		t.Errorf("Foobar -> %+v, want nothing", k)
	}
}

func TestSectionBody(t *testing.T) {
	if got := sectionBody("no keyword here", "xkb_symbols"); got != "" {
		t.Errorf("missing keyword -> %q", got)
	}
	if got := sectionBody("xkb_symbols no brace", "xkb_symbols"); got != "" {
		t.Errorf("missing brace -> %q", got)
	}
	if got := sectionBody("xkb_symbols { a { b } c }", "xkb_symbols"); got != " a { b } c " {
		t.Errorf("nested braces -> %q", got)
	}
	// Unbalanced: no closing brace returns the remainder.
	if got := sectionBody("xkb_symbols { unclosed", "xkb_symbols"); got != " unclosed" {
		t.Errorf("unbalanced -> %q", got)
	}
}

func TestFirstSymbolList(t *testing.T) {
	if got := firstSymbolList("type[Group1]=\"X\""); got != nil {
		t.Errorf("index-only body -> %v, want nil", got)
	}
	if got := firstSymbolList("no brackets"); got != nil {
		t.Errorf("no brackets -> %v", got)
	}
	if got := firstSymbolList("[]"); got != nil {
		t.Errorf("empty brackets -> %v, want nil", got)
	}
	got := firstSymbolList("[ a , B ]")
	if len(got) != 2 || got[0] != "a" || got[1] != "B" {
		t.Errorf("symbol list -> %v", got)
	}
}
