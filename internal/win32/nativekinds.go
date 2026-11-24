// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package win32

// The OS-independent half of the five native kinds the Win32 host gained after
// Progress/Spinner/Stepper: List, Link, Date, Color and Segmented. The Win32
// glue (native_kinds_windows.go) only moves bytes into and out of controls; the
// decisions -- how a descriptor string becomes a control's value and back --
// are here, where the Linux lane holds them to 100% coverage.

import (
	"fmt"
	"math"
	"strings"
	"time"
)

// linkMarkup wraps a link's visible text as the one-anchor markup a SysLink
// control displays: "<a>text</a>". SysLink parses its caption as markup, so the
// text's own '&', '<' and '>' are escaped -- otherwise a title such as
// "Terms <b>" would be read as a tag and vanish.
func linkMarkup(text string) string {
	r := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;")
	return "<a>" + r.Replace(text) + "</a>"
}

// parseISODate reads a YYYY-MM-DD string, the descriptor's date form. ok is
// false for anything else, including a well-shaped but impossible date.
func parseISODate(s string) (year, month, day int, ok bool) {
	t, err := time.Parse("2006-01-02", s)
	if err != nil {
		return 0, 0, 0, false
	}
	return t.Year(), int(t.Month()), t.Day(), true
}

// formatISODate is the inverse of parseISODate.
func formatISODate(year, month, day int) string {
	return fmt.Sprintf("%04d-%02d-%02d", year, month, day)
}

// parseHexColor reads "#RRGGBB" into a Win32 COLORREF (0x00BBGGRR). ok is false
// for any other form.
func parseHexColor(s string) (ref uint32, ok bool) {
	hex, found := strings.CutPrefix(s, "#")
	if !found || len(hex) != 6 {
		return 0, false
	}
	var r, g, b uint8
	if n, err := fmt.Sscanf(hex, "%02x%02x%02x", &r, &g, &b); err != nil || n != 3 {
		return 0, false
	}
	return uint32(r) | uint32(g)<<8 | uint32(b)<<16, true
}

// colorHex is the inverse of parseHexColor, in the lower case the other hosts
// report.
func colorHex(ref uint32) string {
	return fmt.Sprintf("#%02x%02x%02x", ref&0xFF, (ref>>8)&0xFF, (ref>>16)&0xFF)
}

// rowIndex maps a descriptor's Number (the chosen row, -1 for none) onto a list
// of count rows: a value that is not a row of the list is "none", -1.
func rowIndex(number float64, count int) int {
	if math.IsNaN(number) || number < 0 || number >= float64(count) {
		return -1
	}
	return int(number)
}

// itemIndex is the position of text among items, -1 when it is not one of them.
func itemIndex(items []string, text string) int {
	for i, it := range items {
		if it == text {
			return i
		}
	}
	return -1
}
