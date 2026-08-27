// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package cocoa

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/go-macos/objc"
	"github.com/go-widgets/toolkit"
)

var (
	selScreens           = objc.RegisterName("screens")
	selLocalizedName     = objc.RegisterName("localizedName")
	selArrayCount        = objc.RegisterName("count")
	selObjectAtIndex     = objc.RegisterName("objectAtIndex:")
	selDeviceDescription = objc.RegisterName("deviceDescription")
	selObjectForKey      = objc.RegisterName("objectForKey:")
	selUnsignedIntValue  = objc.RegisterName("unsignedIntValue")
	selCurrentRunLoop    = objc.RegisterName("currentRunLoop")
	selRunModeBeforeDate = objc.RegisterName("runMode:beforeDate:")
	selDateWithInterval  = objc.RegisterName("dateWithTimeIntervalSinceNow:")
)

// nsScreenNumberKey is the deviceDescription key carrying a display's
// CGDirectDisplayID. It is the only stable link between an NSScreen and the
// display the window server means, and it is what this file matches on: a
// rectangle is not an identity, and AppKit's rectangles can be stale (see
// displaylist_darwin.go).
const nsScreenNumberKey = "NSScreenNumber"

// ScreenInfo describes one attached display. Sizes and positions are in LOGICAL
// points, the unit the toolkit lays out in.
//
// X and Y are TOP-LEFT origin, Y growing downwards, which is the convention
// every other back-end in this repo uses and the one a caller expects. AppKit's
// own space is bottom-left with Y growing up, so the two differ, and
// [ScreenInfo.appKitFrame] is the single place that difference is expressed.
//
// EVERY FIELD THAT PLACEMENT DEPENDS ON IS EXPORTED, deliberately. This value
// crosses a package boundary — window.Screen.toCocoa rebuilds one from the
// numbers a caller was given — and an unexported field cannot survive that
// crossing. There used to be one: a `nativeFrame` holding AppKit's own
// rectangle, which the enumerated value carried and a rebuilt one did not.
// Nothing said so; a window built from a rebuilt value simply went to the
// wrong place. Deriving the AppKit rectangle from the exported fields instead
// makes a rebuilt ScreenInfo place a window in exactly the same place as the
// enumerated one it was copied from, which is the only behaviour a caller can
// possibly expect.
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
}

// flipY converts a bottom-left-origin Y (AppKit) to a top-left-origin Y, given
// the height of the primary screen, which is where the global origin sits. It
// is the whole of the coordinate-space difference, kept separate so it can be
// tested without a display attached. It is its own inverse, so the same
// function converts in both directions.
func flipY(y, height, primaryHeight float64) float64 {
	return primaryHeight - (y + height)
}

// appKitFrame returns the rectangle AppKit would use for this screen: the same
// display, expressed in the bottom-left space -[NSWindow initWithContentRect:]
// reads. primaryHeight is the height of the display owning the global origin.
//
// It is computed from the exported fields and nothing else — see the type's
// documentation for why that matters.
func (s ScreenInfo) appKitFrame(primaryHeight float64) nsRect {
	return nsRect{
		Origin: nsPoint{X: float64(s.X), Y: flipY(float64(s.Y), float64(s.Height), primaryHeight)},
		Size:   nsSize{W: float64(s.Width), H: float64(s.Height)},
	}
}

// visibleTopLeftInAppKit returns the top-left corner of the screen's USABLE
// area — below the menu bar, right of nothing — in AppKit's bottom-left space.
// It is where a titled window's FRAME goes when a caller has chosen a display
// but not asked for the whole of it.
func (s ScreenInfo) visibleTopLeftInAppKit(primaryHeight float64) nsPoint {
	return nsPoint{X: float64(s.VisibleX), Y: primaryHeight - float64(s.VisibleY)}
}

// appKitScreen is what AppKit, and only AppKit, knows about a display: what it
// is called, how many pixels it puts in a point, and how much of it the menu
// bar and the Dock leave free.
type appKitScreen struct {
	name           string
	scale          float64
	frame, visible nsRect
}

// visibleInset returns how far the usable area is inset from the display's
// bounds: from the left, and from the TOP in top-left space.
//
// Insets rather than absolute coordinates on purpose. The inset is a fact about
// the menu bar and the Dock and stays true however out of date AppKit's idea of
// where the display sits happens to be, so it can be applied to the window
// server's live rectangle without importing the staleness along with it.
func (a appKitScreen) visibleInset() (left, top float64) {
	return a.visible.Origin.X - a.frame.Origin.X,
		(a.frame.Origin.Y + a.frame.Size.H) - (a.visible.Origin.Y + a.visible.Size.H)
}

// appKitScreens reads +[NSScreen screens] into a map keyed by the display's
// CGDirectDisplayID. Frameworks must already be loaded.
func appKitScreens() map[uint32]appKitScreen {
	out := map[uint32]appKitScreen{}
	arr := objc.ID(objc.GetClass("NSScreen")).Send(selScreens)
	if arr == 0 {
		return out
	}
	n := int(objc.Send[uint64](arr, selArrayCount))
	key := objc.NSString(nsScreenNumberKey)
	for i := 0; i < n; i++ {
		s := objc.Send[objc.ID](arr, selObjectAtIndex, uint64(i))
		if s == 0 {
			continue
		}
		desc := objc.Send[objc.ID](s, selDeviceDescription)
		if desc == 0 {
			continue
		}
		num := objc.Send[objc.ID](desc, selObjectForKey, key)
		if num == 0 {
			continue
		}
		id := uint32(objc.Send[uint64](num, selUnsignedIntValue))
		scale := float64(objc.Send[float64](s, selBackingScaleFactor))
		if scale <= 0 {
			scale = 1
		}
		out[id] = appKitScreen{
			name:    objc.GoString(objc.Send[objc.ID](s, selLocalizedName)),
			scale:   scale,
			frame:   objc.Send[nsRect](s, selFrame),
			visible: objc.Send[nsRect](s, selVisibleFrame),
		}
	}
	return out
}

// Screens enumerates the attached displays, the one owning the global origin
// first.
//
// It is safe to call before any window exists — which is the point: a caller
// picks its display, then passes the chosen ScreenInfo back in to place the
// window there. The geometry comes from the window server, so it is current
// even in a process that has not started an NSApplication; see
// displaylist_darwin.go for what that is worth.
func Screens() ([]ScreenInfo, error) {
	if err := loadFrameworks(); err != nil {
		return nil, err
	}
	live, err := liveDisplays()
	if err != nil {
		return nil, err
	}
	if len(live) == 0 {
		return nil, nil
	}
	// The geometry below comes from the window server and is right whatever
	// AppKit thinks. The NAME does not: it is AppKit's to know, and a display
	// that arrived while this process had no running application is not in
	// AppKit's list at all, so it would be reported nameless -- which is
	// precisely the display a user is most likely to be choosing, having just
	// plugged it in. So when the two lists disagree, and only then, AppKit is
	// asked to catch up. That needs the main thread; off it, the honest
	// unnamed answer stands rather than an undefined behaviour.
	if !appKitAgreesWithWindowServer() && onMainThread() {
		syncAppKitScreens(appKitScreenSyncTimeout)
	}
	ak := appKitScreens()
	out := make([]ScreenInfo, 0, len(live))
	for _, d := range live {
		s := ScreenInfo{
			X: int(d.bounds.X), Y: int(d.bounds.Y),
			Width: int(d.bounds.W), Height: int(d.bounds.H),
			VisibleX: int(d.bounds.X), VisibleY: int(d.bounds.Y),
			VisibleWidth: int(d.bounds.W), VisibleHeight: int(d.bounds.H),
			Scale:   1,
			Primary: d.main,
		}
		// A display AppKit has not caught up with yet keeps the window server's
		// bounds as its usable area and a scale of 1: an honest under-statement
		// rather than a value borrowed from a different display.
		if a, ok := ak[d.id]; ok {
			left, top := a.visibleInset()
			s.Name = a.name
			s.Scale = a.scale
			s.VisibleX = int(d.bounds.X + left)
			s.VisibleY = int(d.bounds.Y + top)
			s.VisibleWidth = int(a.visible.Size.W)
			s.VisibleHeight = int(a.visible.Size.H)
		}
		out = append(out, s)
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
//
// It returns an ERROR rather than a bare "not found", and the two failures are
// kept apart: a display that is genuinely no longer attached gives
// [ErrScreenGone], while a display list that could not be read gives the
// reading's own error. Reporting the second as the first would send a caller
// looking for a cable that is perfectly well plugged in.
func FindScreen(want ScreenInfo) (ScreenInfo, error) {
	screens, err := Screens()
	if err != nil {
		return ScreenInfo{}, err
	}
	for _, s := range screens {
		if s.Name == want.Name && s.X == want.X && s.Y == want.Y &&
			s.Width == want.Width && s.Height == want.Height {
			return s, nil
		}
	}
	attached := make([]string, 0, len(screens))
	for _, s := range screens {
		attached = append(attached, fmt.Sprintf("%q %dx%d at %d,%d", s.Name, s.Width, s.Height, s.X, s.Y))
	}
	return ScreenInfo{}, fmt.Errorf("%w: %q %dx%d at %d,%d; attached: %s",
		ErrScreenGone, want.Name, want.Width, want.Height, want.X, want.Y,
		strings.Join(attached, ", "))
}

// ErrScreenGone reports that the screen a caller asked for is no longer among
// the attached displays. It is a normal outcome, not a defect: an external
// display can be unplugged between being listed and being used.
var ErrScreenGone = errors.New("cocoa: the requested screen is no longer attached")

// appKitScreenSyncTimeout bounds how long window creation will wait for AppKit
// to notice a display arrangement the window server has already moved to. It is
// generous: on the machine this was measured on the notice arrives within one
// turn of the run loop, well under 100ms.
const appKitScreenSyncTimeout = 2 * time.Second

// syncAppKitScreens waits until +[NSScreen screens] agrees with the window
// server, and reports whether it got there.
//
// AppKit only refreshes that list when a RUNNING application processes a
// display reconfiguration. Merely re-reading it does nothing, and neither does
// turning the run loop with no NSApplication in existence — both were measured.
// So the caller must have created the shared application first, and this must
// run on the thread AppKit is driven from, which is where window creation
// already is.
//
// Why bother, when the geometry a window is placed at comes from the window
// server and not from here? Because AppKit still has to AGREE about which
// display that rectangle is on. A window placed at the right coordinates on a
// display AppKit has not heard of comes back with -[NSWindow screen] nil and a
// backing factor borrowed from the main display — so a framebuffer sized from
// it is twice the size the panel wants. That was measured too.
func syncAppKitScreens(timeout time.Duration) bool {
	app := objc.ID(objc.GetClass("NSApplication")).Send(selSharedApplication)
	deadline := time.Now().Add(timeout)
	for {
		if appKitAgreesWithWindowServer() {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		nudgeAppKit(app, 20*time.Millisecond)
	}
}

// appKitAgreesWithWindowServer reports whether AppKit's cached screen list
// describes the same displays, in the same places, as the window server does.
//
// Comparing the frames and not merely the identities is deliberate: adding a
// display MOVES the others, and it is the move — not the addition — that puts a
// window on the wrong panel.
func appKitAgreesWithWindowServer() bool {
	live, err := liveDisplays()
	if err != nil || len(live) == 0 {
		// Nothing to reconcile against; do not spin waiting for an answer that
		// is not coming.
		return true
	}
	ak := appKitScreens()
	if len(ak) != len(live) {
		return false
	}
	primaryHeight := live[0].bounds.H
	for _, d := range live {
		a, ok := ak[d.id]
		if !ok {
			return false
		}
		want := ScreenInfo{
			X: int(d.bounds.X), Y: int(d.bounds.Y),
			Width: int(d.bounds.W), Height: int(d.bounds.H),
		}.appKitFrame(primaryHeight)
		if a.frame != want {
			return false
		}
	}
	return true
}

// nudgeAppKit asks the application for an event and then gives the run loop one
// bounded turn, which between them are what make AppKit notice that the desktop
// has changed shape.
//
// Both halves are necessary and neither is obvious. Turning the run loop alone
// does nothing, however long for: measured at three seconds with an
// NSApplication in existence, an activation policy set and -finishLaunching
// called, and the screen list did not move. What moves it is
// -[NSApplication nextEventMatchingMask:...], which is where AppKit connects
// itself to the window server; after that the notification arrives within a
// single turn — measured at 13ms.
//
// The event is PEEKED, not dequeued (dequeue:NO). Taking events off the queue
// here would consume input that belongs to the window about to be created, or
// to an application already running one; asking whether there is an event is
// enough to make the connection.
func nudgeAppKit(app objc.ID, d time.Duration) {
	distantPast := objc.ID(objc.GetClass("NSDate")).Send(selDistantPast)
	objc.Send[objc.ID](app, selNextEvent, ^uint64(0), distantPast,
		objc.NSString("kCFRunLoopDefaultMode"), false)
	rl := objc.ID(objc.GetClass("NSRunLoop")).Send(selCurrentRunLoop)
	if rl == 0 {
		return
	}
	until := objc.ID(objc.GetClass("NSDate")).Send(selDateWithInterval, d.Seconds())
	rl.Send(selRunModeBeforeDate, objc.NSString("kCFRunLoopDefaultMode"), until)
}

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
	// Immersive puts the window ABOVE the platform's own furniture instead of
	// underneath it.
	//
	// A borderless window at a display's exact bounds covers the desktop, and
	// nothing more: the menu bar and the Dock are drawn by the window server at
	// levels above an ordinary window, so on a display that carries them they
	// appear ON TOP of the picture. On glasses showing a captured desktop that
	// reads as two menu bars, one of them belonging to a screen the viewer is
	// not looking at.
	//
	// This is a window LEVEL and not -[NSApp setPresentationOptions:], on
	// purpose. Presentation options apply only while the application is active,
	// and an immersive surface driven by global shortcuts is used precisely
	// while another application has the keyboard: the menu bar would come back
	// over the picture the moment the viewer typed anywhere else.
	Immersive bool
	// FixedSize makes the window unresizable: no resize control, no drag on an
	// edge, no zoom.
	//
	// For a window sized to its own content there is nothing to gain from
	// resizing it and something to lose: room has to be found for what a smaller
	// window cannot show, which means a scrollbar in a dialogue that never needs
	// to scroll, or a layout that reflows into something nobody designed.
	FixedSize bool
}

// resolveScreen turns the requested placement into a live screen, or nil when
// the window should be placed by macOS as before.
func (o Options) resolveScreen() (*ScreenInfo, error) {
	if o.Screen != nil {
		s, err := FindScreen(*o.Screen)
		if err != nil {
			return nil, err
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
	// Fullscreen was asked for and there is no display to be full-screen on.
	// Refusing is the only honest answer: a window opened anyway would be a
	// window nobody can see.
	return nil, fmt.Errorf("%w: no display owns the desktop origin, so there is "+
		"no primary screen to cover", ErrScreenGone)
}
