// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package cocoa

// RaiseSteps is what bringing a window to the front takes on macOS, decided
// over plain values so it is tested on every GOOS; the AppKit calls that carry
// it out are in visibility_darwin.go.
//
//   - Deminiaturize: a window in the Dock is brought out of it first, or
//     ordering it front leaves it where it is.
//   - Activate: the application has to be the active one for its window to
//     take the keyboard. A tray app's menu runs while ANOTHER application is
//     active, which is exactly when Raise is asked for.
//   - MakeKey: order the window front AND give it the keyboard; without it,
//     ordering front regardless of activation is all that is done.
//
// A passive window (Options.Passive) never takes the keyboard and never
// activates the application -- that is the whole of what passive means -- so
// it is only ordered front.
type RaiseSteps struct {
	Deminiaturize, Activate, MakeKey bool
}

// PlanRaise decides the steps for a window that is, or is not, miniaturised
// and passive.
func PlanRaise(miniaturized, passive bool) RaiseSteps {
	return RaiseSteps{
		Deminiaturize: miniaturized,
		Activate:      !passive,
		MakeKey:       !passive,
	}
}
