// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin && integration

package cocoa

import (
	"errors"
	"testing"
	"time"

	"github.com/go-macos/objc"
)

// TestLiveShowHideRaiseFromAnotherGoroutine (go-widgets/window#134) asks a REAL
// NSWindow to hide, show and come back out of the Dock, from a goroutine that
// is not the main thread -- which is where a tray's menu runs -- and reads the
// answer from AppKit itself (-isVisible, -isMiniaturized), not from pixels:
// nothing here takes a picture of the screen.
func TestLiveShowHideRaiseFromAnotherGoroutine(t *testing.T) {
	skipUnlessIntegration(t)

	var win *Window
	callOnMain(func() {
		var err error
		if win, err = NewScaled("visibility", 240, 160, nil, 0); err != nil {
			t.Errorf("open: %v", err)
		}
	})
	if win == nil {
		return
	}
	closed := false
	defer func() {
		if !closed {
			callOnMain(func() { _ = win.Close() })
		}
	}()

	selIsVisible := objc.RegisterName("isVisible")
	selMiniaturize := objc.RegisterName("miniaturize:")
	state := func() (visible, mini bool) {
		callOnMain(func() {
			visible = objc.Send[bool](win.win, selIsVisible)
			mini = objc.Send[bool](win.win, selIsMiniaturized)
		})
		return
	}
	// ask runs the request off the main thread, then turns the main run loop
	// until AppKit reports what was wanted.
	ask := func(name string, req func() error, ok func(visible, mini bool) bool) {
		t.Helper()
		errc := make(chan error, 1)
		go func() { errc <- req() }()
		if err := <-errc; err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			callOnMain(func() { spin(0.05) })
			if ok(state()) {
				return
			}
		}
		v, m := state()
		t.Fatalf("%s: AppKit reports visible=%v miniaturized=%v 3s later", name, v, m)
	}

	callOnMain(func() { spin(0.2) })
	if v, _ := state(); !v {
		t.Fatal("the window was not visible to begin with")
	}
	ask("Hide", win.Hide, func(v, _ bool) bool { return !v })
	ask("Show", win.Show, func(v, _ bool) bool { return v })

	callOnMain(func() { win.win.Send(selMiniaturize, objc.ID(0)); spin(0.5) })
	ask("Raise of a miniaturised window", win.Raise, func(v, m bool) bool { return v && !m })

	ask("Hide again", win.Hide, func(v, _ bool) bool { return !v })
	ask("Raise of a hidden window", win.Raise, func(v, _ bool) bool { return v })

	callOnMain(func() { _ = win.Close(); spin(0.1) })
	closed = true
	for name, req := range map[string]func() error{"Show": win.Show, "Hide": win.Hide, "Raise": win.Raise} {
		if err := req(); !errors.Is(err, ErrClosed) {
			t.Errorf("%s after Close = %v, want ErrClosed", name, err)
		}
	}
}
