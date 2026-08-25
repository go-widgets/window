// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package cocoa

import (
	"errors"
	"fmt"
	"sync"

	"github.com/ebitengine/purego"
)

// The window server's own account of the attached displays.
//
// WHY THIS EXISTS AT ALL. +[NSScreen screens] is a CACHE. AppKit fills it the
// first time it is read and refreshes it only when the application processes a
// display reconfiguration — which needs an NSApplication AND a turn of the main
// run loop. A process that has neither, which is any process on its way IN
// (list the displays, choose one, open a window), therefore reads the
// arrangement as it stood at the first read and keeps reading it for ever.
//
// Measured on this project's own machine, 2026-08-25: a MacBook plus a VITURE
// Beast, then three virtual displays created afterwards. CoreGraphics reported
// five displays with the Beast pushed out to (-7680,0); NSScreen went on
// reporting two, with the Beast still at (-1920,0), through repeated reads and
// through spinning the run loop. A window placed from the AppKit answer landed
// on a display the caller had never asked for — and no error was raised,
// because the stale list is self-consistent: a stale value looked up in a stale
// list matches.
//
// CoreGraphics has no such cache. CGGetActiveDisplayList and CGDisplayBounds
// ask the window server and are answered now. So this is where the geometry a
// caller receives comes from, and AppKit is asked only about what is properly
// its own — the localized name, the backing factor, the area the menu bar and
// the Dock leave free — keyed by CGDirectDisplayID rather than by a rectangle
// that may have stopped describing anything.

// cgRect is CoreGraphics' CGRect, flat rather than nested so the field order
// this is passed through purego by is the C one beyond any doubt. Its origin is
// TOP-LEFT with Y growing downwards — the same convention [ScreenInfo] uses,
// and the opposite of the bottom-left space AppKit places windows in.
type cgRect struct{ X, Y, W, H float64 }

// display is one attached display as the window server describes it: an
// identity, a rectangle and whether it owns the global origin.
type display struct {
	id     uint32
	bounds cgRect
	main   bool
}

// ErrDisplayList reports that the window server could not be asked for its
// display list. It is wrapped, never returned bare, so a caller can tell this
// apart from a display that is merely absent.
var ErrDisplayList = errors.New("cocoa: cannot read the window server's display list")

// coreGraphicsPath is the framework the display list is read from. It is a
// variable so a test can point the loader at something that does not exist and
// exercise the failure branch.
var coreGraphicsPath = "/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics"

var (
	cgOnce sync.Once
	cgErr  error

	cgMainDisplayID        func() uint32
	cgGetActiveDisplayList func(maxDisplays uint32, displays *uint32, count *uint32) int32
	cgDisplayBounds        func(id uint32) cgRect
)

// loadCoreGraphics binds the three display-list entry points, once.
func loadCoreGraphics() error {
	cgOnce.Do(func() {
		lib, err := purego.Dlopen(coreGraphicsPath, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			cgErr = fmt.Errorf("%w: %v", ErrDisplayList, err)
			return
		}
		purego.RegisterLibFunc(&cgMainDisplayID, lib, "CGMainDisplayID")
		purego.RegisterLibFunc(&cgGetActiveDisplayList, lib, "CGGetActiveDisplayList")
		purego.RegisterLibFunc(&cgDisplayBounds, lib, "CGDisplayBounds")
	})
	return cgErr
}

// liveDisplays returns every active display, main one first, as the window
// server describes it AT THIS MOMENT.
//
// The main display leads the slice because that is the display AppKit's
// coordinate space is anchored to, and because every caller that wants "the
// default screen" wants that one. The rest keep the order CoreGraphics gives.
func liveDisplays() ([]display, error) {
	if err := loadCoreGraphics(); err != nil {
		return nil, err
	}
	var n uint32
	if e := cgGetActiveDisplayList(0, nil, &n); e != 0 {
		return nil, fmt.Errorf("%w: CGGetActiveDisplayList counted: CGError %d", ErrDisplayList, e)
	}
	if n == 0 {
		return nil, nil
	}
	ids := make([]uint32, n)
	if e := cgGetActiveDisplayList(n, &ids[0], &n); e != 0 {
		return nil, fmt.Errorf("%w: CGGetActiveDisplayList: CGError %d", ErrDisplayList, e)
	}
	main := cgMainDisplayID()
	out := make([]display, 0, n)
	for _, id := range ids[:n] {
		d := display{id: id, bounds: cgDisplayBounds(id), main: id == main}
		if d.main {
			out = append([]display{d}, out...)
			continue
		}
		out = append(out, d)
	}
	return out, nil
}

// primaryBounds returns the rectangle of the display that owns the global
// origin. Its HEIGHT is the whole of the difference between AppKit's
// bottom-left coordinate space and the top-left one everything else here uses,
// which is why it is read live rather than remembered.
func primaryBounds() (cgRect, error) {
	ds, err := liveDisplays()
	if err != nil {
		return cgRect{}, err
	}
	if len(ds) == 0 || !ds[0].main {
		return cgRect{}, fmt.Errorf("%w: no main display", ErrDisplayList)
	}
	return ds[0].bounds, nil
}

// ---------------------------------------------------------------------------
// The main thread.
// ---------------------------------------------------------------------------

// libSystemPath carries pthread_main_np. It is a variable so a test can point
// the loader somewhere that does not exist and exercise the failure branch.
var libSystemPath = "/usr/lib/libSystem.B.dylib"

var (
	mainThreadOnce sync.Once
	pthreadMainNP  func() int32
)

// onMainThread reports whether the calling goroutine is running on the process
// MAIN OS thread — the one, and the only one, AppKit may be driven from.
//
// It is asked before doing anything that initialises AppKit off the beaten
// path. Go gives no such predicate: a goroutine may be scheduled onto any
// thread unless it has been locked to one, so "we are in main()" is not an
// answer. pthread_main_np is, and it is the answer for the thread that is
// running right now, which is what matters.
func onMainThread() bool {
	mainThreadOnce.Do(func() {
		lib, err := purego.Dlopen(libSystemPath, purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			return
		}
		purego.RegisterLibFunc(&pthreadMainNP, lib, "pthread_main_np")
	})
	return pthreadMainNP != nil && pthreadMainNP() != 0
}
