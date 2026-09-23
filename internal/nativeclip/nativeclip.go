// Copyright (c) the go-widgets authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// Package nativeclip works out the geometry of showing only the part of a
// native control that its viewport still shows.
//
// Every native-control descriptor carries a Clip: the rectangle of the control
// that is still inside whatever scrolls it. Three backends embed real OS
// controls -- Cocoa, GTK and Win32 -- and each honours that rectangle with a
// different primitive:
//
//   - Cocoa and GTK NEST: a clipping view (an NSView, a GtkFixed with overflow
//     hidden) takes the visible rectangle, and the control sits inside it,
//     offset so the right part of it shows. Both want [Frames].
//   - Win32 MASKS: the control's own window is confined to a region, given in
//     the control's own coordinates. That is [Region].
//
// The decision of WHETHER to clip is the same for all three, and it lives here
// once. Restated at three call sites it would drift: the case that decides
// whether a control is wrapped at all is easy to spell three subtly different
// ways, and two of them would be wrong on a platform nobody is looking at.
package nativeclip

import "github.com/go-widgets/toolkit"

// Clipped says whether a control shows only PART of itself: its clip is a real
// rectangle, and smaller than where it wants to be.
//
// Equal rectangles are not clipped -- that is the ordinary case and honouring
// it would cost a view, or a region, per control for nothing. An empty clip is
// not clipped either: the descriptor already reports that as not Visible, and
// every backend hides it, which is cheaper and was the only part of this a
// backend could honour before.
func Clipped(rect, clip toolkit.Rect) bool {
	if clip.W <= 0 || clip.H <= 0 {
		return false
	}
	return clip != rect
}

// Frames is, for a backend that nests, where the clipping view goes and where
// the control goes inside it: the view takes the visible rectangle, and the
// control keeps its full size at the offset that puts the right part of it on
// show.
//
// The control is NOT resized to the clip. A button squashed to the sliver of it
// that shows would redraw its label to fit and look like a different button;
// moved behind a smaller window, it is the same button with part of it out of
// sight, which is what scrolling means.
func Frames(rect, clip toolkit.Rect) (outer, inner toolkit.Rect) {
	return clip, toolkit.Rect{
		X: rect.X - clip.X, Y: rect.Y - clip.Y, W: rect.W, H: rect.H,
	}
}

// Region is, for a backend that masks, the part of the control the system may
// draw -- expressed in the CONTROL's own coordinates, because that is the space
// a window region is read in, not the window's parent's.
//
// It is the same geometry [Frames] describes, seen from the other side: where
// Frames offsets the control inside the visible box, Region offsets the visible
// box inside the control. The two therefore disagree in sign, and a test here
// holds them to it.
func Region(rect, clip toolkit.Rect) toolkit.Rect {
	return toolkit.Rect{
		X: clip.X - rect.X, Y: clip.Y - rect.Y, W: clip.W, H: clip.H,
	}
}
