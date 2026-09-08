// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import "fmt"

// ScreenList is the display list of a live system: NEVER EMPTY, primary first,
// and with exactly one primary — by construction rather than by convention.
//
// ⛔⛔ BOTH HALVES WERE CONVENTIONS, AND ONE OF THEM COST SOMEBODY THEIR DESK.
// Screens used to answer []Screen, so nothing stopped an empty one arriving,
// and the darwin back-end sent one: liveDisplays returned (nil, nil) when the
// window server counted zero, which is a failed read wearing the clothes of a
// fact. go-xrkit/desk looks its display up by name, found none, and quit --
// twice on 2026-09-08, on a headset somebody was wearing:
//
//	desk: "VITURE Beast" is not attached any more; there is  -- stopping
//
// Nothing after "there is": the message names every attached display and named
// NONE. Three of the four back-ends already refused to say it (Windows and
// Wayland returned an error, and primaryBounds called it ErrDisplayList); the
// fourth was the one that shipped.
//
// ⭐ AND THE PRIMARY WAS A REPAIR RATHER THAN A GUARANTEE. primaryFirst walked
// the slice unsetting duplicate flags -- "only one may claim it" -- and every
// caller that wanted the main display then scanned for the flag again. It is a
// field of the list, not a property to go looking for.
//
// The zero ScreenList is empty and reports itself so; there is no way to build
// a populated one but [newScreenList], which is where both rules live.
type ScreenList struct {
	// all is primary-first, exactly one Primary, and either empty (the zero
	// value) or complete. It is never partially built.
	all []Screen
}

// newScreenList is the only way a populated ScreenList comes into being.
//
// It REFUSES an empty slice, because a live display server always has a screen:
// somebody is looking at something. A back-end that has nothing to report has
// failed to read, and must say so as an error rather than as a list.
func newScreenList(screens []Screen) (ScreenList, error) {
	if len(screens) == 0 {
		return ScreenList{}, fmt.Errorf(
			"window: the display server reported no screen at all, which a live "+
				"one does not have -- this is a read that failed, not a machine "+
				"with nothing attached: %w", ErrNoScreens)
	}
	return ScreenList{all: primaryFirst(screens)}, nil
}

// ErrNoScreens is what a back-end that could not read the display list reports.
//
// It is deliberately NOT [ErrScreensUnsupported]: that one means this platform
// has no way to enumerate at all, which is a fact about the build. This one
// means the enumeration ran and came back with nothing, which is a fact about
// the moment and may be different a second later.
var ErrNoScreens = fmt.Errorf("window: no screens in the display list")

// Len is how many screens there are. It is at least 1 for any list a back-end
// returned, and 0 only for the zero value.
func (l ScreenList) Len() int { return len(l.all) }

// Primary is the display that owns the desktop's origin.
//
// ⭐ A FIELD RATHER THAN A SEARCH. Every caller that wanted it used to loop
// looking for Screen.Primary, which is a scan for something the list already
// knew — and a scan that has to decide what to do when it finds none.
//
// The zero value has no screens and returns the zero Screen, which
// [Screen.IsZero] reports.
func (l ScreenList) Primary() Screen {
	if len(l.all) == 0 {
		return Screen{}
	}
	return l.all[0]
}

// All is every screen, primary first.
//
// The slice is the list's own: reading it is free, and writing to it would
// break the guarantees this type exists for. Copy it if you mean to sort it.
func (l ScreenList) All() []Screen { return l.all }

// ByName is the screen called name, and whether there is one.
//
// ⭐ IT IS HERE BECAUSE EVERY CALLER WROTE IT. Looking a display up by name is
// what survives the desktop being rearranged — creating a virtual display moves
// the others, so a rectangle captured a moment ago names nothing — and three
// separate loops were doing it.
func (l ScreenList) ByName(name string) (Screen, bool) {
	for _, s := range l.all {
		if s.Name == name {
			return s, true
		}
	}
	return Screen{}, false
}
