// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package window

import (
	"fmt"
	"net"

	"github.com/go-widgets/window/internal/wayland"
)

// Enumerating displays on Wayland.
//
// A compositor announces one wl_output per screen through the registry, and
// each of them describes itself in a burst of events closed by done: where it
// is in the global compositor space, what the panel calls itself, which mode it
// is running and at what scale. That is everything [Screen] needs but one
// thing, and the one thing is not there to be had.
//
// The missing one is the USABLE AREA. Wayland deliberately tells an ordinary
// client nothing about panels, docks or bars: they are layer-shell surfaces the
// compositor arranges, and a client is not told what they took. So Visible* is
// the full bounds here, which is the honest answer rather than a guess — a
// compositor that wants a window somewhere else positions it there itself.

// waylandScreens enumerates the compositor's outputs.
//
// It opens its OWN connection and closes it again: enumerating displays is
// something an application does before it has a window, and borrowing a
// window's connection would make the answer depend on having one.
func waylandScreens(name string) ([]Screen, error) {
	path, err := waylandSocketPath(name)
	if err != nil {
		return nil, err
	}
	nc, err := net.Dial("unix", path)
	if err != nil {
		return nil, fmt.Errorf("window: cannot connect to Wayland compositor: %w", err)
	}
	uc, ok := nc.(*net.UnixConn)
	if !ok { // net.Dial("unix", ...) always yields *net.UnixConn
		_ = nc.Close()
		return nil, fmt.Errorf("window: Wayland dial returned %T, want *net.UnixConn", nc)
	}
	return screensOnWayland(wayland.New(uc))
}

// screensOnWayland is waylandScreens with the connection already open, which
// is what makes the whole exchange testable against a scripted compositor.
// It closes the connection: it is the only owner of it.
func screensOnWayland(conn *wayland.Conn) ([]Screen, error) {
	defer func() { _ = conn.Close() }()

	reg, err := conn.Display().GetRegistry()
	if err != nil {
		return nil, err
	}
	// One round trip for the globals the compositor advertises, a second for
	// the property burst each bound output then sends. Both are needed: an
	// output read before its done has no mode and no name.
	if err := conn.Roundtrip(); err != nil {
		return nil, err
	}
	outs, err := reg.Outputs()
	if err != nil {
		return nil, err
	}
	if err := conn.Roundtrip(); err != nil {
		return nil, err
	}
	if len(outs) == 0 {
		return nil, fmt.Errorf("window: the Wayland compositor advertises no output")
	}
	return primaryFirst(waylandScreensOf(outs)), nil
}

// waylandScreensOf is the projection onto [Screen], separated from the dialing
// so it can be tested against outputs a scripted compositor described.
//
// No output is marked primary, because Wayland has no such notion: a
// compositor arranges its outputs and does not rank them. [primaryFirst] then
// marks the first, which is the one the compositor advertised first — the same
// answer a bare X server gets, and for the same reason.
func waylandScreensOf(outs []*wayland.Output) []Screen {
	ids := make([]displayName, len(outs))
	for i, o := range outs {
		ids[i] = displayName{Model: o.Model(), Connector: o.Connector(), Vendor: o.Make()}
	}
	// resolveNames is shared with the X11 back-end, so a caller reading
	// Screen.Name does not have to know which session it is in to know what it
	// is holding.
	names := resolveNames(ids)

	screens := make([]Screen, 0, len(outs))
	for i, o := range outs {
		x, y := o.Position()
		w, h := o.LogicalSize()
		screens = append(screens, Screen{
			Name:   names[i],
			X:      x,
			Y:      y,
			Width:  w,
			Height: h,
			// Wayland tells an ordinary client nothing about what the
			// compositor's own surfaces reserved, so the whole panel is the
			// only area this back-end can honestly report as usable.
			VisibleX:      x,
			VisibleY:      y,
			VisibleWidth:  w,
			VisibleHeight: h,
			Scale:         float64(o.Scale()),
		})
	}
	return screens
}
