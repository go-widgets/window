// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"testing"

	"github.com/go-widgets/toolkit"
)

// swapA11ySeams installs stand-ins for the two bridge entry points and returns
// a restore func. The live D-Bus package needs a running accessibility bus, so
// the wiring is unit-tested against these seams and proven end-to-end on the VM.
func swapA11ySeams(take func() []struct{ X, Y int }, pub func(toolkit.Widget, string, int, int)) func() {
	oldTake, oldPub := a11yTakePending, a11yPublish
	a11yTakePending, a11yPublish = take, pub
	return func() { a11yTakePending, a11yPublish = oldTake, oldPub }
}

// TestRefreshA11yReplaysActivationsAndPublishes checks the shared helper: every
// pending activation is replayed as an ordinary click at its point, and the
// tree is then published with the caller's title and origin.
func TestRefreshA11yReplaysActivationsAndPublishes(t *testing.T) {
	want := []struct{ X, Y int }{{11, 22}, {33, 44}}
	var (
		gotRoot      toolkit.Widget
		gotTitle     string
		gotOX, gotOY int
		published    int
		drained      bool
	)
	restore := swapA11ySeams(
		func() []struct{ X, Y int } {
			if drained {
				return nil
			}
			drained = true
			return want
		},
		func(r toolkit.Widget, title string, ox, oy int) {
			gotRoot, gotTitle, gotOX, gotOY = r, title, ox, oy
			published++
		},
	)
	defer restore()

	root := &recWidget{}
	refreshA11y(root, "My App", 7, 9)

	// Both activations reached the root as clicks, at the exact points.
	if len(root.events) != len(want) {
		t.Fatalf("replayed %d events, want %d: %+v", len(root.events), len(want), root.events)
	}
	for i, wp := range want {
		e := root.events[i]
		if e.Kind != toolkit.EventClick || e.X != wp.X || e.Y != wp.Y {
			t.Fatalf("event %d = %+v, want click at (%d,%d)", i, e, wp.X, wp.Y)
		}
	}
	// The tree was published once, with the caller's title and origin.
	if published != 1 || gotTitle != "My App" || gotOX != 7 || gotOY != 9 {
		t.Fatalf("publish: n=%d title=%q origin=(%d,%d)", published, gotTitle, gotOX, gotOY)
	}
	if gotRoot != root {
		t.Fatalf("publish got a different root")
	}
}

// TestRefreshA11yNilRootDropsActivations checks the guard: with no bound root a
// pending activation is simply discarded (never dispatched into a nil tree) and
// the publish still happens.
func TestRefreshA11yNilRootDropsActivations(t *testing.T) {
	var published bool
	restore := swapA11ySeams(
		func() []struct{ X, Y int } { return []struct{ X, Y int }{{1, 2}} },
		func(toolkit.Widget, string, int, int) { published = true },
	)
	defer restore()

	refreshA11y(nil, "", 0, 0) // must not panic
	if !published {
		t.Fatal("publish should run even with a nil root")
	}
}

// TestWlPaintFrameRefreshesA11y checks the Wayland call site: paintFrame drives
// refreshA11y with the window's title and a (0,0) origin — a Wayland client
// cannot know its position on screen.
func TestWlPaintFrameRefreshesA11y(t *testing.T) {
	var gotTitle string
	var gotOX, gotOY int
	var called bool
	restore := swapA11ySeams(
		func() []struct{ X, Y int } { return nil },
		func(_ toolkit.Widget, title string, ox, oy int) {
			gotTitle, gotOX, gotOY, called = title, ox, oy, true
		},
	)
	defer restore()

	// A plain (non-incremental) root, not yet configured: present() no-ops, so
	// paintFrame exercises the a11y refresh and the draw with no compositor.
	w := &wlWindow{w: 8, h: 6, buf: make([]byte, 4*8*6), theme: toolkit.DefaultDark(),
		root: &recWidget{}, title: "WL App"}
	if err := w.paintFrame(); err != nil {
		t.Fatalf("paintFrame: %v", err)
	}
	if !called || gotTitle != "WL App" || gotOX != 0 || gotOY != 0 {
		t.Fatalf("wl refresh: called=%v title=%q origin=(%d,%d)", called, gotTitle, gotOX, gotOY)
	}
}

// TestX11PaintFrameRefreshesA11y checks the X11 call site: paintFrame drives
// refreshA11y with the window's real screen origin (X11, unlike Wayland, knows
// where its window sits).
func TestX11PaintFrameRefreshesA11y(t *testing.T) {
	var gotTitle string
	var gotOX, gotOY int
	restore := swapA11ySeams(
		func() []struct{ X, Y int } { return nil },
		func(_ toolkit.Widget, title string, ox, oy int) { gotTitle, gotOX, gotOY = title, ox, oy },
	)
	defer restore()

	w, _ := dialFake(t, Config{Title: "X11 App", Width: 40, Height: 30})
	w.root = &recWidget{}
	w.originX, w.originY = 100, 200
	if err := w.paintFrame(false, false); err != nil {
		t.Fatalf("paintFrame: %v", err)
	}
	if gotTitle != "X11 App" || gotOX != 100 || gotOY != 200 {
		t.Fatalf("x11 refresh: title=%q origin=(%d,%d)", gotTitle, gotOX, gotOY)
	}
}
