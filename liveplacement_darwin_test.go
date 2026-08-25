// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin && integration

// The live proof that Config.Screen means the display the caller named, through
// the PUBLIC Open, with the desktop rearranged underneath the way a real one is.
//
// The bug this exists for could not be seen from inside internal/cocoa. That
// package's own live test opens a window on every attached screen and passes,
// because it lists the displays and opens the window in the same breath. A real
// application does not: it lists the displays, chooses one, and opens a window
// some time later — and in between, something changes the arrangement. Creating
// a virtual display does. Plugging in a monitor does. Waking a headset does.
//
// +[NSScreen screens] is a cache that a process with no running NSApplication
// never refreshes, so the arrangement it reports in that window of time is the
// one that was true when it was first read. Every reading agrees with every
// other, so nothing looks wrong; the value is simply false. A window placed
// from it went to whatever is now at those coordinates — measured on 2026-08-25
// as a full-screen surface meant for a VITURE Beast appearing on the operator's
// laptop screen, in a window, with no error raised anywhere.
//
// So the assertions here are made against the WINDOW SERVER's own display list
// (github.com/go-macos/virtualdisplay's ActiveDisplays, CoreGraphics underneath,
// with no cache in front of it) rather than against anything AppKit says, and
// the window's position is read as metadata — Placement.Bounds — rather than by
// photographing the operator's desk.
package window

import (
	"fmt"
	"os"
	"runtime"
	"testing"
	"time"

	objc "github.com/go-macos/objc"
	"github.com/go-macos/virtualdisplay"
	"github.com/go-widgets/toolkit"
)

// --- main-thread plumbing --------------------------------------------------
//
// NSApplication and NSWindow may only be touched from the process main OS
// thread, and a Go test runs on whatever thread the scheduler picked. So the
// main thread is reserved here and every call that reaches AppKit is funnelled
// onto it, exactly as internal/cocoa's own live suite does. Assertions stay on
// the test goroutine: t.Fatal unwinds with runtime.Goexit, which on the
// reserved thread would kill the pump and hang the run.

var mainfuncs = make(chan func())

func TestMain(m *testing.M) {
	runtime.LockOSThread()
	go func() { os.Exit(m.Run()) }()
	for f := range mainfuncs {
		f()
	}
}

// callOnMain runs f on the reserved main OS thread and blocks until it returns.
func callOnMain(f func()) {
	done := make(chan struct{})
	mainfuncs <- func() {
		f()
		close(done)
	}
	<-done
}

// mainScreens is Screens() on the reserved thread.
func mainScreens(t *testing.T) []Screen {
	t.Helper()
	var (
		ss  []Screen
		err error
	)
	callOnMain(func() { ss, err = Screens() })
	if err != nil {
		t.Fatalf("Screens() = %v", err)
	}
	return ss
}

// mainOpen is Open() on the reserved thread.
func mainOpen(cfg Config) (Backend, error) {
	var (
		b   Backend
		err error
	)
	callOnMain(func() { b, err = Open(cfg) })
	return b, err
}

// closeWindow closes the window AND turns the run loop, so it is really gone
// before the display it was covering is destroyed. -[NSWindow close] is queued
// onto the main thread with waitUntilDone NO, and nothing here runs an
// NSApplication that would otherwise get to it — which would leave a borderless
// window behind, migrated onto the operator's own screen when its display went
// away, for the rest of the process.
func closeWindow(t *testing.T, win Backend) {
	t.Helper()
	var err error
	callOnMain(func() {
		err = win.Close()
		rl := objc.ID(objc.GetClass("NSRunLoop")).Send(objc.RegisterName("currentRunLoop"))
		until := objc.ID(objc.GetClass("NSDate")).
			Send(objc.RegisterName("dateWithTimeIntervalSinceNow:"), 0.2)
		rl.Send(objc.RegisterName("runMode:beforeDate:"),
			objc.NSString("kCFRunLoopDefaultMode"), until)
	})
	if err != nil {
		t.Errorf("Close() = %v", err)
	}
}

// --- staging ---------------------------------------------------------------

// liveEnv gates this file on the same variable internal/cocoa's live suite
// uses, so one setting turns on every on-device proof in the repository.
const liveEnv = "WINDOW_COCOA_INTEGRATION"

// requireLiveDisplays skips unless this is a machine where the test may create
// displays: the private CoreGraphics virtual-display API has to be present, and
// the operator has to have asked for on-device tests.
func requireLiveDisplays(t *testing.T) {
	t.Helper()
	if os.Getenv(liveEnv) == "" {
		t.Skipf("set %s=1 to run the live display-placement proofs", liveEnv)
	}
	var err error
	callOnMain(func() { err = virtualdisplay.Available() })
	if err != nil {
		t.Skipf("virtual displays unavailable on this machine: %v", err)
	}
}

// makeDisplay creates one virtual display and registers its removal, waiting
// for the window server to finish taking it away again: creating a display
// while a previous one is still being removed is refused, which turns the next
// test into a spurious skip.
//
// Virtual rather than real on purpose. The test needs a display it may cover
// entirely with a borderless window, and covering the operator's own screen to
// prove a point is not acceptable.
//
// Every virtualdisplay call is made on the reserved thread. It wraps its work
// in an Objective-C autorelease pool, and a pool must be drained on the thread
// it was created on — which a goroutine free to migrate between threads
// mid-call cannot promise. Left unpinned this crashed the run with SIGSEGV
// inside the pool's drain, a failure that says nothing about anything on test.
func makeDisplay(t *testing.T, name string, w, h uint32) *virtualdisplay.Display {
	t.Helper()
	var (
		d   *virtualdisplay.Display
		err error
	)
	callOnMain(func() {
		d, err = virtualdisplay.Open(virtualdisplay.Spec{Name: name, Width: w, Height: h})
	})
	if err != nil {
		t.Skipf("cannot create a virtual display (%s): %v", name, err)
	}
	t.Cleanup(func() {
		id := d.ID()
		var err error
		callOnMain(func() { err = d.Close() })
		if err != nil {
			t.Errorf("closing virtual display %q: %v", name, err)
			return
		}
		const removeWithin = 20 * time.Second
		deadline := time.Now().Add(removeWithin)
		for time.Now().Before(deadline) {
			ds, err := activeDisplays()
			if err != nil || !has(ds, id) {
				return
			}
			// Turn the run loop while waiting: this process holds a window-server
			// connection by now, and the display goes when AppKit has finished
			// letting go of it.
			callOnMain(func() {
				rl := objc.ID(objc.GetClass("NSRunLoop")).Send(objc.RegisterName("currentRunLoop"))
				until := objc.ID(objc.GetClass("NSDate")).
					Send(objc.RegisterName("dateWithTimeIntervalSinceNow:"), 0.05)
				rl.Send(objc.RegisterName("runMode:beforeDate:"),
					objc.NSString("kCFRunLoopDefaultMode"), until)
			})
		}
		t.Errorf("virtual display %q (%d) was still active %s after Close", name, id, removeWithin)
	})
	return d
}

// activeDisplays is virtualdisplay.ActiveDisplays on the reserved thread, for
// the same reason makeDisplay is.
func activeDisplays() ([]virtualdisplay.DisplayInfo, error) {
	var (
		ds  []virtualdisplay.DisplayInfo
		err error
	)
	callOnMain(func() { ds, err = virtualdisplay.ActiveDisplays() })
	return ds, err
}

// has reports whether the window server still lists this display.
func has(ds []virtualdisplay.DisplayInfo, id uint32) bool {
	for _, d := range ds {
		if d.ID == id {
			return true
		}
	}
	return false
}

// settledDisplays reads the window server's display list until two consecutive
// readings are identical, so the arrangement is not caught mid-shuffle.
//
// macOS does not finish rearranging the desktop by the time a display becomes
// active: the others slide along over the following moments. Waiting for the
// list to stop moving is a fact about the machine; sleeping for a guessed
// interval is not.
func settledDisplays(t *testing.T) []virtualdisplay.DisplayInfo {
	t.Helper()
	const (
		settleFor = 5 * time.Second
		between   = 40 * time.Millisecond
	)
	var last string
	deadline := time.Now().Add(settleFor)
	for {
		ds, err := activeDisplays()
		if err != nil {
			t.Fatalf("ActiveDisplays() = %v", err)
		}
		if now := describe(ds); now == last {
			return ds
		} else {
			last = now
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("the display arrangement was still moving after %s", settleFor)
		}
		time.Sleep(between)
	}
}

// describe renders a display list so two readings can be compared.
func describe(ds []virtualdisplay.DisplayInfo) string {
	s := ""
	for _, d := range ds {
		s += fmt.Sprintf("|%d@%g,%g %gx%g", d.ID, d.X, d.Y, d.Width, d.Height)
	}
	return s
}

// find returns the display with this ID, failing the test when the window
// server no longer reports it.
func find(t *testing.T, ds []virtualdisplay.DisplayInfo, id uint32) virtualdisplay.DisplayInfo {
	t.Helper()
	for _, d := range ds {
		if d.ID == id {
			return d
		}
	}
	t.Fatalf("the window server no longer reports display %d; it lists %s", id, describe(ds))
	return virtualdisplay.DisplayInfo{}
}

// chosenScreen is the application's own step: list the displays and pick the
// one wanted, by name. By NAME, because that is what survives the desktop being
// rearranged — a rectangle does not, which is the whole subject here.
func chosenScreen(t *testing.T, name string) *Screen {
	t.Helper()
	screens := mainScreens(t)
	for i := range screens {
		if screens[i].Name == name {
			return &screens[i]
		}
	}
	seen := make([]string, len(screens))
	for i, s := range screens {
		seen[i] = fmt.Sprintf("%q@%d,%d %dx%d", s.Name, s.X, s.Y, s.Width, s.Height)
	}
	t.Fatalf("Screens() does not list %q; it lists %v", name, seen)
	return nil
}

// bounds is Placement.Bounds on the reserved thread.
func bounds(t *testing.T, win Backend) (x, y, w, h int) {
	t.Helper()
	placed, ok := win.(Placement)
	if !ok {
		t.Fatal("the macOS back-end does not implement Placement; placement cannot be checked")
	}
	callOnMain(func() { x, y, w, h, ok = placed.Bounds() })
	if !ok {
		t.Fatal("Bounds() could not read the desktop arrangement")
	}
	return x, y, w, h
}

// --- the proof -------------------------------------------------------------

// TestLivePlacementAfterTheDesktopIsRearranged stages the reported failure and
// asserts, through the public API, that it does not happen.
//
// The order is the one an application follows and the one that matters:
//
//  1. a display exists and the application ENUMERATES the displays — this is
//     the step that used to freeze AppKit's idea of the desktop for good;
//  2. the desktop is rearranged underneath, as attaching a panel does;
//  3. the application opens a window on the display it chose.
//
// All three subtests fail with the fix reverted. The enumeration reports the
// arrangement from step 1, so the window is placed at coordinates that by then
// belong to a different panel.
func TestLivePlacementAfterTheDesktopIsRearranged(t *testing.T) {
	requireLiveDisplays(t)

	const targetName = "window placement target"
	target := makeDisplay(t, targetName, 1600, 900)
	beforeMove := find(t, settledDisplays(t), target.ID())

	// Enumerate NOW, while the arrangement is the one above. Whatever AppKit
	// caches, it caches here.
	mainScreens(t)
	t.Logf("the target display is at (%g,%g) %gx%g before the desktop moves",
		beforeMove.X, beforeMove.Y, beforeMove.Width, beforeMove.Height)

	// Move it: three more displays, which macOS lays out alongside and which
	// push the existing ones along. A different size from the target, so no
	// stale rectangle can accidentally coincide with a real one.
	for i := 1; i <= 3; i++ {
		makeDisplay(t, fmt.Sprintf("window placement crowd %d", i), 1024, 768)
	}
	live := settledDisplays(t)
	afterMove := find(t, live, target.ID())
	t.Logf("the target display is now at (%g,%g) %gx%g",
		afterMove.X, afterMove.Y, afterMove.Width, afterMove.Height)
	if afterMove.X == beforeMove.X && afterMove.Y == beforeMove.Y {
		t.Skip("macOS did not move the target display; this machine cannot stage the failure")
	}

	t.Run("screens follow the window server", func(t *testing.T) {
		got := mainScreens(t)
		if len(got) != len(live) {
			t.Fatalf("Screens() reports %d display(s), the window server has %d (%s); "+
				"the enumeration is answering from a cache that stopped being true",
				len(got), len(live), describe(live))
		}
		for _, d := range live {
			var match *Screen
			for i := range got {
				if got[i].X == int(d.X) && got[i].Y == int(d.Y) &&
					got[i].Width == int(d.Width) && got[i].Height == int(d.Height) {
					match = &got[i]
					break
				}
			}
			if match == nil {
				t.Errorf("no screen reported at the window server's %gx%g at (%g,%g) for display %d",
					d.Width, d.Height, d.X, d.Y, d.ID)
				continue
			}
			if match.Primary != d.Main {
				t.Errorf("screen %q reports Primary=%v, the window server says main=%v",
					match.Name, match.Primary, d.Main)
			}
		}
	})

	t.Run("a fullscreen window covers the chosen display", func(t *testing.T) {
		chosen := chosenScreen(t, targetName)
		win, err := mainOpen(Config{
			Title:       "placement proof",
			Screen:      chosen,
			Fullscreen:  true,
			RenderScale: NativeScale,
			Theme:       toolkit.DefaultDark(),
		})
		if err != nil {
			t.Fatalf("Open(fullscreen on %q) = %v", targetName, err)
		}
		defer closeWindow(t, win)

		x, y, w, h := bounds(t, win)
		t.Logf("the window is at (%d,%d) %dx%d", x, y, w, h)

		// THE assertion: the window covers the display the caller named, where
		// that display is NOW — read from the window server, not from anything
		// the code under test consulted.
		now := find(t, settledDisplays(t), target.ID())
		if x != int(now.X) || y != int(now.Y) || w != int(now.Width) || h != int(now.Height) {
			t.Errorf("the window is at (%d,%d) %dx%d, but %q is at (%g,%g) %gx%g; "+
				"a full-screen window went to a panel the caller did not choose",
				x, y, w, h, targetName, now.X, now.Y, now.Width, now.Height)
		}

		// And the framebuffer must be the whole panel, or the window covers the
		// display while drawing into part of it.
		fw, fh := win.Size()
		wantW, wantH := int(now.Width*chosen.Scale), int(now.Height*chosen.Scale)
		if fw != wantW || fh != wantH {
			t.Errorf("framebuffer %dx%d, want %dx%d (%gx%g at scale %.1f)",
				fw, fh, wantW, wantH, now.Width, now.Height, chosen.Scale)
		}
	})

	// The windowed path shares the origin arithmetic with the full-screen one,
	// so it shared the defect — and nothing exercised it.
	t.Run("a titled window opens on the chosen display", func(t *testing.T) {
		chosen := chosenScreen(t, targetName)
		const cw, ch = 640, 400
		win, err := mainOpen(Config{
			Title:  "windowed placement proof",
			Width:  cw,
			Height: ch,
			Screen: chosen,
			Theme:  toolkit.DefaultDark(),
		})
		if err != nil {
			t.Fatalf("Open(windowed on %q) = %v", targetName, err)
		}
		defer closeWindow(t, win)

		x, y, _, _ := bounds(t, win)
		now := find(t, settledDisplays(t), target.ID())
		t.Logf("the titled window is at (%d,%d); %q spans (%g,%g)+(%gx%g)",
			x, y, targetName, now.X, now.Y, now.Width, now.Height)

		// A titled window is placed with its CONTENT's top-left at the display's
		// top-left, so the frame — which carries the title bar above the content
		// — starts a title bar higher. Asserting an exact origin would pin the
		// height of AppKit's chrome; asserting the display is the point.
		if x < int(now.X) || x >= int(now.X+now.Width) {
			t.Errorf("the window's x=%d is not within %q, which spans %g..%g",
				x, targetName, now.X, now.X+now.Width)
		}
		if y+ch <= int(now.Y) || y >= int(now.Y+now.Height) {
			t.Errorf("the window's y=%d..%d does not overlap %q, which spans %g..%g",
				y, y+ch, targetName, now.Y, now.Y+now.Height)
		}
	})
}
