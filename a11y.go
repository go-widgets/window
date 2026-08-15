// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/window/internal/atspi"
)

// The accessibility wiring shared by the X11 and Wayland back-ends.
//
// AT-SPI is a D-Bus protocol that knows nothing about the display server (see
// internal/atspi), so both Linux back-ends drive it identically: from the place
// that presents pixels they replay any activation a screen reader asked for and
// republish the widget tree. Keeping that in one function is what makes the two
// back-ends behave the same and lets the wiring be tested without a live bus.

// a11yTakePending and a11yPublish are the bridge entry points, held in
// variables so a test can stand in for the live D-Bus package (which needs a
// running accessibility bus and so is exercised only on the VM). They default
// to the real implementation — atspi.TakePending returns the same anonymous
// struct type these are typed against.
var (
	a11yTakePending = atspi.TakePending
	a11yPublish     = atspi.Publish
)

// refreshA11y brings the accessibility view of the window up to date for the
// frame being drawn. It runs on the run-loop goroutine — the one thread that
// owns the widget tree — because both halves touch that tree:
//
//   - Any activation a client requested since the last frame is applied HERE,
//     by replaying it as an ordinary click at the element's centre. The bridge
//     only RECORDS the request (it arrives on a D-Bus goroutine, which must not
//     touch the tree); replaying it as a real click means every behaviour a
//     click has is had by an accessibility action, with no second code path to
//     drift from the first.
//   - The tree is then republished, from the same place that presents pixels,
//     so a screen reader's description never lags what is on screen.
//
// originX/originY are where the window sits on screen, needed because AT-SPI
// reports element extents in screen space too. The X11 back-end knows its
// position and passes it; the Wayland back-end cannot know it (the protocol
// never tells a client where it is) and passes (0,0), which atspi.ScreenRect is
// written to accept.
func refreshA11y(root toolkit.Widget, title string, originX, originY int) {
	for _, p := range a11yTakePending() {
		if root != nil {
			root.OnEvent(toolkit.Event{Kind: toolkit.EventClick, X: p.X, Y: p.Y})
		}
	}
	a11yPublish(root, title, originX, originY)
}
