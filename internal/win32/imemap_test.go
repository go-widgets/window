// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package win32

import (
	"reflect"
	"testing"
	"unicode/utf16"

	"github.com/go-widgets/toolkit"
)

func u16(s string) []uint16 { return utf16.Encode([]rune(s)) }

func TestBuildPreeditMapsAttributesToStyles(t *testing.T) {
	// かんじ: the first two kana converted, the last still being typed, the caret
	// after the second one.
	p := buildPreedit(u16("かんじ"), 2, []byte{attrConverted, attrConverted, attrTargetNotConverted})
	want := toolkit.Preedit{Text: "かんじ", Cursor: 2, Segments: []toolkit.PreeditSegment{
		{Start: 0, End: 2, Style: toolkit.PreeditConverted},
		{Start: 2, End: 3, Style: toolkit.PreeditTarget},
	}}
	if !reflect.DeepEqual(p, want) {
		t.Errorf("got %+v, want %+v", p, want)
	}
	// Plain input carries no segment: it is the default style.
	if p := buildPreedit(u16("ab"), 2, []byte{attrInput, attrInputError}); len(p.Segments) != 0 {
		t.Errorf("input and error attributes made segments %+v", p.Segments)
	}
	// No attribute array (an IME that reports none) is plain input too.
	if p := buildPreedit(u16("ab"), 1, nil); p.Text != "ab" || p.Cursor != 1 || len(p.Segments) != 0 {
		t.Errorf("no attributes: %+v", p)
	}
}

func TestBuildPreeditCountsRunesNotUTF16Units(t *testing.T) {
	// U+20BB7 is a surrogate pair: two units, one rune. A caret after it is unit 2
	// and rune 1; the attribute run covering both units is one rune wide.
	p := buildPreedit(u16("\U00020BB7a"), 2, []byte{attrTargetConverted, attrTargetConverted, attrInput})
	if p.Cursor != 1 {
		t.Errorf("cursor after a surrogate pair = %d, want 1", p.Cursor)
	}
	want := []toolkit.PreeditSegment{{Start: 0, End: 1, Style: toolkit.PreeditTarget}}
	if !reflect.DeepEqual(p.Segments, want) {
		t.Errorf("segments = %+v, want %+v", p.Segments, want)
	}
	// A caret between the two units of a pair lands on the rune, not past it.
	if p := buildPreedit(u16("\U00020BB7"), 1, nil); p.Cursor != 0 {
		t.Errorf("cursor inside a pair = %d, want 0", p.Cursor)
	}
}

func TestBuildPreeditIsBoundedAndNeverPanics(t *testing.T) {
	if p := buildPreedit(nil, 0, nil); p.Text != "" || p.Cursor != -1 {
		t.Errorf("empty composition = %+v", p)
	}
	if p := buildPreedit(u16("ab"), -1, nil); p.Cursor != -1 {
		t.Errorf("negative caret = %d, want none", p.Cursor)
	}
	if p := buildPreedit(u16("ab"), 99, nil); p.Cursor != -1 {
		t.Errorf("caret past the end = %d, want none", p.Cursor)
	}
	// An attribute array longer than the text, and a lone surrogate.
	buildPreedit([]uint16{0xD800}, 1, []byte{1, 1, 1, 1})
	huge := make([]uint16, 100000)
	for i := range huge {
		huge[i] = 'x'
	}
	attrs := make([]byte, len(huge))
	for i := range attrs {
		attrs[i] = byte(i % 6)
	}
	p := buildPreedit(huge, 50000, attrs)
	if n := len([]rune(p.Text)); n > toolkit.MaxPreeditRunes {
		t.Errorf("preedit kept %d runes, bound is %d", n, toolkit.MaxPreeditRunes)
	}
	if len(p.Segments) > toolkit.MaxPreeditSegments {
		t.Errorf("preedit kept %d segments, bound is %d", len(p.Segments), toolkit.MaxPreeditSegments)
	}
}

func TestCommitFromReplacesALoneSurrogate(t *testing.T) {
	if got := commitFrom([]uint16{'a', 0xD800, 'b'}); got != "a�b" {
		t.Errorf("got %q", got)
	}
	if got := commitFrom(u16("漢字")); got != "漢字" {
		t.Errorf("got %q", got)
	}
}

func TestCommitFromIsBounded(t *testing.T) {
	units := make([]uint16, maxImeUnits*8+500)
	for i := range units {
		units[i] = 'x'
	}
	if got := commitFrom(units); len(got) != maxImeUnits*8 {
		t.Errorf("kept %d units, bound is %d", len(got), maxImeUnits*8)
	}
}

func FuzzBuildPreedit(f *testing.F) {
	f.Add([]byte("a\x00b\x00"), 1, []byte{1, 2})
	f.Add([]byte{0x00, 0xD8, 0x00, 0xDC}, 2, []byte{3, 3})
	f.Fuzz(func(t *testing.T, raw []byte, cursor int, attrs []byte) {
		units := make([]uint16, len(raw)/2)
		for i := range units {
			units[i] = uint16(raw[2*i]) | uint16(raw[2*i+1])<<8
		}
		p := buildPreedit(units, cursor, attrs)
		n := len([]rune(p.Text))
		if n > toolkit.MaxPreeditRunes || p.Cursor < -1 || p.Cursor > n {
			t.Fatalf("unbounded preedit: %d runes, cursor %d", n, p.Cursor)
		}
		for _, s := range p.Segments {
			if s.Start < 0 || s.End > n || s.Start >= s.End {
				t.Fatalf("segment %+v outside %d runes", s, n)
			}
		}
		commitFrom(units)
	})
}
