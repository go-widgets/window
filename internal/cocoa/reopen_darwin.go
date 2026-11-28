// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package cocoa

import (
	"sync"

	objc "github.com/go-macos/objc"
)

// Dock reopen. A click on the application's Dock icon sends the NSApplication
// delegate -applicationShouldHandleReopen:hasVisibleWindows: (Apple,
// NSApplicationDelegate). AppKit's default un-hides nothing that was ordered
// out, so a window hidden by Options.HideOnClose stayed gone until the app
// offered another way back. The delegate below shows it again, raises it, and
// answers YES so AppKit still does its own default work.

var (
	appDelegateOnce sync.Once
	appDelegateID   objc.ID
	selDelegate     = objc.RegisterName("delegate")
)

// installAppDelegate makes the reopen handler the NSApplication delegate,
// unless the process already has a delegate of its own, which is left alone.
func installAppDelegate() {
	app := objc.App()
	if app == 0 || app.Send(selDelegate) != 0 {
		return
	}
	appDelegateOnce.Do(func() {
		cls, err := objc.RegisterClass("GoWidgetsAppDelegate", objc.GetClass("NSObject"),
			[]objc.MethodDef{{
				Cmd: objc.RegisterName("applicationShouldHandleReopen:hasVisibleWindows:"),
				Fn:  appShouldHandleReopen,
			}})
		if err != nil {
			return
		}
		appDelegateID = objc.ID(cls).Send(selAlloc).Send(selInit)
		appDelegateID.Send(selRetain)
	})
	if appDelegateID != 0 && app.Send(selDelegate) == 0 {
		app.Send(selSetDelegate, appDelegateID)
	}
}

// appShouldHandleReopen runs on the main thread. It always answers YES.
func appShouldHandleReopen(_ objc.ID, _ objc.SEL, _ objc.ID, hasVisible bool) bool {
	w := active
	if w != nil && PlanReopen(hasVisible, w.hideOnClose, w.closed, w.child) {
		w.raiseNow()
	}
	return true
}
