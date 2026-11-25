// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux && !android

package gtk

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/go-gtk/gtk4"
	"github.com/go-widgets/toolkit"
)

// pump runs the GLib main loop for n idle turns: enough for GTK to map a window
// and carry out what it was asked.
func pump(n int) {
	loop := gtk4.MainLoopNew()
	var step func(int)
	step = func(i int) {
		if i >= n {
			loop.Quit()
			return
		}
		gtk4.IdleAdd(func() { step(i + 1) })
	}
	gtk4.IdleAdd(func() { step(0) })
	loop.Run()
}

func testPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for i := range img.Pix {
		img.Pix[i] = 200
	}
	img.SetNRGBA(0, 0, color.NRGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func surfaceOf(controls func() []toolkit.NativeControl) *toolkit.Surface {
	s := toolkit.NewSurface(func() ([]byte, int, int) { return make([]byte, 320*200*4), 320, 200 })
	s.Controls = controls
	return s
}

// TestGTKBackendContextMenu proves a NativeControl's Menu is hosted: a popover
// opens over the control, the picked entry runs the CURRENT closure (the
// application rebuilds them every frame), a frame whose words did not change
// leaves the popover alone, and one that did swaps it in place.
func TestGTKBackendContextMenu(t *testing.T) {
	win, err := Open("gtk menu test", 320, 200, nil, 1)
	if err != nil {
		t.Skipf("no GTK display: %v", err)
	}
	defer win.Close()

	var picked []string
	mk := func(tag string, labels ...string) func() []toolkit.NativeControl {
		return func() []toolkit.NativeControl {
			var items []toolkit.NativeMenuItem
			for _, l := range labels {
				switch l {
				case "":
					items = append(items, toolkit.NativeMenuItem{})
				case "Remove":
					items = append(items, toolkit.NativeMenuItem{Label: l}) // inert
				default:
					items = append(items, toolkit.NativeMenuItem{Label: l, Pick: func() { picked = append(picked, tag+l) }})
				}
			}
			return []toolkit.NativeControl{{
				Kind: toolkit.NativeButton, Key: "b", Text: "Row", Visible: true,
				Rect: toolkit.Rect{X: 10, Y: 10, W: 120, H: 28}, Menu: items,
			}}
		}
	}

	win.root = surfaceOf(mk("a", "Open", "", "Remove", "Rename"))
	win.win.Present()
	pump(20)
	win.frame()
	lc := win.native["b"]
	if lc == nil || lc.menu == nil {
		t.Fatal("no context menu was attached to the button")
	}
	if got := lc.menu.Len(); got != 3 {
		t.Fatalf("menu Len = %d, want 3 (separator not counted)", got)
	}
	first := lc.menu

	lc.menu.Popup(5, 5)
	if !lc.menu.IsOpen() {
		t.Fatal("the popover did not open")
	}

	// New closures, same words: the popover is kept, and a pick runs the new one.
	win.root = surfaceOf(mk("b", "Open", "", "Remove", "Rename"))
	win.frame()
	if lc.menu != first || !lc.menu.IsOpen() {
		t.Error("a frame with the same words rebuilt or closed the open popover")
	}
	if !lc.menu.Activate(0) || lc.menu.Activate(2) || lc.menu.Activate(1) {
		t.Error("Activate: want the live entry accepted and the inert and the separator refused")
	}
	pump(5)
	if len(picked) != 1 || picked[0] != "bOpen" {
		t.Errorf("picked = %v, want [bOpen] (the current closure)", picked)
	}

	// Other words: swapped in place.
	win.root = surfaceOf(mk("c", "Copy"))
	win.frame()
	if lc.menu.Len() != 1 {
		t.Errorf("Len after the words changed = %d, want 1", lc.menu.Len())
	}

	// No menu any more: nothing to open.
	win.root = surfaceOf(mk("d"))
	win.frame()
	if lc.menu.Len() != 0 {
		t.Errorf("Len with no menu = %d, want 0", lc.menu.Len())
	}

	// Reconciling the control away destroys the popover before its parent.
	win.root = surfaceOf(func() []toolkit.NativeControl { return nil })
	win.frame()
	if _, ok := win.native["b"]; ok || lc.menu != nil {
		t.Error("control or popover survived reconcile")
	}
}

// TestGTKBackendButtonImage proves a button takes a picture, alone or beside its
// caption, only when the PNG changed, and keeps its caption for a bad one.
func TestGTKBackendButtonImage(t *testing.T) {
	win, err := Open("gtk image test", 320, 200, nil, 1)
	if err != nil {
		t.Skipf("no GTK display: %v", err)
	}
	defer win.Close()

	good := testPNG(t)
	img, only := good, true
	win.root = surfaceOf(func() []toolkit.NativeControl {
		return []toolkit.NativeControl{{
			Kind: toolkit.NativeButton, Key: "b", Text: "Save", Visible: true,
			Rect: toolkit.Rect{X: 10, Y: 10, W: 80, H: 32}, Image: img, ImageOnly: only,
		}}
	})
	win.frame()
	lc := win.native["b"]
	if lc == nil || lc.pic == 0 {
		t.Fatal("the button has no picture")
	}
	pic := lc.pic
	win.frame()
	if lc.pic != pic {
		t.Error("an unchanged PNG was decoded and replaced again")
	}

	only = false // picture beside the caption
	win.frame()
	if lc.pic == pic {
		t.Error("ImageOnly flipped but the button child was not rebuilt")
	}

	// A PNG that cannot be read, and one over the bound: the caption is kept and
	// nothing panics.
	for _, bad := range [][]byte{[]byte("not a png"), make([]byte, 2<<20)} {
		before := lc.pic
		img = bad
		win.frame()
		if lc.pic != before {
			t.Errorf("a refused picture (%d bytes) replaced the good one", len(bad))
		}
	}

	img = nil // the picture is withdrawn: caption again
	win.frame()
}

// TestGTKBackendHideOnClose proves the user's close hides the window under
// HideOnClose and lets it through otherwise, and that Show/Hide/Raise work and
// refuse once the window is closing.
func TestGTKBackendHideOnClose(t *testing.T) {
	win, err := Open("gtk hide test", 320, 200, nil, 1)
	if err != nil {
		t.Skipf("no GTK display: %v", err)
	}
	win.loop = gtk4.MainLoopNew()
	win.win.OnCloseRequest(win.onCloseRequest)
	win.win.Present()
	pump(20)
	if !win.win.Mapped() {
		t.Fatal("the window did not map")
	}

	win.SetHideOnClose(true)
	win.win.Close() // the user's close control
	pump(10)
	if win.win.Mapped() {
		t.Error("HideOnClose: the close did not hide the window")
	}
	if win.closing.Load() {
		t.Error("HideOnClose: a user close ended the window")
	}

	if err := win.Show(); err != nil {
		t.Fatalf("Show: %v", err)
	}
	pump(10)
	if !win.win.Mapped() {
		t.Error("Show did not map the window")
	}
	if err := win.Hide(); err != nil {
		t.Fatalf("Hide: %v", err)
	}
	pump(10)
	if win.win.Mapped() {
		t.Error("Hide did not unmap the window")
	}
	if err := win.Raise(); err != nil {
		t.Fatalf("Raise: %v", err)
	}
	pump(10)
	if !win.win.Mapped() {
		t.Error("Raise did not bring a hidden window back")
	}

	if err := win.Close(); err != nil {
		t.Fatal(err)
	}
	for name, call := range map[string]func() error{"Show": win.Show, "Hide": win.Hide, "Raise": win.Raise} {
		if err := call(); !errors.Is(err, ErrClosed) {
			t.Errorf("%s after Close = %v, want ErrClosed", name, err)
		}
	}
}
