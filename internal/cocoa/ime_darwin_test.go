// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package cocoa

import (
	"testing"

	"github.com/go-widgets/toolkit"
)

// withActive runs f with the package's active window replaced, restoring it
// after. The input-client methods read the active window; a bare Window whose
// root is a laid-out tree is all they need when no repaint is reached.
func withActive(t *testing.T, w *Window, f func()) {
	t.Helper()
	old := active
	active = w
	defer func() { active = old }()
	f()
}

// imeTree is a laid-out root holding one Entry, focused or not.
func imeTree(focused bool) (*Window, *toolkit.Entry) {
	e := toolkit.NewEntry("")
	e.SetFocused(focused)
	root := toolkit.NewVBox()
	root.Append(e)
	root.SetBounds(toolkit.Rect{W: 200, H: 40})
	return &Window{root: root}, e
}

func TestInputClientWithoutAnActiveWindowAnswersNothing(t *testing.T) {
	withActive(t, nil, func() {
		if viewHasMarkedText(0, 0) {
			t.Error("hasMarkedText with no window")
		}
		if r := viewMarkedRange(0, 0); r != (nsRange{nsNotFound, 0}) {
			t.Errorf("markedRange = %+v, want not found", r)
		}
		if r := viewSelectedRange(0, 0); r != (nsRange{0, 0}) {
			t.Errorf("selectedRange = %+v, want {0,0}", r)
		}
		if r := viewFirstRect(0, 0, nsRange{}, 0); r != (nsRect{}) {
			t.Errorf("firstRect = %+v, want empty", r)
		}
		viewInsertText(0, 0, 0, nsRange{})
		viewSetMarkedText(0, 0, 0, nsRange{}, nsRange{})
		viewUnmarkText(0, 0)
		viewKeyDown(0, 0, 0) // must not touch the event: there is no window to send it to
	})
}

func TestInputClientWithoutAComposingFieldDoesNothing(t *testing.T) {
	w, e := imeTree(false) // an Entry that does not have the focus composes nothing
	withActive(t, w, func() {
		imeStep.touched = false
		viewSetMarkedText(0, 0, 0, nsRange{}, nsRange{})
		if !imeStep.touched {
			t.Error("setMarkedText did not record that the input context was used")
		}
		viewUnmarkText(0, 0)
		if viewHasMarkedText(0, 0) {
			t.Error("a field without the focus reports marked text")
		}
	})
	if p := e.Preedit(); p.Text != "" {
		t.Errorf("preedit = %+v, want none", p)
	}
}

func TestInsertingNothingKeepsTheComposition(t *testing.T) {
	w, e := imeTree(true)
	e.SetPreedit(toolkit.Preedit{Text: "かな", Cursor: 2})
	withActive(t, w, func() {
		if !viewHasMarkedText(0, 0) {
			t.Fatal("the focused field does not report its preedit")
		}
		// An empty or nil string commits nothing and must not cancel what is being
		// composed.
		viewInsertText(0, 0, 0, nsRange{})
	})
	if p := e.Preedit(); p.Text != "かな" {
		t.Errorf("preedit after inserting nothing = %q, want it kept", p.Text)
	}
}
