// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin && integration

package cocoa

import (
	"testing"

	"github.com/go-macos/objc"
)

// TestLiveDockReopenShowsTheHiddenWindow hides a HideOnClose window through the
// real close callback, then runs the REAL delegate callback AppKit calls for a
// Dock click: the window must be visible again and the answer YES. The delegate
// installed on NSApp is the one that answers.
func TestLiveDockReopenShowsTheHiddenWindow(t *testing.T) {
	skipUnlessIntegration(t)

	selIsVisible := objc.RegisterName("isVisible")
	var win *Window
	callOnMain(func() {
		var err error
		win, err = NewWithOptions(Options{Title: "reopen", Width: 240, Height: 160, HideOnClose: true})
		if err != nil {
			t.Errorf("open: %v", err)
		}
	})
	if win == nil {
		return
	}
	var installed, hidden, answer, shown bool
	callOnMain(func() {
		spin(0.2)
		installed = objc.App().Send(selDelegate) != 0
		windowShouldClose(0, 0, 0)
		hidden = !objc.Send[bool](win.win, selIsVisible)
		answer = appShouldHandleReopen(0, 0, 0, false)
		spin(0.2)
		shown = objc.Send[bool](win.win, selIsVisible)
	})
	if !installed {
		t.Error("no NSApplication delegate after opening a HideOnClose window")
	}
	if !hidden {
		t.Error("the close request did not hide the window")
	}
	if !answer {
		t.Error("applicationShouldHandleReopen answered NO, want YES")
	}
	if !shown {
		t.Error("the Dock reopen did not show the hidden window")
	}
	callOnMain(func() { _ = win.Close(); spin(0.1) })
}

// A delegate the application installed itself is never replaced.
func TestLiveAppDelegateKeepsAForeignDelegate(t *testing.T) {
	skipUnlessIntegration(t)

	var before, after objc.ID
	callOnMain(func() {
		app := objc.App()
		old := app.Send(selDelegate)
		foreign := objc.ID(objc.GetClass("NSObject")).Send(selAlloc).Send(selInit)
		app.Send(selSetDelegate, foreign)
		before = foreign
		installAppDelegate()
		after = app.Send(selDelegate)
		app.Send(selSetDelegate, old)
	})
	if before == 0 || after != before {
		t.Errorf("installAppDelegate replaced a foreign delegate: %v -> %v", before, after)
	}
}
