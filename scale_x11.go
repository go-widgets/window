// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"strconv"
	"strings"

	"github.com/go-widgets/window/internal/x11"
)

// HiDPI on X11, which the protocol has no opinion about.
//
// Wayland has a scale on every output and Windows has a DPI per monitor. X11
// has neither: the server reports a screen in pixels and in millimetres, and
// what a desktop actually does is publish `Xft.dpi` in the X RESOURCE MANAGER —
// the property every toolkit reads and every desktop environment writes. GTK
// reads it (GDK_SCALE falls back to it), Qt reads it, and a login session
// without it is a session that has decided it is 96 dpi.
//
// The millimetres are not a substitute. Servers report them wrongly often enough
// that computing a scale from them is a well-known way to get a giant or
// microscopic UI on somebody else's machine, which is why nothing serious does
// it. A desktop that says nothing is taken at 1:1.
//
// Scaling on X11 is then simply a bigger window: an application asking for
// 200×150 points on a 2× screen gets a 400×300-pixel window, which is the same
// physical size on that panel and twice the detail. There is no compositor to
// tell — unlike Wayland, nothing here upscales anything.

// dpiPerPoint is the dpi at which one pixel is one point. It is X11's own
// convention and the one Xft.dpi is quoted against.
const dpiPerPoint = 96.0

// maxAutoScale bounds what a resource file can ask for.
//
// The value comes from another process and is not validated by anything: a
// stray Xft.dpi of 9600 would ask for a hundredfold framebuffer, which is not a
// crisp window but an allocation failure. Four is past every shipping panel.
const maxAutoScale = 4

// resourceManager is the root-window property a desktop publishes its X
// resources in, one per line, "Name:\tvalue".
const resourceManagerAtom = "RESOURCE_MANAGER"

// desktopScale reads the desktop's Xft.dpi and turns it into whole framebuffer
// pixels per point.
//
// Whole, because a fractional framebuffer is not what this back-end draws: the
// widget tree is laid out in integer pixels and a 1.5 would put half a pixel
// between a border and the edge it is drawn against. A desktop at 144 dpi
// therefore rounds to 2, which is what GTK does with the same number.
func desktopScale(conn *x11.Conn, root uint32) int {
	atom, err := conn.InternAtom(resourceManagerAtom, true)
	if err != nil || atom == 0 {
		return 1 // no such property has ever existed on this server
	}
	_, _, data, err := conn.GetProperty(root, atom, 0 /* AnyPropertyType */, false, 16*1024)
	if err != nil {
		return 1
	}
	dpi, ok := xftDPI(string(data))
	if !ok {
		return 1
	}
	return scaleFromDPI(dpi)
}

// xftDPI finds the Xft.dpi resource in an X resource database.
//
// The format is one resource per line, name and value separated by a colon and
// whitespace. The name is matched exactly: Xft.dpi is not *.Xft.dpi and not
// Xft.dpiScale, and a prefix match would happily read the wrong one.
func xftDPI(db string) (float64, bool) {
	for _, line := range strings.Split(db, "\n") {
		name, value, found := strings.Cut(line, ":")
		if !found || strings.TrimSpace(name) != "Xft.dpi" {
			continue
		}
		dpi, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil || dpi <= 0 {
			return 0, false
		}
		return dpi, true
	}
	return 0, false
}

// scaleFromDPI rounds a dpi to whole pixels per point, within bounds.
func scaleFromDPI(dpi float64) int {
	s := int(dpi/dpiPerPoint + 0.5)
	switch {
	case s < 1:
		return 1
	case s > maxAutoScale:
		return maxAutoScale
	}
	return s
}

// RenderScale reports how many framebuffer pixels this window allocates per
// logical point. Implements the [Scaler] capability.
//
// It is 1 unless the caller asked for [NativeScale] and the desktop published
// an Xft.dpi that says otherwise.
func (w *Window) RenderScale() float64 {
	if w.scale < 1 {
		return 1
	}
	return float64(w.scale)
}
