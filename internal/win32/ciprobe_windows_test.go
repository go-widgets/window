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

// TestWin32ControlsCreateHeadless probes whether the plain cross-build CI runner
// (windows-latest, no interactive desktop) can actually CREATE native child
// controls — including the common-control classes (progress, up-down) — and round
// a value through them. If it can, win32 native controls are CI-verifiable and no
// longer only cross-verified. It is UNGATED (no integration tag) on purpose, but
// skips cleanly if the runner has no window station, so it can never break CI: it
// either proves creation works (pass) or records that it does not (skip).
func TestWin32ControlsCreateHeadless(t *testing.T) {
	win, err := New("go-widgets win32 CI probe", 360, 240, toolkit.DefaultDark())
	if err != nil {
		t.Skipf("no window station on this runner: %v", err)
	}
	t.Cleanup(func() { win.Close() })

	surf := toolkit.NewSurface(func() ([]byte, int, int) { return make([]byte, 360*240*4), 360, 240 })
	surf.Controls = func() []toolkit.NativeControl {
		return []toolkit.NativeControl{
			{Kind: toolkit.NativeEntry, Key: "e", Rect: toolkit.Rect{X: 10, Y: 10, W: 200, H: 24}, Visible: true, Text: "hello", OnText: func(string) {}},
			{Kind: toolkit.NativeProgress, Key: "p", Rect: toolkit.Rect{X: 10, Y: 40, W: 200, H: 24}, Visible: true, Min: 0, Max: 100, Number: 50},
			{Kind: toolkit.NativeStepper, Key: "s", Rect: toolkit.Rect{X: 10, Y: 70, W: 200, H: 24}, Visible: true, Min: 0, Max: 10, Number: 3, OnNumber: func(float64) {}},
		}
	}
	win.root = surf
	win.syncNative(surf)

	if got := len(win.nativeControls); got != 3 {
		t.Fatalf("controls created = %d, want 3 (Entry+Progress+Stepper)", got)
	}
	if lc := win.nativeControls["e"]; lc != nil {
		if v := win32.GetWindowText(win32.HWND(lc.hwnd)); v != "hello" {
			t.Errorf("entry value = %q, want hello", v)
		}
	}
}
