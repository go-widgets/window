// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package cocoa

import (
	"strings"
	"testing"

	"github.com/go-widgets/toolkit"
)

func TestUTF16LenAndRuneOffset(t *testing.T) {
	// "a😀é": 1 + 2 + 1 UTF-16 units, 3 runes.
	s := "a😀é"
	if got := utf16Len(s); got != 4 {
		t.Errorf("utf16Len = %d, want 4", got)
	}
	for _, tc := range []struct {
		u16  uint64
		want int
	}{{0, 0}, {1, 1}, {2, 1}, {3, 2}, {4, 3}, {99, 3}} {
		if got := runeOffset(s, tc.u16); got != tc.want {
			t.Errorf("runeOffset(%d) = %d, want %d", tc.u16, got, tc.want)
		}
	}
}

func TestBuildPreedit(t *testing.T) {
	// Kana: "かんじ" with the first two converted (single underline) and the
	// last the target clause (thick), cursor after the second character.
	p := buildPreedit("かんじ", nsRange{2, 0}, []imeClause{
		{0, 2, 1}, {2, 1, 2}, {0, 0, 1}, {nsNotFound, 1, 1},
	})
	if p.Text != "かんじ" || p.Cursor != 2 {
		t.Fatalf("text/cursor = %q/%d", p.Text, p.Cursor)
	}
	want := []toolkit.PreeditSegment{
		{Start: 0, End: 2, Style: toolkit.PreeditConverted},
		{Start: 2, End: 3, Style: toolkit.PreeditTarget},
	}
	if len(p.Segments) != len(want) {
		t.Fatalf("segments = %+v, want %+v", p.Segments, want)
	}
	for i := range want {
		if p.Segments[i] != want[i] {
			t.Errorf("segment %d = %+v, want %+v", i, p.Segments[i], want[i])
		}
	}
}

func TestBuildPreeditCursorAbsent(t *testing.T) {
	if p := buildPreedit("ab", nsRange{nsNotFound, 0}, nil); p.Cursor != -1 {
		t.Errorf("NSNotFound cursor = %d, want -1", p.Cursor)
	}
	if p := buildPreedit("ab", nsRange{9, 0}, nil); p.Cursor != -1 {
		t.Errorf("out-of-range cursor = %d, want -1", p.Cursor)
	}
}

func TestBuildPreeditIsBounded(t *testing.T) {
	long := strings.Repeat("あ", toolkit.MaxPreeditRunes*3)
	var clauses []imeClause
	for i := uint64(0); i < 500; i++ {
		clauses = append(clauses, imeClause{i, 1, 1})
	}
	p := buildPreedit(long, nsRange{0, 0}, clauses)
	if n := len([]rune(p.Text)); n > toolkit.MaxPreeditRunes {
		t.Errorf("%d runes kept, bound %d", n, toolkit.MaxPreeditRunes)
	}
	if len(p.Segments) > toolkit.MaxPreeditSegments {
		t.Errorf("%d segments kept, bound %d", len(p.Segments), toolkit.MaxPreeditSegments)
	}
}

func TestMarkedRangeOf(t *testing.T) {
	if r := markedRangeOf(toolkit.Preedit{}); r.Location != nsNotFound {
		t.Errorf("empty preedit range = %+v", r)
	}
	if r := markedRangeOf(toolkit.Preedit{Text: "a😀"}); r != (nsRange{0, 3}) {
		t.Errorf("range = %+v, want {0 3}", r)
	}
}
