// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin && integration

package cocoa

import (
	"testing"

	"github.com/go-macos/objc"
)

// TestLiveHideOnCloseOrdersOutAndKeepsTheWindow runs the REAL delegate callback
// AppKit calls for the close button against a real NSWindow: with HideOnClose it
// must answer NO (the window is kept), order the window out, and leave it
// showable; without it, it must answer YES and mark the window closed.
func TestLiveHideOnCloseOrdersOutAndKeepsTheWindow(t *testing.T) {
	skipUnlessIntegration(t)

	selIsVisible := objc.RegisterName("isVisible")
	for _, tc := range []struct {
		name       string
		hide       bool
		wantAnswer bool
	}{
		{"HideOnClose", true, false},
		{"default", false, true},
	} {
		var win *Window
		callOnMain(func() {
			var err error
			win, err = NewWithOptions(Options{Title: tc.name, Width: 240, Height: 160, HideOnClose: tc.hide})
			if err != nil {
				t.Errorf("%s: open: %v", tc.name, err)
			}
		})
		if win == nil {
			return
		}
		var answer, visible, closed bool
		callOnMain(func() {
			spin(0.2)
			answer = windowShouldClose(0, 0, 0)
			visible = objc.Send[bool](win.win, selIsVisible)
			closed = win.closed
		})
		if answer != tc.wantAnswer {
			t.Errorf("%s: windowShouldClose = %v, want %v", tc.name, answer, tc.wantAnswer)
		}
		if tc.hide && (visible || closed) {
			t.Errorf("%s: after the close request visible=%v closed=%v, want hidden and not closed", tc.name, visible, closed)
		}
		if !tc.hide && !closed {
			t.Errorf("%s: the window was not marked closed", tc.name)
		}
		callOnMain(func() { _ = win.Close(); spin(0.1) })
	}
}
