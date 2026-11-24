// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package gtk

import (
	"unicode/utf8"

	"github.com/go-widgets/toolkit"
)

// preeditFrom turns what a GtkIMContext reports — the text being composed and the
// caret in characters — into the toolkit's bounded preedit. GTK's Pango underline
// attributes are not read, so the whole text is one input clause; the cursor is
// clamped by Sanitize, so a context that reports one past the end, or a negative,
// cannot put the caret outside the text. An empty text is no composition.
func preeditFrom(text string, cursor int) toolkit.Preedit {
	if text == "" {
		return toolkit.Preedit{Cursor: -1}
	}
	n := utf8.RuneCountInString(text)
	return toolkit.Preedit{
		Text:     text,
		Cursor:   cursor,
		Segments: []toolkit.PreeditSegment{{Start: 0, End: n, Style: toolkit.PreeditInput}},
	}.Sanitize()
}

// composeInto delivers a commit to the focused painted field: it ends any preedit
// and inserts text. It reports false when no field that composes has the focus,
// and the caller then falls back to the key events it replaces.
func composeInto(root toolkit.Widget, text string) bool {
	c, _, ok := toolkit.FocusedComposition(root)
	if !ok {
		return false
	}
	c.CancelPreedit()
	c.CommitText(text)
	return true
}

// setPreedit shows p in the focused painted field, or ends the composition when p
// is empty. It reports whether a field took it.
func setPreedit(root toolkit.Widget, p toolkit.Preedit) bool {
	c, _, ok := toolkit.FocusedComposition(root)
	if !ok {
		return false
	}
	c.SetPreedit(p) // an empty preedit ends the composition (toolkit.Composer)
	return true
}

// commitFallback is the key events a commit stands for when no field composes: a
// KeyDown and a Char per rune, what the key itself produced before the input
// method sat in front of it.
func commitFallback(text string) []toolkit.Event {
	var evs []toolkit.Event
	n := 0
	for _, r := range text {
		if n++; n > toolkit.MaxCommitRunes {
			break
		}
		s := string(r)
		evs = append(evs,
			toolkit.Event{Kind: toolkit.EventKeyDown, Code: s},
			toolkit.Event{Kind: toolkit.EventChar, Code: s})
	}
	return evs
}

// caretRect converts the composition rectangle (framebuffer pixels) to the
// client-widget points GTK takes, one point wide at least so a collapsed caret
// still anchors the candidate window.
func caretRect(r toolkit.Rect, scale float64) (x, y, w, h int) {
	if scale <= 0 {
		scale = 1
	}
	x, y = int(float64(r.X)/scale), int(float64(r.Y)/scale)
	w, h = int(float64(r.W)/scale), int(float64(r.H)/scale)
	if w < 1 {
		w = 1
	}
	return x, y, w, h
}
