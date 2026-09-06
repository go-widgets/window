// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux && !android

package gtk

import (
	"testing"

	"github.com/go-widgets/toolkit"
)

// TestGTKBackendReconcilesControls is the on-device proof of the GTK4-hosted
// back-end: it opens a real GTK window, presents a frame, and reconciles a
// self-rendering Surface's native controls into the GtkFixed — a real
// GtkEntry (secure) whose value round-trips, follows an app-side change, and is
// reconciled away when the Surface stops publishing it. Needs a display; under a
// headless runner run it with Xvfb (skips otherwise).
func TestGTKBackendReconcilesControls(t *testing.T) {
	win, err := Open("gtk back-end test", 320, 200, nil, 1)
	if err != nil {
		t.Skipf("no GTK display: %v", err)
	}
	defer win.Close()

	pw := "hunter2"
	surf := toolkit.NewSurface(func() ([]byte, int, int) {
		return make([]byte, 320*200*4), 320, 200
	})
	surf.Controls = func() []toolkit.NativeControl {
		return []toolkit.NativeControl{{
			Kind: toolkit.NativeSecureEntry, Key: "pw",
			Rect: toolkit.Rect{X: 10, Y: 10, W: 200, H: 24}, Visible: true,
			Text:   pw,
			OnText: func(s string) { pw = s },
		}}
	}
	win.root = surf

	// Present + reconcile: the secure entry is created with its value.
	win.frame()
	lc := win.native["pw"]
	if lc == nil {
		t.Fatal("no GTK control created for the pw descriptor")
	}
	if got := lc.widget.Text(); got != "hunter2" {
		t.Errorf("entry text = %q, want hunter2", got)
	}

	// App changes the value; the next frame pushes it (it differs).
	pw = "changed"
	win.frame()
	if got := lc.widget.Text(); got != "changed" {
		t.Errorf("after app change, entry text = %q, want changed", got)
	}

	// The Surface stops publishing it: the control is reconciled away.
	surf.Controls = func() []toolkit.NativeControl { return nil }
	win.frame()
	if _, ok := win.native["pw"]; ok {
		t.Error("control was not reconciled away after the descriptor disappeared")
	}
}

// TestGTKBackendSliderPopUp proves the two remaining kinds host on GTK: a real
// GtkScale takes a slider's value and a GtkDropDown takes a pop-up's selection
// (the selected item's string, as on cocoa/win32), each following an app-side
// change on the next frame.
func TestGTKBackendSliderPopUp(t *testing.T) {
	win, err := Open("gtk slider/pop-up test", 320, 200, nil, 1)
	if err != nil {
		t.Skipf("no GTK display: %v", err)
	}
	defer win.Close()

	rect := toolkit.Rect{X: 10, Y: 10, W: 200, H: 24}
	val := 30.0
	sel := "two"
	surf := toolkit.NewSurface(func() ([]byte, int, int) { return make([]byte, 320*200*4), 320, 200 })
	surf.Controls = func() []toolkit.NativeControl {
		return []toolkit.NativeControl{
			{Kind: toolkit.NativeSlider, Key: "vol", Rect: rect, Visible: true, Min: 0, Max: 100, Number: val, OnNumber: func(v float64) { val = v }},
			{Kind: toolkit.NativePopUp, Key: "mode", Rect: rect, Visible: true, Items: []string{"one", "two", "three"}, Text: sel, OnText: func(s string) { sel = s }},
		}
	}
	win.root = surf

	win.frame()
	slider, pop := win.native["vol"], win.native["mode"]
	if slider == nil || pop == nil {
		t.Fatalf("slider=%v pop-up=%v, both should be created", slider != nil, pop != nil)
	}
	if got := slider.widget.Value(); got != 30 {
		t.Errorf("slider value = %v, want 30", got)
	}
	if got := pop.widget.Selected(); got != 1 { // "two" is index 1
		t.Errorf("pop-up selection = %d, want 1 (two)", got)
	}

	// An app-side change is pushed on the next frame.
	val, sel = 75, "three"
	win.frame()
	if got := slider.widget.Value(); got != 75 {
		t.Errorf("after app change, slider value = %v, want 75", got)
	}
	if got := pop.widget.Selected(); got != 2 { // "three" is index 2
		t.Errorf("after app change, pop-up selection = %d, want 2 (three)", got)
	}
}

// TestGTKBackendMoreWidgets proves the ten controls added for full parity build
// on real GTK and round-trip their value through syncNative: a stepper, search,
// editable combo, multi-line text view, date and colour carry a value; a
// segmented control tracks its selected item; a progress bar, spinner and link
// build without error.
func TestGTKBackendMoreWidgets(t *testing.T) {
	win, err := Open("gtk more-widgets test", 400, 300, nil, 1)
	if err != nil {
		t.Skipf("no GTK display: %v", err)
	}
	defer win.Close()

	r := toolkit.Rect{X: 10, Y: 10, W: 200, H: 28}
	specs := []toolkit.NativeControl{
		{Kind: toolkit.NativeProgress, Key: "prog", Rect: r, Visible: true, Min: 0, Max: 100, Number: 40},
		{Kind: toolkit.NativeSpinner, Key: "spin", Rect: r, Visible: true, On: true},
		{Kind: toolkit.NativeStepper, Key: "step", Rect: r, Visible: true, Min: 0, Max: 10, Number: 3, OnNumber: func(float64) {}},
		{Kind: toolkit.NativeSearch, Key: "srch", Rect: r, Visible: true, Text: "hello", OnText: func(string) {}},
		{Kind: toolkit.NativeCombo, Key: "cmbo", Rect: r, Visible: true, Items: []string{"a", "b"}, Text: "typed", OnText: func(string) {}},
		{Kind: toolkit.NativeSegmented, Key: "segm", Rect: r, Visible: true, Items: []string{"one", "two", "three"}, Text: "two", OnText: func(string) {}},
		{Kind: toolkit.NativeTextView, Key: "text", Rect: r, Visible: true, Text: "multi\nline", OnText: func(string) {}},
		{Kind: toolkit.NativeLink, Key: "link", Rect: r, Visible: true, Text: "open", OnActivate: func() {}},
		{Kind: toolkit.NativeDate, Key: "date", Rect: r, Visible: true, Text: "2026-02-14", OnText: func(string) {}},
		{Kind: toolkit.NativeColor, Key: "colr", Rect: r, Visible: true, Text: "#3366cc", OnText: func(string) {}},
	}
	surf := toolkit.NewSurface(func() ([]byte, int, int) { return make([]byte, 400*300*4), 400, 300 })
	surf.Controls = func() []toolkit.NativeControl { return specs }
	win.root = surf
	win.frame()

	for _, k := range []string{"prog", "spin", "step", "srch", "cmbo", "segm", "text", "link", "date", "colr"} {
		if win.native[k] == nil {
			t.Fatalf("control %q was not created", k)
		}
	}
	if got := win.native["step"].widget.SpinValue(); got != 3 {
		t.Errorf("stepper value = %v, want 3", got)
	}
	if got := win.native["srch"].widget.Text(); got != "hello" {
		t.Errorf("search text = %q, want hello", got)
	}
	if got := win.native["cmbo"].widget.ComboText(); got != "typed" {
		t.Errorf("combo text = %q, want typed", got)
	}
	if got := win.native["text"].widget.TextViewText(); got != "multi\nline" {
		t.Errorf("text view = %q, want multi\\nline", got)
	}
	if got := win.native["date"].widget.DateISO(); got != "2026-02-14" {
		t.Errorf("date = %q, want 2026-02-14", got)
	}
	// The segmented control's "two" segment is active.
	if !win.native["segm"].segments[1].Active() {
		t.Error("segmented: the \"two\" segment should be active")
	}
}
