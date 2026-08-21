// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package cocoa

import (
	"errors"
	"fmt"

	"github.com/go-macos/objc"
	"github.com/go-widgets/toolkit"
)

var (
	selScreens       = objc.RegisterName("screens")
	selLocalizedName = objc.RegisterName("localizedName")
	selArrayCount    = objc.RegisterName("count")
	selObjectAtIndex = objc.RegisterName("objectAtIndex:")
)

// ScreenInfo describes one attached display. Sizes and positions are in LOGICAL
// points, the unit the toolkit lays out in.
//
// X and Y are TOP-LEFT origin, Y growing downwards, which is the convention
// every other back-end in this repo uses and the one a caller expects. AppKit's
// own space is bottom-left with Y growing up, so the two differ; nativeFrame
// keeps AppKit's rectangle verbatim so that placing a window on this screen is
// an exact copy of what AppKit reported, never the result of re-deriving a
// flipped coordinate. Getting a window onto the right display must not depend
// on arithmetic that could be off by a menu bar.
type ScreenInfo struct {
	Name          string
	X, Y          int
	Width, Height int
	// Visible* is the usable area, with the menu bar and Dock excluded.
	VisibleX, VisibleY          int
	VisibleWidth, VisibleHeight int
	// Scale is the display's backing factor: device pixels per logical point.
	Scale float64
	// Primary reports the screen that owns the global origin — the one AppKit
	// puts the menu bar on. It is NOT [NSScreen mainScreen], which follows the
	// key window and therefore changes as the user clicks around.
	Primary bool

	nativeFrame nsRect
}

// flipY converts a bottom-left-origin Y (AppKit) to a top-left-origin Y, given
// the height of the primary screen, which is where the global origin sits. It
// is the whole of the coordinate-space difference, kept separate so it can be
// tested without a display attached.
func flipY(y, height, primaryHeight float64) float64 {
	return primaryHeight - (y + height)
}

// Screens enumerates the attached displays. The slice is ordered as AppKit
// reports it, so index 0 is the screen holding the global origin.
//
// It loads the frameworks on first use and is safe to call before any window
// exists — which is the point: a caller picks its display, then passes the
// chosen ScreenInfo back in to place the window there.
func Screens() ([]ScreenInfo, error) {
	if err := loadFrameworks(); err != nil {
		return nil, err
	}
	arr := objc.ID(objc.GetClass("NSScreen")).Send(selScreens)
	if arr == 0 {
		return nil, nil
	}
	n := int(objc.Send[uint64](arr, selArrayCount))
	if n <= 0 {
		return nil, nil
	}

	// Every Y flip is relative to the primary screen's height, so read it once
	// before converting anything.
	first := objc.Send[objc.ID](arr, selObjectAtIndex, uint64(0))
	primaryHeight := objc.Send[nsRect](first, selFrame).Size.H

	out := make([]ScreenInfo, 0, n)
	for i := 0; i < n; i++ {
		s := objc.Send[objc.ID](arr, selObjectAtIndex, uint64(i))
		if s == 0 {
			continue
		}
		f := objc.Send[nsRect](s, selFrame)
		vf := objc.Send[nsRect](s, selVisibleFrame)
		scale := float64(objc.Send[float64](s, selBackingScaleFactor))
		if scale <= 0 {
			scale = 1
		}
		out = append(out, ScreenInfo{
			Name:          objc.GoString(objc.Send[objc.ID](s, selLocalizedName)),
			X:             int(f.Origin.X),
			Y:             int(flipY(f.Origin.Y, f.Size.H, primaryHeight)),
			Width:         int(f.Size.W),
			Height:        int(f.Size.H),
			VisibleX:      int(vf.Origin.X),
			VisibleY:      int(flipY(vf.Origin.Y, vf.Size.H, primaryHeight)),
			VisibleWidth:  int(vf.Size.W),
			VisibleHeight: int(vf.Size.H),
			Scale:         scale,
			Primary:       i == 0,
			nativeFrame:   f,
		})
	}
	return out, nil
}

// FindScreen returns the enumerated screen matching want by name and geometry,
// re-reading the display list so the answer reflects the displays attached NOW.
//
// The re-read is deliberate. A ScreenInfo is a value the caller may have been
// holding for a while, and an external display — an XR headset especially — can
// be unplugged, or change resolution, between being listed and being used.
// Matching against the live list turns that into an honest error instead of a
// window placed at coordinates that no longer describe anything.
func FindScreen(want ScreenInfo) (ScreenInfo, bool) {
	screens, err := Screens()
	if err != nil {
		return ScreenInfo{}, false
	}
	for _, s := range screens {
		if s.Name == want.Name && s.X == want.X && s.Y == want.Y &&
			s.Width == want.Width && s.Height == want.Height {
			return s, true
		}
	}
	return ScreenInfo{}, false
}

// ErrScreenGone reports that the screen a caller asked for is no longer among
// the attached displays. It is a normal outcome, not a defect: an external
// display can be unplugged between being listed and being used.
var ErrScreenGone = errors.New("cocoa: the requested screen is no longer attached")

// Options parametrises window creation. The zero value asks for what New would
// give: a titled, centred window on whatever display macOS picks.
type Options struct {
	Title         string
	Width, Height int
	Theme         *toolkit.Theme
	// RenderScale is framebuffer pixels per logical point: 0 for the readable
	// default of 1, negative to follow the display's backing factor, positive to
	// use as-is.
	RenderScale float64
	// Screen places the window on a particular display, as returned by
	// [Screens]. Nil lets macOS choose.
	Screen *ScreenInfo
	// Fullscreen sizes the window to cover its screen entirely, with no title
	// bar and no frame. With Screen nil it covers the primary screen.
	//
	// This is NOT macOS native full screen: no Space, no animation, no menu bar
	// waiting at the top edge. A borderless window at the panel's exact bounds
	// is what an immersive surface needs, and it is also what lets a caller put
	// one on an external display while the desktop carries on elsewhere.
	Fullscreen bool
}

// resolveScreen turns the requested placement into a live screen, or nil when
// the window should be placed by macOS as before.
func (o Options) resolveScreen() (*ScreenInfo, error) {
	if o.Screen != nil {
		s, ok := FindScreen(*o.Screen)
		if !ok {
			return nil, fmt.Errorf("%w: %q %dx%d at %d,%d", ErrScreenGone,
				o.Screen.Name, o.Screen.Width, o.Screen.Height, o.Screen.X, o.Screen.Y)
		}
		return &s, nil
	}
	if !o.Fullscreen {
		return nil, nil
	}
	// Fullscreen without a named screen means the primary one, which still needs
	// resolving because the frame is what the window is sized to.
	screens, err := Screens()
	if err != nil {
		return nil, err
	}
	for i := range screens {
		if screens[i].Primary {
			return &screens[i], nil
		}
	}
	return nil, ErrScreenGone
}
