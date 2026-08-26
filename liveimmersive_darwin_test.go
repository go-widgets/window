// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin && integration

// The live proof that Config.Immersive puts the window above the menu bar and
// the Dock rather than underneath them.
//
// A borderless full-screen window covers the DESKTOP and nothing more. The menu
// bar and the Dock are drawn by the window server at levels an ordinary window
// never reaches, so on a display that carries them they sit ON TOP of the
// picture — which on glasses showing a captured desktop reads as two menu bars,
// one of them belonging to a screen the viewer is not looking at. It was
// reported that way from a pair of VITURE Luma Ultra on 2026-08-26.
//
// The level is asserted through CGWindowListCopyWindowInfo, which is the WINDOW
// SERVER's own account of what it is compositing and in what order. That is a
// different instrument from the one that wrote the value: -[NSWindow setLevel:]
// went in through AppKit, and asking AppKit to read it back would prove only
// that a setter and a getter agree. The same listing carries the menu bar's and
// the Dock's own layers, so the comparison is against what is actually there
// rather than against a constant typed out of a header.
package window

import (
	"testing"

	objc "github.com/go-macos/objc"
)

// windowLayers returns the window server's layer for every window of this
// process, and the layers it is using for the menu bar and the Dock.
func windowLayers(t *testing.T) (ours map[string]int, menuBar, dock int) {
	ours = map[string]int{}
	t.Helper()
	menuBar, dock = -1, -1

	// kCGWindowListOptionOnScreenOnly | kCGWindowListExcludeDesktopElements is
	// NOT used: the menu bar and the Dock ARE desktop elements, and they are
	// the two things being compared against.
	const kCGWindowListOptionAll = 0
	const kCGNullWindowID = 0

	list := cgWindowList(kCGWindowListOptionAll, kCGNullWindowID)
	if list == 0 {
		t.Skip("CGWindowListCopyWindowInfo returned nothing; this process may " +
			"not be allowed to enumerate windows")
	}
	defer objc.ID(list).Send(objc.RegisterName("release"))

	me := int(getpid())
	count := int(objc.ID(list).Send(objc.RegisterName("count")))
	for i := 0; i < count; i++ {
		d := objc.ID(list).Send(objc.RegisterName("objectAtIndex:"), i)
		pid := numberFor(d, "kCGWindowOwnerPID")
		layer := numberFor(d, "kCGWindowLayer")
		name := stringFor(d, "kCGWindowOwnerName")
		switch {
		case pid == me:
			// By TITLE. A window this process closed a moment ago is still in the
			// listing, and "every window of ours" would then assert about it too.
			ours[stringFor(d, "kCGWindowName")] = layer
		case name == "Window Server" && stringFor(d, "kCGWindowName") == "Menubar":
			menuBar = layer
		case name == "Dock" && stringFor(d, "kCGWindowName") == "Dock":
			dock = layer
		}
	}
	return ours, menuBar, dock
}

// pump turns the run loop, so that AppKit commits what it was told.
//
// This is not a sleep dressed up. -[NSWindow setLevel:] is recorded by AppKit
// immediately -- -[NSWindow level] reads back 25 at once -- and handed to the
// window server only when the application next runs. A test that opens a window
// and asks CoreGraphics straight away is measuring a change that has not been
// made yet, and it reads 0 no matter how long it waits: the first version of
// this test did exactly that, and blamed the window level.
func pump(seconds float64) {
	callOnMain(func() {
		rl := objc.ID(objc.GetClass("NSRunLoop")).Send(objc.RegisterName("currentRunLoop"))
		until := objc.ID(objc.GetClass("NSDate")).
			Send(objc.RegisterName("dateWithTimeIntervalSinceNow:"), seconds)
		rl.Send(objc.RegisterName("runUntilDate:"), until)
	})
}

// TestLiveAnImmersiveWindowIsAboveTheMenuBarAndTheDock.
func TestLiveAnImmersiveWindowIsAboveTheMenuBarAndTheDock(t *testing.T) {
	requireLiveDisplays(t)

	// The control first, because "above" is worth nothing without the reading it
	// is above. An ordinary window sits at layer 0.
	plain, err := mainOpen(Config{Title: "window level control", Width: 320, Height: 200})
	if err != nil {
		t.Fatalf("opening the control window: %v", err)
	}
	pump(0.4)
	control, menuBar, dock := windowLayers(t)
	closeWindow(t, plain)

	if menuBar < 0 {
		t.Skip("the window server is not reporting a menu bar on this machine")
	}
	l, ok := control["window level control"]
	if !ok {
		t.Fatalf("the control window is not in the window server's own listing: %v", control)
	}
	if l >= menuBar {
		t.Fatalf("an ORDINARY window is already at layer %d, at or above the "+
			"menu bar's %d; the premise of this test is wrong", l, menuBar)
	}
	t.Logf("ordinary %d, menu bar %d, dock %d", l, menuBar, dock)

	// And now the thing itself.
	win, err := mainOpen(Config{
		Title: "immersive", Width: 320, Height: 200, Immersive: true,
	})
	if err != nil {
		t.Fatalf("opening the immersive window: %v", err)
	}
	defer closeWindow(t, win)
	pump(0.4)

	ours, menuBar2, dock2 := windowLayers(t)
	im, ok := ours["immersive"]
	if !ok {
		t.Fatalf("the immersive window is not in the window server's listing: %v", ours)
	}
	if menuBar2 >= 0 && im <= menuBar2 {
		t.Errorf("the immersive window is at layer %d, not above the menu bar's %d",
			im, menuBar2)
	}
	if dock2 >= 0 && im <= dock2 {
		t.Errorf("the immersive window is at layer %d, not above the Dock's %d",
			im, dock2)
	}
	t.Logf("immersive %d, menu bar %d, dock %d", im, menuBar2, dock2)
}
