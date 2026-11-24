// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package cocoa

import (
	"github.com/go-widgets/toolkit"
)

// The pure half of the NSTextInputClient bridge: AppKit speaks UTF-16 offsets,
// the toolkit speaks rune offsets. Nothing here touches Objective-C, so it is
// testable on every platform.

// nsNotFound is NSNotFound: NSIntegerMax.
const nsNotFound = uint64(1<<63 - 1)

// nsRange is NSRange, passed by value in two integer registers.
type nsRange struct{ Location, Length uint64 }

// imeClause is one run of marked text with the same attributes, in UTF-16
// units, as read from an NSAttributedString.
type imeClause struct {
	Loc, Len uint64
	// Underline is the NSUnderline attribute value (0 none, 1 single, 2 thick).
	Underline int
}

// utf16Len is the length of s in UTF-16 units, which is what NSString counts.
func utf16Len(s string) int {
	n := 0
	for _, r := range s {
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return n
}

// runeOffset converts a UTF-16 offset into s to a rune offset. An offset inside
// a surrogate pair rounds down; one past the end clamps to the rune count.
func runeOffset(s string, u16 uint64) int {
	var units uint64
	i := 0
	for _, r := range s {
		w := uint64(1)
		if r >= 0x10000 {
			w = 2
		}
		if units+w > u16 {
			return i
		}
		units += w
		i++
	}
	return i
}

// buildPreedit turns what AppKit handed setMarkedText: into the toolkit's
// Preedit. sel is the selectedRange (its Location is the IME cursor), clauses
// the attribute runs; with none the whole text is one input segment. The result
// is sanitised, so a hostile NSString cannot exceed the toolkit's bounds.
func buildPreedit(text string, sel nsRange, clauses []imeClause) toolkit.Preedit {
	p := toolkit.Preedit{Text: text, Cursor: -1}
	if sel.Location != nsNotFound && sel.Location <= uint64(utf16Len(text)) {
		p.Cursor = runeOffset(text, sel.Location)
	}
	for _, c := range clauses {
		if c.Len == 0 || c.Loc == nsNotFound {
			continue
		}
		style := toolkit.PreeditInput
		switch {
		case c.Underline >= 2:
			style = toolkit.PreeditTarget
		case c.Underline == 1:
			style = toolkit.PreeditConverted
		}
		start := runeOffset(text, c.Loc)
		end := runeOffset(text, c.Loc+c.Len)
		if end > start {
			p.Segments = append(p.Segments, toolkit.PreeditSegment{Start: start, End: end, Style: style})
		}
	}
	return p.Sanitize()
}

// markedRangeOf is the NSRange of the marked text: the whole preedit, or
// NSNotFound when there is none.
func markedRangeOf(p toolkit.Preedit) nsRange {
	if p.Text == "" {
		return nsRange{nsNotFound, 0}
	}
	return nsRange{0, uint64(utf16Len(p.Text))}
}
