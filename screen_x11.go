// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package window

import (
	"fmt"

	"github.com/go-widgets/window/internal/x11"
)

// Enumerating displays on X11.
//
// The rectangles come from RANDR 1.5, through the shared
// github.com/go-freedesktop/x11 — the same enumeration the screen capture in
// go-freedesktop/screencast has been running live, rather than a second copy
// of it here. What this file does is the projection onto [Screen]: X11 states
// everything in device pixels and knows nothing about a usable area, and
// [Screen] is in LOGICAL POINTS with the desktop's panels excluded, because
// that is what a caller reasoning about a window has to work in.

// x11Screens enumerates the displays of the X server named by $DISPLAY.
//
// The names are the displays' OWN names where the protocol offers them: the
// product string out of a panel's EDID ("DELL U2720Q", "VITURE Beast"), which
// is what a user recognises and what an application matching a particular
// headset has to match on. A display that publishes no EDID — an Xvfb, a
// driver that does not export the property — falls back to the RANDR connector
// name ("HDMI-1", "DP-2"), which at least says which socket it is in.
//
// Only the X screen named by $DISPLAY is enumerated. A server in Zaphod mode
// has several, but they are separate coordinate spaces that no window can move
// between, so listing them together would describe a desktop that does not
// exist.
func x11Screens(disp string) ([]Screen, error) {
	d, err := parseDisplay(disp)
	if err != nil {
		return nil, err
	}
	conn, err := dialAuthenticated(disp)
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	return screensOn(conn, d.screen)
}

// screensOn is Screens with the connection already open, which is what makes
// the whole projection testable against a scripted server.
func screensOn(conn *x11.Conn, screen int) ([]Screen, error) {
	sc := conn.Setup().ScreenOf(screen)
	if sc == nil {
		return nil, fmt.Errorf("window: DISPLAY names screen %d, and this server has %d",
			screen, len(conn.Setup().Screens))
	}
	mons, err := conn.Monitors(screen)
	if err != nil {
		return nil, err
	}
	// One scale for the whole desktop, because that is all X11 has: Xft.dpi is
	// a resource on the root window, not a property of a panel. A mixed-DPI X11
	// desktop is scaled by one number and always has been.
	scale := desktopScale(conn, sc.Root)
	wx, wy, ww, wh, hasWork := conn.WorkArea(sc.Root)

	ids := make([]displayName, len(mons))
	for i, m := range mons {
		ids[i] = displayName{Model: m.Model, Connector: m.Name}
	}
	names := resolveNames(ids)

	out := make([]Screen, 0, len(mons))
	for i, m := range mons {
		s := Screen{
			Name:    names[i],
			X:       points(int(m.X), scale),
			Y:       points(int(m.Y), scale),
			Width:   points(int(m.Width), scale),
			Height:  points(int(m.Height), scale),
			Scale:   float64(scale),
			Primary: m.Primary,
		}
		vx, vy, vw, vh := int(m.X), int(m.Y), int(m.Width), int(m.Height)
		if hasWork {
			// EWMH states one work area for the whole desktop and none per
			// monitor, so the usable part of a panel is what is left of it
			// after the desktop's reserved edges are taken out. An empty
			// intersection means the panel is not on the current desktop's
			// work area at all, and the whole of it stays usable.
			if ix, iy, iw, ih, ok := intersect(vx, vy, vw, vh, wx, wy, ww, wh); ok {
				vx, vy, vw, vh = ix, iy, iw, ih
			}
		}
		s.VisibleX = points(vx, scale)
		s.VisibleY = points(vy, scale)
		s.VisibleWidth = points(vw, scale)
		s.VisibleHeight = points(vh, scale)
		out = append(out, s)
	}
	return primaryFirst(out), nil
}

// points converts device pixels to logical points. The X11 back-end scales by
// making the WINDOW bigger — an application asking for 200 points on a 2x
// desktop gets a 400-pixel window — so a display's size in points is its size
// in pixels divided by the same factor.
func points(px, scale int) int {
	if scale < 1 {
		return px
	}
	return px / scale
}

// intersect returns the overlap of two rectangles, and whether there is one.
func intersect(ax, ay, aw, ah, bx, by, bw, bh int) (x, y, w, h int, ok bool) {
	x = max(ax, bx)
	y = max(ay, by)
	right := min(ax+aw, bx+bw)
	bottom := min(ay+ah, by+bh)
	if right <= x || bottom <= y {
		return 0, 0, 0, 0, false
	}
	return x, y, right - x, bottom - y, true
}

// primaryFirst moves the primary display to the front and guarantees that
// EXACTLY ONE screen carries the flag, which is what [Screen.Primary]
// promises.
//
// Neither half is automatic. A bare X server with no window manager marks no
// output primary at all — an Xvfb reports its single monitor as automatic and
// not primary — and a caller looking for "the main display" would find none.
// The first monitor RANDR states is the desktop's origin in that case, which is
// what the flag is for.
func primaryFirst(screens []Screen) []Screen {
	first := -1
	for i := range screens {
		if screens[i].Primary {
			if first < 0 {
				first = i
				continue
			}
			screens[i].Primary = false // only one may claim it
		}
	}
	if first < 0 {
		if len(screens) == 0 {
			return screens
		}
		first = 0
		screens[0].Primary = true
	}
	if first > 0 {
		p := screens[first]
		copy(screens[1:first+1], screens[:first])
		screens[0] = p
	}
	return screens
}
