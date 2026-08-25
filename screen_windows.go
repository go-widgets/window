// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package window

import (
	"fmt"

	xproto "github.com/go-freedesktop/x11"
	"github.com/go-mswin/win32"
	"golang.org/x/sys/windows/registry"
)

// Enumerating displays on Windows.
//
// The rectangles come from EnumDisplayMonitors and GetMonitorInfoW, through
// the shared github.com/go-mswin/win32 — the same binding go-mswin/screencapture
// enumerates capturable displays with, rather than a second copy of it here.
// Windows is unusually generous about what it will say: it states the work
// area PER MONITOR (X11 has one for the whole desktop and Wayland tells an
// ordinary client nothing), and it reports a DPI per monitor rather than one
// for the desktop.
//
// Two things it is NOT generous about, and both are handled rather than
// papered over.
//
// # A process that has not said what it wants is lied to
//
// Until it declares per-monitor DPI awareness, a process is handed VIRTUALISED
// coordinates: a 3840×2160 panel at 200% reports itself as 1920×1080, and
// every rectangle is scaled to match. The numbers are entirely plausible and
// there is nothing in them that says so. Screens therefore declares
// Per-Monitor-V2 before it reads anything — the same context the back-end
// declares when it opens a window, so asking what displays there are and then
// opening on one of them cannot see two different desktops.
//
// # The panel's name is not where one would look for it
//
// EnumDisplayDevices gives a monitor a description, and on most machines that
// description is the literal "Generic PnP Monitor" for every panel attached,
// because that is the inbox driver's name and not the panel's. The panel's own
// model is in its EDID, which Windows keeps in the registry under the device
// instance the enumeration hands back — reachable, but only if the enumeration
// was asked for the device INTERFACE path rather than the hardware id. See
// monitorModel.

// Screens enumerates the attached displays, primary first, in LOGICAL points.
// It is safe to call before Open — picking an output is something an
// application does on the way in.
//
// See [Screen] for what the fields mean, and winScreensOf for the one place
// Windows genuinely differs from the other back-ends: it has no single logical
// coordinate space, so on a mixed-DPI desktop these rectangles do not tile.
func Screens() ([]Screen, error) {
	// Per-Monitor-V2 first, and its result is deliberately ignored: it fails
	// when awareness has ALREADY been set, by an earlier call or by the
	// application manifest, which is not a problem — the process is aware, it
	// is simply not this call that made it so.
	win32.SetProcessDPIAwarenessContext(win32.DPIAwarenessPerMonitorV2)

	var handles []win32.HMONITOR
	if err := win32.EnumDisplayMonitors(func(m win32.HMONITOR, _ win32.HDC, _ win32.Rect) bool {
		handles = append(handles, m)
		return true
	}); err != nil {
		return nil, fmt.Errorf("window: cannot enumerate displays: %w", err)
	}

	// Describing the monitors happens OUTSIDE the enumeration callback. The
	// callback runs on a process-wide trampoline the OS calls back into, and
	// the naming below makes several more Win32 calls and reads the registry;
	// doing that while the OS is walking its own monitor list is a great deal
	// to do inside somebody else's loop for no gain.
	mons := make([]winMonitor, 0, len(handles))
	for _, h := range handles {
		mi, err := win32.GetMonitorInfo(h)
		if err != nil {
			// A monitor unplugged between the walk and this call is gone, not
			// a failure of the enumeration. Reporting the others is better
			// than reporting none.
			continue
		}
		dpi := winDefaultDPI
		if x, _, err := win32.GetDpiForMonitor(h, win32.MDTEffectiveDPI); err == nil {
			dpi = int(x)
		}
		b, w := mi.RcMonitor, mi.RcWork
		mons = append(mons, winMonitor{
			Device:  mi.Device(),
			Model:   monitorModel(mi.Device()),
			X:       int(b.Left),
			Y:       int(b.Top),
			W:       int(b.Width()),
			H:       int(b.Height()),
			WorkX:   int(w.Left),
			WorkY:   int(w.Top),
			WorkW:   int(w.Width()),
			WorkH:   int(w.Height()),
			DPI:     dpi,
			Primary: mi.Primary(),
		})
	}
	if len(mons) == 0 {
		return nil, fmt.Errorf("window: the desktop reports no display")
	}
	return winScreensOf(mons), nil
}

// VisibleScreenSize returns the usable area of the primary display in LOGICAL
// points — the full panel minus the taskbar and any app bars on it. ok is
// false when no display can be enumerated.
//
// See [Screens], which supersedes it for anything multi-display.
func VisibleScreenSize() (w, h int, ok bool) {
	screens, err := Screens()
	if err != nil || len(screens) == 0 {
		return 0, 0, false
	}
	s := screens[0]
	if s.VisibleWidth <= 0 || s.VisibleHeight <= 0 {
		return 0, 0, false
	}
	return s.VisibleWidth, s.VisibleHeight, true
}

// monitorModel returns the best name the panel behind a GDI device
// (`\\.\DISPLAY1`) gives for itself.
//
// Two sources, in that order:
//
//  1. Its EDID, which carries the product name the manufacturer put in it —
//     "DELL U2720Q", "VITURE Beast". This is the one an application looking
//     for a particular headset has to match on, and it is what the X11 and
//     Wayland back-ends return.
//  2. The description its DRIVER gives, when there is no EDID to read. On most
//     machines that is the inbox monitor driver's own name, the same
//     "Generic PnP Monitor" for every panel attached — which is a model at
//     best and never an identity. resolveNames is what makes that safe: a
//     name two displays share is dropped in favour of the connector.
//
// A mirroring pseudo-device — a remote-desktop or capture driver — has no
// panel and is skipped rather than named.
func monitorModel(device string) string {
	// EDD_GET_DEVICE_INTERFACE_NAME is what puts the device INTERFACE path in
	// DeviceID. Without it the field holds the driver's hardware id, which
	// names a class key and leads nowhere near an EDID.
	for _, mon := range win32.DisplayMonitors(device, win32.EDDGetDeviceInterfaceName) {
		if mon.Mirroring() {
			continue
		}
		if name := edidModel(mon.ID()); name != "" {
			return name
		}
		return mon.Description()
	}
	return ""
}

// edidModel reads a monitor's EDID out of the registry and returns the product
// name in it, or "" when there is no EDID (a virtual display, a basic display
// adapter, a driver that never wrote one) or no name descriptor in it.
//
// The parser is github.com/go-freedesktop/x11's, and the import reads oddly in
// a Windows file on purpose: EDID is a VESA structure that has nothing to do
// with X11, and the fleet already has exactly one parser for it — the one the
// X11 back-end feeds its RANDR property through, a few files away. A second
// copy here would drift, and the two back-ends would then disagree about what
// the SAME monitor is called depending on which OS it was plugged into.
func edidModel(deviceID string) string {
	path := edidRegistryPath(deviceID)
	if path == "" {
		return ""
	}
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, path, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer func() { _ = k.Close() }()
	blob, _, err := k.GetBinaryValue("EDID")
	if err != nil {
		return ""
	}
	return xproto.EDIDModelName(blob)
}
