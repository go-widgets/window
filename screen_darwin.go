// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package window

import "github.com/go-widgets/window/internal/cocoa"

// VisibleScreenSize returns the usable area of the primary screen in LOGICAL
// points — the same unit Config.Width/Height are expressed in — with the menu
// bar and Dock excluded on macOS. ok is false when the size cannot be
// determined (a headless build, or a display server with no screen).
//
// It is safe to call before Open, so a desktop shell that wants to launch at,
// say, the full screen height can query the size here and pass it back through
// Config.Height: NewScaled honours an explicit (> 0) size verbatim, applying no
// readability clamp. Today only the macOS (Cocoa) backend reports a size; the
// X11, Wayland, Windows and js/wasm backends return ok=false (see
// screen_other.go) until they grow an equivalent query.
//
// [Screens] supersedes this for anything multi-display: it reports every
// attached panel, and its result is what Config.Screen accepts.
func VisibleScreenSize() (w, h int, ok bool) {
	return cocoa.VisibleScreenSize()
}

// Screens enumerates the attached displays, primary first. It is safe to call
// before Open — picking an output is something an application does on the way
// in.
//
// The geometry is read from the WINDOW SERVER each time, not from AppKit's
// cached NSScreen list, so it describes the desktop as it is now even in a
// process that has not started an NSApplication. That distinction is not
// academic: the cache is filled on first read and refreshed only for a running
// application, so an application that lists the displays on its way in would
// otherwise hold an arrangement frozen at that first read for the rest of its
// life -- and place its window accordingly.
//
// One thing does still come from AppKit, because it is AppKit's to know: the
// display's NAME. A display attached while this process had no running
// application, and enumerated from a goroutine that is not on the main thread,
// can therefore come back nameless. Everything placement depends on is exact
// regardless.
func Screens() ([]Screen, error) {
	infos, err := cocoa.Screens()
	if err != nil {
		return nil, err
	}
	out := make([]Screen, len(infos))
	for i, s := range infos {
		out[i] = Screen{
			Name:          s.Name,
			X:             s.X,
			Y:             s.Y,
			Width:         s.Width,
			Height:        s.Height,
			VisibleX:      s.VisibleX,
			VisibleY:      s.VisibleY,
			VisibleWidth:  s.VisibleWidth,
			VisibleHeight: s.VisibleHeight,
			Scale:         s.Scale,
			Primary:       s.Primary,
		}
	}
	return out, nil
}

// toCocoa is the reverse projection, used by Open to hand a chosen screen back
// to the back-end for re-resolution against the live display list.
func (s Screen) toCocoa() cocoa.ScreenInfo {
	return cocoa.ScreenInfo{
		Name:   s.Name,
		X:      s.X,
		Y:      s.Y,
		Width:  s.Width,
		Height: s.Height,
	}
}
