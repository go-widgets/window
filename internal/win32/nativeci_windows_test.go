// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package win32

import (
	"testing"

	"github.com/go-mswin/win32"
	"github.com/go-widgets/toolkit"
)

// TestWin32NativeControlsCreate verifies the native-control backend on a plain
// windows-latest CI runner (no interactive desktop): it opens a real window and
// reconciles a Surface's descriptors into live Win32 child controls, then checks
// each was created and that the readable values round-trip. Control CREATION and
// window-message value transfer work headless — proven by this test running here —
// so the win32 controls are CI-verified, not only cross-built. (The clipboard and
// appearance proofs stay gated behind `integration`, needing PowerShell witnesses
// and a real desktop; only those need the gate.)
//
// If a future runner has no window station at all, New fails and the test skips
// rather than failing for the wrong reason.
func TestWin32NativeControlsCreate(t *testing.T) {
	win, err := New("go-widgets/window win32 native-control CI proof", 360, 320, toolkit.DefaultDark())
	if err != nil {
		t.Skipf("no window station on this runner: %v", err)
	}
	t.Cleanup(func() { win.Close() })

	surf := toolkit.NewSurface(func() ([]byte, int, int) { return make([]byte, 360*320*4), 360, 320 })
	r := func(y int) toolkit.Rect { return toolkit.Rect{X: 10, Y: y, W: 220, H: 24} }
	surf.Controls = func() []toolkit.NativeControl {
		return []toolkit.NativeControl{
			{Kind: toolkit.NativeEntry, Key: "entry", Rect: r(10), Visible: true, Text: "hello", OnText: func(string) {}},
			{Kind: toolkit.NativeSecureEntry, Key: "secure", Rect: r(40), Visible: true, Text: "s3cret", OnText: func(string) {}},
			{Kind: toolkit.NativeSearch, Key: "search", Rect: r(70), Visible: true, Text: "find", OnText: func(string) {}},
			{Kind: toolkit.NativeTextView, Key: "text", Rect: r(100), Visible: true, Text: "multi\r\nline", OnText: func(string) {}},
			{Kind: toolkit.NativeCombo, Key: "combo", Rect: r(130), Visible: true, Items: []string{"a", "b"}, Text: "typed", OnText: func(string) {}},
			{Kind: toolkit.NativeCheckbox, Key: "check", Rect: r(160), Visible: true, On: true, OnBool: func(bool) {}},
			{Kind: toolkit.NativeSlider, Key: "slider", Rect: r(190), Visible: true, Min: 0, Max: 100, Number: 40, OnNumber: func(float64) {}},
			{Kind: toolkit.NativeStepper, Key: "stepper", Rect: r(220), Visible: true, Min: 0, Max: 10, Number: 3, OnNumber: func(float64) {}},
			{Kind: toolkit.NativeProgress, Key: "prog", Rect: r(250), Visible: true, Min: 0, Max: 100, Number: 60},
			{Kind: toolkit.NativeSpinner, Key: "spin", Rect: r(280), Visible: true, On: true},
			{Kind: toolkit.NativePopUp, Key: "popup", Rect: r(310), Visible: true, Items: []string{"One", "Two"}, Text: "Two", OnText: func(string) {}},
		}
	}
	win.root = surf
	win.syncNative(surf)

	for _, key := range []string{"entry", "secure", "search", "text", "combo", "check", "slider", "stepper", "prog", "spin", "popup"} {
		if win.nativeControls[key] == nil {
			t.Fatalf("control %q was not created", key)
		}
	}
	// Text controls carry their value through WM_SETTEXT/WM_GETTEXT.
	if v := win32.GetWindowText(win32.HWND(win.nativeControls["entry"].hwnd)); v != "hello" {
		t.Errorf("entry text = %q, want hello", v)
	}
	if v := win32.GetWindowText(win32.HWND(win.nativeControls["search"].hwnd)); v != "find" {
		t.Errorf("search text = %q, want find", v)
	}
	if v := win32.GetWindowText(win32.HWND(win.nativeControls["combo"].hwnd)); v != "typed" {
		t.Errorf("combo edit text = %q, want typed", v)
	}
	// The checkbox reports its checked state.
	if !getCheck(win.nativeControls["check"].hwnd) {
		t.Error("checkbox not checked after On:true")
	}

	// An app-side change reconciles into the control (value-diff push).
	surf.Controls = func() []toolkit.NativeControl {
		return []toolkit.NativeControl{
			{Kind: toolkit.NativeEntry, Key: "entry", Rect: r(10), Visible: true, Text: "changed", OnText: func(string) {}},
		}
	}
	win.syncNative(surf)
	if v := win32.GetWindowText(win32.HWND(win.nativeControls["entry"].hwnd)); v != "changed" {
		t.Errorf("after app change, entry = %q, want changed", v)
	}
	if _, gone := win.nativeControls["popup"]; gone {
		t.Error("popup should have been reconciled away")
	}
}
