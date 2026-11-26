// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package cocoa

import (
	"strings"
	"unsafe"

	objc "github.com/go-macos/objc"

	"github.com/go-widgets/toolkit"
)

// NSTextInputClient for the painted view. AppKit sends every key that is not a
// shortcut or a named key through the input context (interpretKeyEvents:), which
// calls back here: a dead key or an input method (kana, pinyin, the emoji
// palette) arrives as setMarkedText: (the preedit), insertText: (the commit),
// unmarkText and firstRectForCharacterRange: (where to put the candidate
// window). The focused painted field is a toolkit.Composer, found through
// toolkit.FocusedComposition; nothing here knows the widget.

var (
	selInterpretKeyEvents = objc.RegisterName("interpretKeyEvents:")
	selArrayWithObject    = objc.RegisterName("arrayWithObject:")
	selEmptyArray         = objc.RegisterName("array")
	selIsKindOfClass      = objc.RegisterName("isKindOfClass:")
	selStringProp         = objc.RegisterName("string")
	selAttrsAtIndex       = objc.RegisterName("attributesAtIndex:effectiveRange:")
	selIntegerValue       = objc.RegisterName("integerValue")
	selUTF16Length        = objc.RegisterName("length")
	selCharacters         = objc.RegisterName("characters")
	selConvertRectScreen  = objc.RegisterName("convertRectToScreen:")
)

// imeStep is the state of one keyDown: pass through interpretKeyEvents:.
var imeStep struct {
	event   objc.ID // the key event being interpreted, 0 outside a pass
	touched bool    // insertText: or setMarkedText: ran during the pass
}

func imeMethods() []objc.MethodDef {
	return []objc.MethodDef{
		{Cmd: objc.RegisterName("insertText:replacementRange:"), Fn: viewInsertText},
		{Cmd: objc.RegisterName("setMarkedText:selectedRange:replacementRange:"), Fn: viewSetMarkedText},
		{Cmd: objc.RegisterName("unmarkText"), Fn: viewUnmarkText},
		{Cmd: objc.RegisterName("hasMarkedText"), Fn: viewHasMarkedText},
		{Cmd: objc.RegisterName("markedRange"), Fn: viewMarkedRange},
		{Cmd: objc.RegisterName("selectedRange"), Fn: viewSelectedRange},
		{Cmd: objc.RegisterName("validAttributesForMarkedText"), Fn: viewValidAttributes},
		{Cmd: objc.RegisterName("firstRectForCharacterRange:actualRange:"), Fn: viewFirstRect},
		{Cmd: objc.RegisterName("characterIndexForPoint:"), Fn: viewCharacterIndexForPoint},
		{Cmd: objc.RegisterName("attributedSubstringForProposedRange:actualRange:"), Fn: viewAttributedSubstring},
		{Cmd: objc.RegisterName("doCommandBySelector:"), Fn: viewDoCommand},
	}
}

// imeComposer is the focused composing field of the active window.
func imeComposer() (toolkit.Composer, toolkit.Rect, bool) {
	w := active
	if w == nil || w.root == nil {
		return nil, toolkit.Rect{}, false
	}
	return toolkit.FocusedComposition(w.root)
}

func imeRepaint() {
	if w := active; w != nil {
		w.paintFrame(false)
	}
}

// imeString reads the NSString or NSAttributedString AppKit passes, and the
// attribute clauses of the latter.
func imeString(obj objc.ID) (string, []imeClause) {
	// A nil string must not reach Send: a message to nil made first poisons the
	// cached call for isKindOfClass:, and the next real attributed string is then
	// taken for a plain one (measured: it aborts in lengthOfBytesUsingEncoding:).
	if obj == 0 {
		return "", nil
	}
	if !objc.Send[bool](obj, selIsKindOfClass, objc.ID(objc.GetClass("NSAttributedString"))) {
		return objc.GoString(obj), nil
	}
	text := objc.GoString(obj.Send(selStringProp))
	n := objc.Send[uint64](obj, selUTF16Length)
	// The preedit is bounded by the toolkit; bound the walk to the same order so
	// a hostile attributed string cannot make this loop long.
	if n > 4*toolkit.MaxPreeditRunes {
		n = 4 * toolkit.MaxPreeditRunes
	}
	underlineKey := objc.NSString("NSUnderline")
	var clauses []imeClause
	for i := uint64(0); i < n && len(clauses) < 2*toolkit.MaxPreeditSegments; {
		var eff nsRange
		attrs := obj.Send(selAttrsAtIndex, i, unsafe.Pointer(&eff))
		if eff.Length == 0 {
			break
		}
		ul := 0
		if attrs != 0 {
			if v := attrs.Send(selObjectForKey, underlineKey); v != 0 {
				ul = int(objc.Send[int64](v, selIntegerValue))
			}
		}
		clauses = append(clauses, imeClause{Loc: eff.Location, Len: eff.Length, Underline: ul})
		i = eff.Location + eff.Length
	}
	return text, clauses
}

func viewSetMarkedText(self objc.ID, _ objc.SEL, str objc.ID, sel nsRange, _ nsRange) {
	route(self)
	imeStep.touched = true
	comp, _, ok := imeComposer()
	if !ok {
		return
	}
	text, clauses := imeString(str)
	if text == "" {
		comp.CancelPreedit()
	} else {
		comp.SetPreedit(buildPreedit(text, sel, clauses))
	}
	imeRepaint()
}

func viewInsertText(self objc.ID, _ objc.SEL, str objc.ID, _ nsRange) {
	imeStep.touched = true
	text, _ := imeString(str)
	w := route(self)
	if w == nil || text == "" {
		return
	}
	comp, _, composing := imeComposer()
	hadPreedit := composing && comp.Preedit().Text != ""
	if !hadPreedit && imeStep.event != 0 && imeIsPlainKey(imeStep.event, text) {
		// An ordinary keystroke: exactly the events the key mapping always sent
		// (KeyDown with its modifiers, then Char), so shortcuts and widgets that
		// read key codes see no change.
		keyEvent(imeStep.event, true)
		return
	}
	if !hadPreedit && imeStep.event != 0 {
		// Not plain (a dead-key result, option-c): keep the key-down so shortcuts
		// see the chord, then commit what the layout produced.
		if evs := MapKey(uint16(objc.Send[uint64](imeStep.event, selKeyCode)),
			objc.GoString(imeStep.event.Send(selCharsIgnoringMods)), eventMods(imeStep.event), true); len(evs) > 0 &&
			evs[0].Kind == toolkit.EventKeyDown {
			w.dispatch(evs[0])
		}
	}
	imeCommit(w, text)
}

// imeIsPlainKey reports that text is just what the key types on its own: the
// event's characters equal text, and differ from the unmodified key at most by
// case (shift). A dead-key result (e after option-e gives é) is not plain.
func imeIsPlainKey(event objc.ID, text string) bool {
	chars := objc.GoString(event.Send(selCharacters))
	base := objc.GoString(event.Send(selCharsIgnoringMods))
	return chars == text && strings.EqualFold(text, base)
}

// imeCommit puts committed text into the focused field, or sends it as
// characters when nothing composes.
func imeCommit(w *Window, text string) {
	if comp, _, ok := toolkit.FocusedComposition(w.root); ok {
		comp.CancelPreedit()
		comp.CommitText(text)
		w.paintFrame(false)
		return
	}
	for _, r := range text {
		w.dispatch(toolkit.Event{Kind: toolkit.EventChar, Code: string(r)})
	}
}

func viewUnmarkText(self objc.ID, _ objc.SEL) {
	route(self)
	comp, _, ok := imeComposer()
	if !ok {
		return
	}
	text := comp.Preedit().Text
	comp.CancelPreedit()
	if text != "" {
		comp.CommitText(text)
	}
	imeRepaint()
}

func viewHasMarkedText(self objc.ID, _ objc.SEL) bool {
	route(self)
	comp, _, ok := imeComposer()
	return ok && comp.Preedit().Text != ""
}

func viewMarkedRange(self objc.ID, _ objc.SEL) nsRange {
	route(self)
	if comp, _, ok := imeComposer(); ok {
		return markedRangeOf(comp.Preedit())
	}
	return nsRange{nsNotFound, 0}
}

func viewSelectedRange(self objc.ID, _ objc.SEL) nsRange {
	route(self)
	if comp, _, ok := imeComposer(); ok {
		if p := comp.Preedit(); p.Text != "" && p.Cursor >= 0 {
			rs := []rune(p.Text)
			return nsRange{uint64(utf16Len(string(rs[:min(p.Cursor, len(rs))]))), 0}
		}
	}
	return nsRange{0, 0}
}

func viewValidAttributes(_ objc.ID, _ objc.SEL) objc.ID {
	return objc.ID(objc.GetClass("NSArray")).Send(selEmptyArray)
}

func viewCharacterIndexForPoint(_ objc.ID, _ objc.SEL, _ nsPoint) uint64 { return nsNotFound }

func viewAttributedSubstring(_ objc.ID, _ objc.SEL, _ nsRange, _ uintptr) objc.ID { return 0 }

func viewDoCommand(_ objc.ID, _ objc.SEL, _ objc.SEL) {}

// viewFirstRect answers where the candidate window goes: the composition
// rectangle of the focused field, converted from framebuffer pixels (top-left)
// through the flipped view and the window to screen points.
func viewFirstRect(self objc.ID, _ objc.SEL, _ nsRange, _ uintptr) nsRect {
	w := route(self)
	if w == nil || w.win == 0 {
		return nsRect{}
	}
	var r toolkit.Rect
	if _, rr, ok := imeComposer(); ok {
		r = rr
	}
	s := w.scale
	if s <= 0 {
		s = 1
	}
	vr := nsRect{
		Origin: nsPoint{X: float64(r.X) / s, Y: float64(r.Y) / s},
		Size:   nsSize{W: float64(r.W) / s, H: float64(r.H) / s},
	}
	inWindow := objc.Send[nsRect](self, selConvertRectToView, vr, objc.ID(0))
	return objc.Send[nsRect](w.win, selConvertRectScreen, inWindow)
}

// viewKeyDown feeds a key to the input context when it can be part of a
// composition, and otherwise straight to the key mapping as before.
func viewKeyDown(self objc.ID, _ objc.SEL, event objc.ID) {
	w := route(self)
	if w == nil {
		return
	}
	comp, _, _ := imeComposer()
	composing := comp != nil && comp.Preedit().Text != ""
	code := uint16(objc.Send[uint64](event, selKeyCode))
	name, _ := DecodeKey(code, objc.GoString(event.Send(selCharsIgnoringMods)))
	// Every key goes through the input context first: it takes the ones that are
	// part of a composition (and a dead key), and leaves a shortcut or a named key
	// to the fallthrough below, which sends it as the key mapping always did.
	imeStep.event, imeStep.touched = event, false
	arr := objc.ID(objc.GetClass("NSArray")).Send(selArrayWithObject, event)
	self.Send(selInterpretKeyEvents, arr)
	touched := imeStep.touched
	imeStep.event = 0
	if touched {
		return
	}
	if composing && name == "Escape" {
		comp.CancelPreedit()
		imeRepaint()
		return
	}
	keyEvent(event, true) // the input context did not take it (a command key)
}
