// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package win32

import (
	"unicode/utf16"

	"github.com/go-widgets/toolkit"
)

// IMM32 composition attributes, one byte per UTF-16 unit of the composition
// string (imm.h: ATTR_*).
const (
	attrInput              = 0x00 // typed, not yet converted
	attrTargetConverted    = 0x01 // converted, and the clause being edited
	attrConverted          = 0x02 // converted
	attrTargetNotConverted = 0x03 // not converted, and the clause being edited
	attrInputError         = 0x04
	attrFixedConverted     = 0x05
)

// maxImeUnits bounds the UTF-16 units read from one composition. The toolkit
// keeps at most MaxPreeditRunes runes; reading more than twice that (a rune is at
// most two units) only spends memory on text it would drop.
const maxImeUnits = 2 * toolkit.MaxPreeditRunes

// attrStyle maps an IMM32 attribute to the toolkit's three styles: a clause under
// edit (target) is the thick underline, a converted one the thin, and what is
// still being typed, or is an error, is plain input.
func attrStyle(a byte) toolkit.PreeditStyle {
	switch a {
	case attrTargetConverted, attrTargetNotConverted:
		return toolkit.PreeditTarget
	case attrConverted, attrFixedConverted:
		return toolkit.PreeditConverted
	}
	return toolkit.PreeditInput
}

// utf16Offsets maps each UTF-16 unit offset of units to the rune offset it starts
// in (a unit in the middle of a surrogate pair maps to the pair's rune), with one
// extra entry for the end. It is what turns IMM32's unit-based cursor and
// attribute positions into the toolkit's rune offsets.
func utf16Offsets(units []uint16) []int {
	off := make([]int, len(units)+1)
	ri := 0
	for i := 0; i < len(units); ri++ {
		off[i] = ri
		if i+1 < len(units) && utf16.IsSurrogate(rune(units[i])) && utf16.IsSurrogate(rune(units[i+1])) && units[i] < 0xDC00 && units[i+1] >= 0xDC00 {
			off[i+1] = ri
			i += 2
			continue
		}
		i++
	}
	off[len(units)] = ri
	return off
}

// buildPreedit turns what ImmGetCompositionStringW reports — the composition
// string in UTF-16, the caret in units (GCS_CURSORPOS) and one attribute per unit
// (GCS_COMPATTR, possibly absent) — into the toolkit's bounded preedit. An empty
// composition is no preedit. A caret outside the string is none.
func buildPreedit(units []uint16, cursor int, attrs []byte) toolkit.Preedit {
	if len(units) > maxImeUnits {
		units = units[:maxImeUnits]
	}
	if len(units) == 0 {
		return toolkit.Preedit{Cursor: -1}
	}
	off := utf16Offsets(units)
	p := toolkit.Preedit{Text: string(utf16.Decode(units)), Cursor: -1}
	if cursor >= 0 && cursor <= len(units) {
		p.Cursor = off[cursor]
	}
	if len(attrs) > len(units) {
		attrs = attrs[:len(units)]
	}
	for i := 0; i < len(attrs); {
		st := attrStyle(attrs[i])
		j := i + 1
		for j < len(attrs) && attrStyle(attrs[j]) == st {
			j++
		}
		if st != toolkit.PreeditInput && off[i] < off[j] {
			p.Segments = append(p.Segments, toolkit.PreeditSegment{Start: off[i], End: off[j], Style: st})
		}
		i = j
	}
	return p.Sanitize()
}

// commitFrom is the text of a finished composition (GCS_RESULTSTR), bounded like
// the preedit; a lone surrogate becomes U+FFFD.
func commitFrom(units []uint16) string {
	if len(units) > maxImeUnits*8 {
		units = units[:maxImeUnits*8]
	}
	return string(utf16.Decode(units))
}
