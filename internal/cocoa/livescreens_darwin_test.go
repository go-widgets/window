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
	"path/filepath"
	"strconv"
	"strings"
	"sync"
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
			// AppKit's own coordinates.
			primary, err := primaryBounds()
			if err != nil {
				t.Fatalf("primaryBounds() = %v", err)
			}
			want := target.appKitFrame(primary.H)
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
			//
			// It is a picture of WHOEVER RAN THIS, at work, and this repository is
			// public. It used to be written into internal/cocoa as an untracked
			// file — one `git add -A` from publication — and that is what this
			// directory refusal exists to stop.
			out := filepath.Join(captureDir(t), fmt.Sprintf("cocoa-fullscreen-screen%d.png", i))
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

// TestLiveBorderlessWindowCanBecomeKey is the diagnosis of a bug that made a
// full-screen window look finished and be unusable.
//
// AppKit's -[NSWindow canBecomeKeyWindow] returns NO for a BORDERLESS window
// unless a subclass says otherwise. A window that cannot become key receives no
// keyboard events and no -mouseMoved:, so a full-screen player built on
// Config.Fullscreen showed its picture perfectly and then ignored every key and
// never saw the pointer — its controls could not be summoned and Escape could
// not close it.
//
// The titled case is checked alongside, because a fix that made only the
// borderless one work by breaking the other would pass a one-sided test.
func TestLiveBorderlessWindowCanBecomeKey(t *testing.T) {
	if os.Getenv("WINDOW_COCOA_INTEGRATION") == "" {
		t.Skip("set WINDOW_COCOA_INTEGRATION=1 to run the live Cocoa screen tests")
	}
	selCanBecomeKey := objc.RegisterName("canBecomeKeyWindow")
	selIsKey := objc.RegisterName("isKeyWindow")
	selAcceptsMoved := objc.RegisterName("acceptsMouseMovedEvents")

	screens, err := Screens()
	if err != nil {
		t.Fatalf("Screens() = %v", err)
	}

	cases := []struct {
		name string
		opts Options
	}{
		{"titled window", Options{Title: "key-window", Width: 320, Height: 200}},
	}
	for i := range screens {
		if !screens[i].Primary {
			cases = append(cases, struct {
				name string
				opts Options
			}{
				"borderless fullscreen on " + screens[i].Name,
				Options{Title: "key-window", Screen: &screens[i], Fullscreen: true},
			})
			break
		}
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var canKey, isKey, moved bool
			callOnMain(func() {
				w, err := NewWithOptions(tc.opts)
				if err != nil {
					t.Errorf("NewWithOptions = %v", err)
					return
				}
				defer w.Close()
				canKey = objc.Send[bool](w.win, selCanBecomeKey)
				isKey = objc.Send[bool](w.win, selIsKey)
				moved = objc.Send[bool](w.win, selAcceptsMoved)
			})
			t.Logf("%s: canBecomeKeyWindow=%v isKeyWindow=%v acceptsMouseMoved=%v",
				tc.name, canKey, isKey, moved)
			if !canKey {
				t.Errorf("%s: canBecomeKeyWindow is false; this window can receive no keys and no pointer motion", tc.name)
			}
			if !moved {
				t.Errorf("%s: acceptsMouseMovedEvents is false; hover will never reach the widget tree", tc.name)
			}
		})
	}
}

// eventRecorder is a widget that remembers every event kind it is handed. It
// draws nothing: this test is about delivery, not appearance.
type eventRecorder struct {
	toolkit.Base
	mu    sync.Mutex
	kinds []toolkit.EventKind
}

func (r *eventRecorder) OnEvent(ev toolkit.Event) {
	r.mu.Lock()
	r.kinds = append(r.kinds, ev.Kind)
	r.mu.Unlock()
}

func (r *eventRecorder) saw(k toolkit.EventKind) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, got := range r.kinds {
		if got == k {
			return true
		}
	}
	return false
}

// deliveryCaseEnv names the one delivery case a child process should run.
//
// Each case needs its own process, and that is not fussiness. Closing a window
// stops NSApp — that is precisely how a closing window ends Run — and nothing
// restarts it, so a SECOND window in the same process is ordered front and never
// becomes visible. It then reports that no event arrived, for a reason that has
// nothing to do with the window under test. That false failure cost an evening
// once; rather than leave the trap set, the parent re-runs itself per case.
const deliveryCaseEnv = "WINDOW_COCOA_DELIVERY_CASE"

// TestLiveFullscreenReceivesMouseMovedAndKeys is the protocol that should have
// existed before a full-screen application was called finished.
//
// A window can display a perfect picture and be entirely deaf, and nothing about
// the picture says so. So this posts REAL NSEvents — a mouse move and a key —
// at a borderless full-screen window and asserts the widget tree was handed
// them. Both shapes are checked, because a titled window that works proves
// nothing about the one this feature exists to make.
func TestLiveFullscreenReceivesMouseMovedAndKeys(t *testing.T) {
	if os.Getenv("WINDOW_COCOA_INTEGRATION") == "" {
		t.Skip("set WINDOW_COCOA_INTEGRATION=1 to run the live Cocoa screen tests")
	}
	cases, err := deliveryCases()
	if err != nil {
		t.Fatalf("Screens() = %v", err)
	}

	// A child process: run the one case it was given, in this process, alone.
	if v := os.Getenv(deliveryCaseEnv); v != "" {
		i, err := strconv.Atoi(v)
		if err != nil || i < 0 || i >= len(cases) {
			t.Fatalf("%s=%q does not name one of the %d cases", deliveryCaseEnv, v, len(cases))
		}
		runDeliveryCase(t, cases[i])
		return
	}

	// The parent: one child per case, output passed through so a failure reads
	// the same as it would if it had happened here.
	for i, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0],
				"-test.run=^TestLiveFullscreenReceivesMouseMovedAndKeys$",
				"-test.v", "-test.timeout=2m")
			cmd.Env = append(os.Environ(), fmt.Sprintf("%s=%d", deliveryCaseEnv, i))
			out, err := cmd.CombinedOutput()
			for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
				t.Log(line)
			}
			if err != nil {
				t.Errorf("case %q failed in its own process: %v", tc.name, err)
			}
		})
	}
}

type deliveryCase struct {
	name string
	opts Options
}

// deliveryCases is the titled window, plus a borderless full-screen window on a
// secondary display if the machine has one. The secondary is what the feature
// exists for; the titled one is the control that says the harness itself works.
func deliveryCases() ([]deliveryCase, error) {
	screens, err := Screens()
	if err != nil {
		return nil, err
	}
	cases := []deliveryCase{
		{"titled window", Options{Title: "events", Width: 480, Height: 320}},
	}
	for i := range screens {
		if !screens[i].Primary {
			cases = append(cases, deliveryCase{
				"borderless fullscreen on " + screens[i].Name,
				Options{Title: "events", Screen: &screens[i], Fullscreen: true},
			})
			break
		}
	}
	return cases, nil
}

func runDeliveryCase(t *testing.T, tc deliveryCase) {
	t.Helper()
	root := &eventRecorder{}
	var sawMove, sawKey bool
	callOnMain(func() {
		w, err := NewWithOptions(tc.opts)
		if err != nil {
			t.Errorf("NewWithOptions = %v", err)
			return
		}
		defer w.Close()
		w.bindAndSeed(root)

		app := objc.ID(objc.GetClass("NSApplication")).Send(selSharedApplication)
		app.Send(selSetActivationPolicy, 0) // NSApplicationActivationPolicyRegular
		app.Send(selActivateIgnoring, true)
		// Make the window key by hand: outside a running NSApp nothing else
		// will, and a non-key window is deaf whatever its style.
		w.win.Send(objc.RegisterName("makeKeyAndOrderFront:"), objc.ID(0))
		w.pumpPending(app)

		bounds := objc.Send[nsRect](w.view, selBounds)
		mid := nsPoint{X: bounds.Size.W / 2, Y: bounds.Size.H / 2}
		w.postMouse(app, nsEventTypeMouseMoved, mid)
		w.postKey(app, nsEventTypeKeyDown, " ", 49) // space

		sawMove = root.saw(toolkit.EventMouseMove)
		sawKey = root.saw(toolkit.EventKeyDown)
		t.Logf("  isKey=%v isVisible=%v isMain=%v firstResponderIsView=%v appActive=%v",
			objc.Send[bool](w.win, objc.RegisterName("isKeyWindow")),
			objc.Send[bool](w.win, objc.RegisterName("isVisible")),
			objc.Send[bool](w.win, objc.RegisterName("isMainWindow")),
			objc.Send[objc.ID](w.win, objc.RegisterName("firstResponder")) == w.view,
			objc.Send[bool](app, objc.RegisterName("isActive")))
	})
	t.Logf("%s: mouseMoved delivered=%v keyDown delivered=%v", tc.name, sawMove, sawKey)
	if !sawMove {
		t.Errorf("%s: a synthesised mouseMoved never reached the widget tree; "+
			"an overlay that appears on pointer motion can never appear", tc.name)
	}
	if !sawKey {
		t.Errorf("%s: a synthesised keyDown never reached the widget tree; "+
			"there is no way to close this window from the keyboard", tc.name)
	}
}

// NSEventType values used by the delivery test.
const (
	nsEventTypeMouseMoved = 5  // NSEventTypeMouseMoved
	nsEventTypeKeyDown    = 10 // NSEventTypeKeyDown
)
