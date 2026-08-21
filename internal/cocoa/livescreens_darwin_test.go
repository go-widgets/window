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

	objc "github.com/go-macos/objc"
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
