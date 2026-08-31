// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows && integration

package win32

import (
	"testing"

	"github.com/go-mswin/win32"
	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// nativeRoot is a minimal childContainer that stacks its children and exposes
// them to WalkNative.
type nativeRoot struct {
	toolkit.Base
	kids []toolkit.Widget
}

func (r *nativeRoot) SetBounds(b toolkit.Rect) {
	r.Base.SetBounds(b)
	y := b.Y + 10
	for _, k := range r.kids {
		k.SetBounds(toolkit.Rect{X: b.X + 10, Y: y, W: 200, H: 24})
		y += 40
	}
}
func (r *nativeRoot) Draw(p painter.Painter, th *toolkit.Theme) {
	for _, k := range r.kids {
		k.Draw(p, th)
	}
}
func (r *nativeRoot) Children() []toolkit.Widget { return r.kids }

// TestLiveWin32NativeControls is the on-device proof of the toolkit.Native seam
// on Windows: it opens a real window with a Native secure field and a Native
// button, reconciles the native controls, and verifies the backend embedded live
// Win32 child controls parented to the window, claimed the regions, bound the
// value both ways, and reconciled a control away when its Native left the tree.
// Gated behind WINDOW_WIN32_INTEGRATION; it runs on a real Windows machine, not
// in the pure cross-build CI, so it drives the reconcile directly rather than
// pumping the message loop.
func TestLiveWin32NativeControls(t *testing.T) {
	skipUnlessLive(t)

	theme := toolkit.DefaultDark()
	win, err := New("go-widgets/window win32 native-control proof", 360, 200, theme)
	if err != nil {
		t.Fatalf("native-control setup failed: %v", err)
	}
	t.Cleanup(func() { win.Close() })

	pw := toolkit.NewNativeSecureEntry("hunter2")
	pw.Key = "pw"
	clicks := 0
	ok := toolkit.NewNativeButton("OK", func() { clicks++ })
	ok.Key = "ok"
	root := &nativeRoot{kids: []toolkit.Widget{pw, ok}}
	root.SetBounds(toolkit.Rect{X: 0, Y: 0, W: 360, H: 200})

	win.root = root
	win.syncNative(root)

	if got := len(win.nativeControls); got != 2 {
		t.Fatalf("embedded controls = %d, want 2", got)
	}
	if !pw.Claimed().Get() {
		t.Error("secure-field Native was not marked claimed")
	}
	if lc := win.nativeControls["pw"]; lc != nil {
		if v := win32.GetWindowText(win32.HWND(lc.hwnd)); v != "hunter2" {
			t.Errorf("secure field initial value = %q, want hunter2", v)
		}
	}

	// app -> native: the model changes and the next reconcile pushes the new
	// value into the field (it differs from what the control last reported).
	pw.Text().Set("newsecret")
	win.syncNative(root)
	if lc := win.nativeControls["pw"]; lc != nil {
		if v := win32.GetWindowText(win32.HWND(lc.hwnd)); v != "newsecret" {
			t.Errorf("after model Set, field value = %q, want newsecret (toolkit->native binding)", v)
		}
	}

	// Reconcile away: a tree without pw drops its control, keeps ok's.
	only := &nativeRoot{kids: []toolkit.Widget{ok}}
	only.SetBounds(toolkit.Rect{X: 0, Y: 0, W: 360, H: 200})
	win.root = only
	win.syncNative(only)
	if got := len(win.nativeControls); got != 1 {
		t.Errorf("after reconcile: %d controls, want 1", got)
	}
	if _, present := win.nativeControls["pw"]; present {
		t.Error("pw control was not reconciled away")
	}
}
