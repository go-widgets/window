// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin && integration

// This is the live proof that a window lands on the display the caller chose.
//
// The interesting claim is not "a borderless window was created" — it is "it
// covers THAT panel and no other". Rendering a picture proves neither, so the
// assertions here are geometric and identity-based: the created NSWindow's
// frame must equal, to the point, the frame AppKit reported for the target
// screen, and -[NSWindow screen] must come back as the same display by name.
// A screenshot of the panel is saved alongside as an artefact, best-effort,
// because it is the thing a human can check.
package cocoa

import (
	"fmt"
	"os"
	"os/exec"
	"testing"
	"time"

	objc "github.com/go-macos/objc"

	"github.com/go-widgets/toolkit"
)

var (
	selWindowScreen = objc.RegisterName("screen")
	selFrameOfWin   = objc.RegisterName("frame")
)

// TestLiveFullscreenLandsOnEveryAttachedScreen opens a borderless fullscreen
// window on each attached display in turn and asserts it is really there.
func TestLiveFullscreenLandsOnEveryAttachedScreen(t *testing.T) {
	if os.Getenv("WINDOW_COCOA_INTEGRATION") == "" {
		t.Skip("set WINDOW_COCOA_INTEGRATION=1 to run the live Cocoa screen tests")
	}
	screens, err := Screens()
	if err != nil {
		t.Fatalf("Screens() = %v", err)
	}
	if len(screens) == 0 {
		t.Skip("no display attached")
	}
	t.Logf("%d display(s) attached", len(screens))
	for _, s := range screens {
		t.Logf("  %q %dx%d at (%d,%d) scale %.1f primary=%v",
			s.Name, s.Width, s.Height, s.X, s.Y, s.Scale, s.Primary)
	}

	for i := range screens {
		target := screens[i]
		t.Run(fmt.Sprintf("screen%d", i), func(t *testing.T) {
			var (
				win    *Window
				openEr error
				frame  nsRect
				onName string
			)
			callOnMain(func() {
				win, openEr = NewWithOptions(Options{
					Title:      "go-widgets fullscreen placement proof",
					Theme:      nil,
					Screen:     &target,
					Fullscreen: true,
				})
				if openEr != nil {
					return
				}
				frame = objc.Send[nsRect](win.win, selFrameOfWin)
				if sc := objc.Send[objc.ID](win.win, selWindowScreen); sc != 0 {
					onName = objc.GoString(objc.Send[objc.ID](sc, selLocalizedName))
				}
			})
			if openEr != nil {
				t.Fatalf("NewWithOptions(fullscreen on %q) = %v", target.Name, openEr)
			}
			defer callOnMain(func() { win.Close() })

			// The window must cover exactly the panel: same origin, same size, in
			// AppKit's own coordinates, with no arithmetic in between.
			want := target.nativeFrame
			if frame != want {
				t.Errorf("window frame = %+v, want the screen's own frame %+v", frame, want)
			}
			// And AppKit must agree about WHICH display that is.
			if onName != target.Name {
				t.Errorf("-[NSWindow screen] reports %q, want %q", onName, target.Name)
			}
			// The framebuffer must be sized for the whole panel, or the window is
			// covering the display while drawing into part of it.
			gotW, gotH := win.Size()
			wantW := int(float64(target.Width) * win.RenderScale())
			wantH := int(float64(target.Height) * win.RenderScale())
			if gotW != wantW || gotH != wantH {
				t.Errorf("framebuffer = %dx%d, want %dx%d (%d x scale %.1f)",
					gotW, gotH, wantW, wantH, target.Width, win.RenderScale())
			}
			t.Logf("covered %q: frame %+v, framebuffer %dx%d", onName, frame, gotW, gotH)

			// Artefact for a human: a shot of the panel itself. Best-effort — it
			// needs screen-recording consent and is not an assertion.
			out := fmt.Sprintf("cocoa-fullscreen-screen%d.png", i)
			if err := exec.Command("screencapture", "-x", fmt.Sprintf("-D%d", i+1), out).Run(); err != nil {
				t.Logf("screencapture unavailable (%v); geometric assertions above stand alone", err)
			} else {
				t.Logf("panel capture written to %s", out)
			}
		})
	}
}

// TestLiveFullscreenRefusesAScreenThatIsGone asserts the failure mode that
// matters for an XR headset: a display named in a stale value must produce an
// error, never a window somewhere else.
func TestLiveFullscreenRefusesAScreenThatIsGone(t *testing.T) {
	if os.Getenv("WINDOW_COCOA_INTEGRATION") == "" {
		t.Skip("set WINDOW_COCOA_INTEGRATION=1 to run the live Cocoa screen tests")
	}
	gone := ScreenInfo{Name: "unplugged panel", X: 9999, Y: 9999, Width: 1280, Height: 720}
	var err error
	callOnMain(func() {
		var w *Window
		w, err = NewWithOptions(Options{Title: "must not open", Screen: &gone, Fullscreen: true})
		if w != nil {
			w.Close()
		}
	})
	if err == nil {
		t.Fatal("NewWithOptions on an unattached screen returned no error; a window was placed somewhere it was not asked to be")
	}
	t.Logf("correctly refused: %v", err)
}

// TestLiveCloseEndsRun is the regression test for a bug that made a full-screen
// immersive window impossible to end.
//
// -[NSWindow close] does not invoke -windowShouldClose:, which is the delegate
// callback that stops the run loop; that one is only sent for a close the USER
// initiates. So Close() used to tear the window down and leave [NSApp run]
// spinning on an empty application, and Run never returned. A borderless
// full-screen window has no close button, so closing itself is the ONLY way such
// an application can end — the bug turned "stop playing" into "hang".
func TestLiveCloseEndsRun(t *testing.T) {
	if os.Getenv("WINDOW_COCOA_INTEGRATION") == "" {
		t.Skip("set WINDOW_COCOA_INTEGRATION=1 to run the live Cocoa screen tests")
	}
	screens, err := Screens()
	if err != nil {
		t.Fatalf("Screens() = %v", err)
	}

	cases := []struct {
		name string
		opts Options
	}{
		{"titled window", Options{Title: "close-ends-run", Width: 320, Height: 200}},
	}
	// Also cover the case that motivated the fix, on a NON-primary display when
	// there is one, so the test does not take over the screen being worked on.
	for i := range screens {
		if !screens[i].Primary {
			cases = append(cases, struct {
				name string
				opts Options
			}{
				"borderless fullscreen on " + screens[i].Name,
				Options{Title: "close-ends-run", Screen: &screens[i], Fullscreen: true},
			})
			break
		}
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := toolkit.NewSurface(func() ([]byte, int, int) { return nil, 0, 0 })
			done := make(chan error, 1)

			// Run must be called on the reserved main thread and blocks there, so
			// it is submitted rather than called through callOnMain: this
			// goroutine has to stay free to time out.
			mainfuncs <- func() {
				w, err := NewWithOptions(tc.opts)
				if err != nil {
					done <- err
					return
				}
				go func() {
					time.Sleep(300 * time.Millisecond)
					w.Close()
				}()
				done <- w.Run(root)
			}

			select {
			case err := <-done:
				if err != nil {
					t.Fatalf("Run = %v", err)
				}
				t.Log("Run returned after Close, as it must")
			case <-time.After(15 * time.Second):
				// Do not let a hung main thread wedge the rest of the suite
				// silently: say what happened.
				t.Fatal("Run did not return within 15s of Close(); the run loop was not stopped")
			}
		})
	}
}
