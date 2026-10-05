// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package cocoa

import (
	"errors"

	objc "github.com/go-macos/objc"
)

// Show, Hide and Raise: the window.Visibility capability on AppKit.
//
// Each is performed on the main thread, like Repaint and Close, because AppKit
// may be touched nowhere else and the caller is typically a tray menu running
// on a goroutine of its own. waitUntilDone is NO: the caller asked for the
// window to move, not to be blocked until the window server has moved it.

// ErrClosed is returned for a request made of a closed window. The parent
// package maps it to its own window.ErrClosed.
var ErrClosed = errors.New("cocoa: the window is closed")

var (
	selShowNow  = objc.RegisterName("goWidgetsShowNow")
	selHideNow  = objc.RegisterName("goWidgetsHideNow")
	selRaiseNow = objc.RegisterName("goWidgetsRaiseNow")

	selOrderFront           = objc.RegisterName("orderFront:")
	selOrderFrontRegardless = objc.RegisterName("orderFrontRegardless")
	selOrderOut             = objc.RegisterName("orderOut:")
	selIsMiniaturized       = objc.RegisterName("isMiniaturized")
	selDeminiaturize        = objc.RegisterName("deminiaturize:")
)

// visibilityMethods are the view-class methods the three requests are
// performed through. They are registered with the view's other methods in
// registerClasses.
func visibilityMethods() []objc.MethodDef {
	return []objc.MethodDef{
		{Cmd: selShowNow, Fn: viewShowNow},
		{Cmd: selHideNow, Fn: viewHideNow},
		{Cmd: selRaiseNow, Fn: viewRaiseNow},
	}
}

// perform sends sel to the view on the main thread, unless the window has gone.
func (w *Window) perform(sel objc.SEL) error {
	if w == nil || w.view == 0 || w.gone.Load() {
		return ErrClosed
	}
	w.view.Send(selPerformOnMain, sel, objc.ID(0), false)
	return nil
}

// Show orders the window back on screen without activating the application.
func (w *Window) Show() error { return w.perform(selShowNow) }

// Hide orders the window out: off the screen and out of the Window menu, with
// Run still running.
func (w *Window) Hide() error { return w.perform(selHideNow) }

// Raise brings the window out of the Dock if it is there, activates the
// application and makes the window key and front. See PlanRaise.
func (w *Window) Raise() error { return w.perform(selRaiseNow) }

// The main-thread halves. They act on the ACTIVE window for the same reason
// every other view callback does: a method on a registered class has no other
// way back to Go state, and this process runs one window at a time.

func viewShowNow(_ objc.ID, _ objc.SEL) {
	w := active
	if w == nil || w.closed {
		return
	}
	if w.passive {
		w.win.Send(selOrderFrontRegardless)
		return
	}
	w.win.Send(selOrderFront, objc.ID(0))
}

func viewHideNow(_ objc.ID, _ objc.SEL) {
	w := active
	if w == nil || w.closed {
		return
	}
	w.win.Send(selOrderOut, objc.ID(0))
}

func viewRaiseNow(_ objc.ID, _ objc.SEL) {
	w := active
	if w == nil || w.closed {
		return
	}
	steps := PlanRaise(objc.Send[bool](w.win, selIsMiniaturized), w.passive)
	if steps.Deminiaturize {
		w.win.Send(selDeminiaturize, objc.ID(0))
	}
	if steps.Activate {
		objc.App().Send(selActivateIgnoring, true)
	}
	if steps.MakeKey {
		w.win.Send(selMakeKeyAndOrderFront, objc.ID(0))
		return
	}
	w.win.Send(selOrderFrontRegardless)
}
