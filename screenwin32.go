// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"math"
	"strings"
)

// The Windows half of display enumeration that is NOT a system call: turning
// what the OS said into [Screen], and turning a monitor's device id into the
// registry path where its EDID lives.
//
// It is in an UNTAGGED file so it is exercised on every platform this repo
// tests on, the same way waylandScreensOf is. The projection is where the
// interesting mistakes are — a scale applied twice, a work area that is not
// clipped, a name that cannot tell two panels apart — and none of them need a
// Windows machine to catch. screen_windows.go is the part that does.

// winMonitor is one monitor as the Win32 API described it, in the units the
// API states: PHYSICAL pixels on the virtual screen, and a DPI rather than a
// factor.
type winMonitor struct {
	// Device is the GDI device name, `\\.\DISPLAY1`. It is the connector:
	// unique by construction and stable while the monitor stays plugged in.
	Device string
	// Model is the panel's own name where one could be had — out of its EDID,
	// or failing that the description its driver gives. It is "" when the
	// panel says nothing about itself.
	Model string
	// X, Y, W, H are the monitor's full rectangle, and Work* the part left
	// after the taskbar and any app bars ON THIS MONITOR have taken theirs.
	// Windows states the work area per monitor, where X11's _NET_WORKAREA is
	// one rectangle for the whole desktop.
	X, Y, W, H   int
	WorkX, WorkY int
	WorkW, WorkH int
	// DPI is the monitor's effective DPI: 96 at 100%, 144 at 150%.
	DPI int
	// Primary reports the monitor that owns the desktop's origin and carries
	// the taskbar.
	Primary bool
}

// winScreensOf projects what the OS said onto [Screen], primary first.
//
// # The one place Windows differs, and it cannot be papered over
//
// [Screen] is in LOGICAL POINTS, and every other back-end has a single logical
// space to state them in: X11 scales the whole desktop by one number, Wayland
// hands out positions in a compositor space that is already logical, macOS has
// one point space with the backing factor kept separate. Windows has none.
// Per-Monitor-V2 exists precisely because a 4K panel at 200% and a 1080p panel
// at 100% sit side by side in ONE pixel space and want different scales.
//
// So each monitor's rectangle is divided by ITS OWN scale, and on a mixed-DPI
// desktop the results do not tile: two monitors that are edge to edge in
// pixels can overlap or leave a gap in points. That is not an error to correct
// — correcting it would mean inventing a coordinate space Windows does not
// have, and reporting positions that no Win32 call would accept back.
//
// Nothing depends on them tiling. [Config.Screen] takes the Screen VALUE back
// and the back-end re-resolves it against the displays attached at that
// moment; placement never goes through these numbers. What they are for is
// telling a user how big each display is and where it roughly sits, and for
// that the panel's own points are the right answer.
func winScreensOf(mons []winMonitor) []Screen {
	ids := make([]displayName, len(mons))
	for i, m := range mons {
		ids[i] = displayName{Model: m.Model, Connector: m.Device}
	}
	// resolveNames is shared with the X11 and Wayland back-ends: two identical
	// monitors report the identical model, and on Windows they do it more
	// often than anywhere else, because the generic monitor driver gives every
	// panel it handles the same description.
	names := resolveNames(ids)

	out := make([]Screen, 0, len(mons))
	for i, m := range mons {
		s := Screen{
			Name:    names[i],
			X:       winPoints(m.X, m.DPI),
			Y:       winPoints(m.Y, m.DPI),
			Width:   winPoints(m.W, m.DPI),
			Height:  winPoints(m.H, m.DPI),
			Scale:   winScale(m.DPI),
			Primary: m.Primary,
		}
		wx, wy, ww, wh := m.WorkX, m.WorkY, m.WorkW, m.WorkH
		if ww <= 0 || wh <= 0 {
			// A monitor the OS reported no work area for keeps all of itself,
			// which is the honest answer and the one a secondary display
			// normally deserves anyway.
			wx, wy, ww, wh = m.X, m.Y, m.W, m.H
		}
		s.VisibleX = winPoints(wx, m.DPI)
		s.VisibleY = winPoints(wy, m.DPI)
		s.VisibleWidth = winPoints(ww, m.DPI)
		s.VisibleHeight = winPoints(wh, m.DPI)
		out = append(out, s)
	}
	return primaryFirst(out)
}

// winDefaultDPI is USER_DEFAULT_SCREEN_DPI: the DPI at which a display is at
// 100% and one point is one pixel.
const winDefaultDPI = 96

// winScale is the display's backing factor, device pixels per logical point.
// A DPI the OS could not report (0) is 100%, which is what an unscaled desktop
// is and the only assumption that cannot make anything smaller than it should
// be.
func winScale(dpi int) float64 {
	if dpi <= 0 {
		return 1
	}
	return float64(dpi) / winDefaultDPI
}

// winPoints converts physical pixels to logical points at a given DPI.
//
// It rounds to NEAREST rather than truncating, and does it in a way that
// survives a negative coordinate: a monitor placed above or to the left of the
// primary one is at negative X or Y, which is ordinary and must not be
// clamped. math.Round rounds half AWAY from zero, so -0.5 becomes -1 and the
// left edge of such a monitor does not creep inwards.
func winPoints(px, dpi int) int {
	if dpi <= 0 || dpi == winDefaultDPI {
		return px
	}
	return int(math.Round(float64(px) * winDefaultDPI / float64(dpi)))
}

// edidRegistryPath turns a monitor's device INTERFACE path — what
// EnumDisplayDevicesW puts in DeviceID when asked for
// EDD_GET_DEVICE_INTERFACE_NAME —
//
//	\\?\DISPLAY#DELA0FF#5&2a5a2f3&0&UID4353#{e6f07b5f-ee97-4a90-b076-33f57bf4eaa7}
//
// into the key under HKEY_LOCAL_MACHINE that holds its EDID:
//
//	SYSTEM\CurrentControlSet\Enum\DISPLAY\DELA0FF\5&2a5a2f3&0&UID4353\Device Parameters
//
// It returns "" for anything that is not such a path — the hardware id the
// enumeration gives WITHOUT that flag included, which names the driver's class
// key and leads nowhere near an EDID.
//
// The two names in the middle are the panel's PnP manufacturer-and-product id
// and its instance; the trailing braces are the display-adapter interface
// class GUID and are not part of the key.
func edidRegistryPath(deviceID string) string {
	id := strings.TrimPrefix(deviceID, `\\?\`)
	parts := strings.Split(id, "#")
	if len(parts) < 3 || !strings.EqualFold(parts[0], "DISPLAY") {
		return ""
	}
	if parts[1] == "" || parts[2] == "" {
		return ""
	}
	return `SYSTEM\CurrentControlSet\Enum\DISPLAY\` + parts[1] + `\` + parts[2] + `\Device Parameters`
}
