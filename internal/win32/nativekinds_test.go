// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package win32

import (
	"math"
	"testing"
)

func TestLinkMarkup(t *testing.T) {
	for in, want := range map[string]string{
		"open":     "<a>open</a>",
		"a & b":    "<a>a &amp; b</a>",
		"<b>x</b>": "<a>&lt;b&gt;x&lt;/b&gt;</a>",
		"":         "<a></a>",
	} {
		if got := linkMarkup(in); got != want {
			t.Errorf("linkMarkup(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestISODate(t *testing.T) {
	y, m, d, ok := parseISODate("2026-02-14")
	if !ok || y != 2026 || m != 2 || d != 14 {
		t.Fatalf("parseISODate = %d-%d-%d %v", y, m, d, ok)
	}
	for _, bad := range []string{"", "2026-02-30", "14/02/2026", "2026-2-1x"} {
		if _, _, _, ok := parseISODate(bad); ok {
			t.Errorf("parseISODate(%q) accepted", bad)
		}
	}
	if got := formatISODate(26, 2, 4); got != "0026-02-04" {
		t.Errorf("formatISODate = %q", got)
	}
}

func TestHexColor(t *testing.T) {
	ref, ok := parseHexColor("#3366cc")
	if !ok || ref != 0xcc6633 {
		t.Fatalf("parseHexColor = %#x %v, want COLORREF 0xcc6633", ref, ok)
	}
	if got := colorHex(ref); got != "#3366cc" {
		t.Errorf("colorHex = %q", got)
	}
	for _, bad := range []string{"#3366ccd", "3366cc", "#3366c", "#33ZZcc", "x3366cc", ""} {
		if _, ok := parseHexColor(bad); ok {
			t.Errorf("parseHexColor(%q) accepted", bad)
		}
	}
}

func TestRowIndex(t *testing.T) {
	for _, c := range []struct {
		n    float64
		want int
	}{{0, 0}, {2, 2}, {3, -1}, {-1, -1}, {math.NaN(), -1}, {1.9, 1}} {
		if got := rowIndex(c.n, 3); got != c.want {
			t.Errorf("rowIndex(%v,3) = %d, want %d", c.n, got, c.want)
		}
	}
}

func TestItemIndex(t *testing.T) {
	items := []string{"one", "two"}
	if got := itemIndex(items, "two"); got != 1 {
		t.Errorf("itemIndex = %d", got)
	}
	if got := itemIndex(items, "x"); got != -1 {
		t.Errorf("itemIndex(missing) = %d", got)
	}
}
