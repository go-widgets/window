// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !js

package window

import (
	"errors"
	"image/color"
	"time"

	"github.com/godbus/dbus/v5"
)

// The Linux half of the AppearanceReader capability, shared by both back-ends.
//
// There is nothing to ask X11 or Wayland itself. Dark mode and an accent colour
// are desktop-environment settings, not display-server ones, and neither
// protocol has an opinion about either. The answer lives behind the XDG desktop
// portal, on the session bus, which is also what a sandboxed application can
// reach — so this works the same inside a Flatpak as outside one, and the same
// under sway as under X11. That is why the reading is a method on the portal
// state rather than on a window: the two back-ends share it exactly, and
// duplicating it would be duplicating a D-Bus client for no reason.
//
// A desktop with no portal (a bare window manager, a minimal container) is not
// a broken desktop. It reports no preference, and an application that asked
// should keep its own theme rather than assume light.

const (
	portalService   = "org.freedesktop.portal.Desktop"
	portalPath      = "/org/freedesktop/portal/desktop"
	portalSettings  = "org.freedesktop.portal.Settings"
	appearanceNS    = "org.freedesktop.appearance"
	keyColorScheme  = "color-scheme"
	keyAccentColour = "accent-color"
)

// appearanceCacheFor is how long a reading is reused.
//
// The macOS side is a handful of in-process message sends and is documented as
// cheap enough to poll every frame. This is a D-Bus round trip to another
// process: doing that sixty times a second to learn something a user changes
// once an hour would be absurd. Half a second is far below what anyone notices
// when they flip their desktop to dark, and three orders of magnitude above
// what it costs.
const appearanceCacheFor = 500 * time.Millisecond

// appearance reads the desktop's colour scheme and accent colour, cached.
func (p *portalConn) appearance() Appearance {
	if !p.at.IsZero() && time.Since(p.at) < appearanceCacheFor {
		return p.cached
	}
	ap := p.read()
	p.cached, p.at = ap, time.Now()
	return ap
}

func (p *portalConn) read() Appearance {
	obj, ok := p.object()
	if !ok {
		return Appearance{}
	}
	return appearanceFrom(obj)
}

// appearanceFrom is the reading itself, separated from the connecting so it can
// be exercised against a portal that is not a desktop.
func appearanceFrom(obj dbus.BusObject) Appearance {
	var ap Appearance
	// color-scheme: 0 no preference, 1 prefer dark, 2 prefer light. Anything
	// else is a newer spec than this code, and "no preference" is the honest
	// reading of a value we do not understand.
	if v, ok := portalRead(obj, appearanceNS, keyColorScheme); ok {
		if n, ok := v.Value().(uint32); ok {
			ap.Dark = n == 1
		}
	}
	// accent-color: three doubles in 0..1, or nothing when the user has not
	// chosen one. Absence is reported as absence, not as black.
	if v, ok := portalRead(obj, appearanceNS, keyAccentColour); ok {
		if rgb, ok := v.Value().([]interface{}); ok && len(rgb) == 3 {
			r, rok := rgb[0].(float64)
			g, gok := rgb[1].(float64)
			b, bok := rgb[2].(float64)
			if rok && gok && bok {
				ap.Accent = color.RGBA{R: unitByte(r), G: unitByte(g), B: unitByte(b), A: 255}
				ap.HasAccent = true
			}
		}
	}
	return ap
}

// object returns the portal, connecting once. A failure is remembered: a desktop
// without a portal will not grow one while the window is open, and retrying
// every poll would spend a connection attempt per frame proving it.
func (p *portalConn) object() (dbus.BusObject, bool) {
	bus, _ := p.bus.(*dbus.Conn)
	if bus == nil {
		if p.tried {
			return nil, false
		}
		p.tried = true
		var err error
		if bus, err = dbus.SessionBus(); err != nil {
			return nil, false
		}
		p.bus = bus
	}
	return bus.Object(portalService, dbus.ObjectPath(portalPath)), true
}

// portalRead calls Settings.Read, whose result is a variant wrapping a variant
// -- the outer one is the method's signature, the inner one the setting's own
// type, and unwrapping only once yields a dbus.Variant where a value was
// expected.
func portalRead(obj dbus.BusObject, ns, key string) (dbus.Variant, bool) {
	var outer dbus.Variant
	if err := obj.Call(portalSettings+".Read", 0, ns, key).Store(&outer); err != nil {
		return dbus.Variant{}, false
	}
	if inner, ok := outer.Value().(dbus.Variant); ok {
		return inner, true
	}
	return outer, true
}

// unitByte maps a 0..1 colour component to 0..255, clamped and rounded.
//
// The clamp is not padding: the portal's doubles come from another process and
// nothing on the bus enforces the range, so an out-of-range value would wrap
// into a wildly wrong byte -- 1.004 turning into 1, a near-white accent
// rendering as near-black.
func unitByte(v float64) uint8 {
	switch {
	case v <= 0:
		return 0
	case v >= 1:
		return 255
	default:
		return uint8(v*255 + 0.5)
	}
}

// errNoSystemFont is what SystemFontTTF returns here, and the wording is the
// point: this is not a missing feature, it is a difference in what the
// platforms offer.
//
// macOS has one system face at a known path, so it can be read and used. A
// Linux desktop instead names a family ("Cantarell 11") and leaves finding it
// to fontconfig, which is a font library's job and not a window's. Returning an
// error says so; returning some arbitrary file found on disk would be worse
// than saying nothing.
var errNoSystemFont = errors.New("window: Linux desktops name a font rather than providing one; resolve it with a font library")
