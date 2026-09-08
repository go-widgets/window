// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"errors"
	"testing"
)

// allOf adapts a ScreenList back to a slice, for the back-end tests written
// before the type existed. They are about the geometry a protocol reports, not
// about the list, so they read better unchanged.
func allOf(l ScreenList, err error) ([]Screen, error) { return l.All(), err }

// TestAnEmptyDisplayListIsRefused.
//
// ⛔⛔ THIS IS THE DEFECT THE TYPE EXISTS FOR. Screens used to answer []Screen,
// so an empty one could arrive, and the darwin back-end sent one: liveDisplays
// returned (nil, nil) when the window server counted zero. go-xrkit/desk looks
// its display up by name, found none, and quit -- twice on 2026-09-08, on a
// headset somebody was wearing.
func TestAnEmptyDisplayListIsRefused(t *testing.T) {
	for _, in := range [][]Screen{nil, {}} {
		l, err := newScreenList(in)
		if !errors.Is(err, ErrNoScreens) {
			t.Errorf("newScreenList(%v) = %v, want ErrNoScreens", in, err)
		}
		if l.Len() != 0 {
			t.Errorf("a refused list came back with %d screens", l.Len())
		}
	}
	// ⛔ AND ErrNoScreens IS NOT ErrScreensUnsupported. One says this build
	// cannot enumerate at all, which no retry will change; the other says the
	// enumeration ran and came back empty, which a second later may not.
	if errors.Is(ErrNoScreens, ErrScreensUnsupported) ||
		errors.Is(ErrScreensUnsupported, ErrNoScreens) {
		t.Error("the two sentinels answer to each other; they mean different things")
	}
}

// ⭐ EXACTLY ONE PRIMARY, FIRST, WHATEVER THE BACK-END HANDED OVER. This used
// to be primaryFirst applied by discipline, and every caller that wanted the
// main display then scanned for the flag again.
func TestTheListHasOnePrimaryAndItLeads(t *testing.T) {
	for _, c := range []struct {
		name string
		in   []Screen
		want string // the name that must end up primary and first
	}{
		{"the flagged one moves to the front",
			[]Screen{{Name: "a"}, {Name: "b", Primary: true}, {Name: "c"}}, "b"},
		{"two claims, the first wins",
			[]Screen{{Name: "a", Primary: true}, {Name: "b", Primary: true}}, "a"},
		{"nobody claims it, the first is it",
			[]Screen{{Name: "a"}, {Name: "b"}}, "a"},
	} {
		t.Run(c.name, func(t *testing.T) {
			l, err := newScreenList(c.in)
			if err != nil {
				t.Fatalf("newScreenList: %v", err)
			}
			if got := l.Primary().Name; got != c.want {
				t.Errorf("Primary() = %q, want %q", got, c.want)
			}
			all := l.All()
			if all[0].Name != c.want || !all[0].Primary {
				t.Errorf("All()[0] = %+v, want %q flagged primary", all[0], c.want)
			}
			n := 0
			for _, s := range all {
				if s.Primary {
					n++
				}
			}
			if n != 1 {
				t.Errorf("%d screens claim to be primary, want exactly 1", n)
			}
			if l.Len() != len(c.in) {
				t.Errorf("Len() = %d, want %d: no screen may be lost", l.Len(), len(c.in))
			}
		})
	}
}

func TestByNameFindsAndRefuses(t *testing.T) {
	l, err := newScreenList([]Screen{{Name: "Color LCD"}, {Name: "VITURE Beast"}})
	if err != nil {
		t.Fatalf("newScreenList: %v", err)
	}
	if s, ok := l.ByName("VITURE Beast"); !ok || s.Name != "VITURE Beast" {
		t.Errorf("ByName(%q) = %+v, %v", "VITURE Beast", s, ok)
	}
	if s, ok := l.ByName("nothing plugged in here"); ok {
		t.Errorf("ByName found %+v for a name that is not there", s)
	}
}

// The zero value says it is empty rather than pretending, and hands back a
// Screen that reports itself zero.
func TestTheZeroListIsEmptyAndSaysSo(t *testing.T) {
	var l ScreenList
	if l.Len() != 0 {
		t.Errorf("the zero ScreenList has %d screens", l.Len())
	}
	if !l.Primary().IsZero() {
		t.Errorf("the zero ScreenList has a primary: %+v", l.Primary())
	}
	if l.All() != nil {
		t.Errorf("the zero ScreenList's All() is %v, want nil", l.All())
	}
	if _, ok := l.ByName("anything"); ok {
		t.Error("the zero ScreenList found a screen by name")
	}
}
