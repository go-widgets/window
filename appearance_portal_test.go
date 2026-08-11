// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !js

package window

import (
	"errors"
	"testing"
	"time"

	"github.com/godbus/dbus/v5"
)

// fakePortal answers Settings.Read the way the portal does, so the unwrapping
// and the range handling can be checked without a desktop.
type fakePortal struct {
	dbus.BusObject
	values map[string]interface{}
	err    error
	calls  int
}

func (f *fakePortal) Call(method string, _ dbus.Flags, args ...interface{}) *dbus.Call {
	f.calls++
	c := &dbus.Call{Method: method}
	if f.err != nil {
		c.Err = f.err
		return c
	}
	key, _ := args[1].(string)
	v, ok := f.values[key]
	if !ok {
		c.Err = errors.New("no such key")
		return c
	}
	// The portal wraps the setting's own variant in the method's variant, which
	// is the detail that makes a single unwrap return a variant where a value
	// was expected.
	c.Body = []interface{}{dbus.MakeVariant(dbus.MakeVariant(v))}
	return c
}

func TestPortalReadUnwrapsTheDoubleVariant(t *testing.T) {
	obj := &fakePortal{values: map[string]interface{}{keyColorScheme: uint32(1)}}

	got, ok := portalRead(obj, appearanceNS, keyColorScheme)
	if !ok {
		t.Fatal("read failed")
	}
	if n, isU32 := got.Value().(uint32); !isU32 || n != 1 {
		t.Errorf("value = %#v, want uint32(1) — one unwrap too few leaves a variant", got.Value())
	}

	if _, ok := portalRead(obj, appearanceNS, "absent"); ok {
		t.Error("a missing key read as present")
	}
	failing := &fakePortal{err: errors.New("no portal")}
	if _, ok := portalRead(failing, appearanceNS, keyColorScheme); ok {
		t.Error("a failed call read as present")
	}
}

// 1 is dark; 0 and 2 are not, and neither is a value from a newer spec than
// this code -- "no preference" is the honest reading of something unknown.
func TestReadAppearanceColourScheme(t *testing.T) {
	for _, tc := range []struct {
		scheme uint32
		dark   bool
	}{{0, false}, {1, true}, {2, false}, {99, false}} {
		w := &Window{}
		w.portal.bus = nil
		obj := &fakePortal{values: map[string]interface{}{keyColorScheme: tc.scheme}}
		ap := appearanceFrom(obj)
		if ap.Dark != tc.dark {
			t.Errorf("color-scheme %d gave Dark=%v, want %v", tc.scheme, ap.Dark, tc.dark)
		}
	}
}

// An accent the user never chose is reported as absent rather than as black,
// because an application told "no accent" keeps its own and one told "black"
// paints with it.
func TestReadAppearanceAccent(t *testing.T) {
	withAccent := &fakePortal{values: map[string]interface{}{
		keyAccentColour: []interface{}{1.0, 0.5, 0.0},
	}}
	ap := appearanceFrom(withAccent)
	if !ap.HasAccent {
		t.Fatal("an accent was reported as absent")
	}
	if ap.Accent.R != 255 || ap.Accent.G != 128 || ap.Accent.B != 0 || ap.Accent.A != 255 {
		t.Errorf("accent = %+v, want opaque 255,128,0", ap.Accent)
	}

	none := &fakePortal{values: map[string]interface{}{}}
	if appearanceFrom(none).HasAccent {
		t.Error("no accent setting was reported as having one")
	}

	// A malformed value is not an accent either.
	wrong := &fakePortal{values: map[string]interface{}{
		keyAccentColour: []interface{}{1.0, "green", 0.0},
	}}
	if appearanceFrom(wrong).HasAccent {
		t.Error("a malformed accent was accepted")
	}
	short := &fakePortal{values: map[string]interface{}{
		keyAccentColour: []interface{}{1.0, 0.5},
	}}
	if appearanceFrom(short).HasAccent {
		t.Error("a two-component accent was accepted")
	}
}

// The portal is another process on a bus: reading it per frame would be sixty
// round trips a second for something a user changes once an hour.
func TestAppearanceIsCached(t *testing.T) {
	w := &Window{}
	w.portal.tried = true // no bus: every read returns the empty appearance
	first := w.Appearance()
	if !w.portal.at.IsZero() && time.Since(w.portal.at) > time.Second {
		t.Fatal("the reading was not stamped")
	}
	w.portal.cached = Appearance{Dark: true} // only a cache hit can return this
	if got := w.Appearance(); !got.Dark {
		t.Error("a second read within the window went back to the portal")
	}
	_ = first

	// Past the window, it reads again -- and gets the real (empty) answer.
	w.portal.at = time.Now().Add(-2 * appearanceCacheFor)
	if got := w.Appearance(); got.Dark {
		t.Error("a stale reading was served after the cache expired")
	}
}

// A desktop with no portal is not a broken desktop: no preference, no retry
// storm, no crash.
func TestAppearanceWithoutAPortal(t *testing.T) {
	w := &Window{}
	w.portal.tried = true
	if got := w.Appearance(); got.Dark || got.HasAccent {
		t.Errorf("a desktop with no portal reported %+v", got)
	}
	if _, ok := w.portalObject(); ok {
		t.Error("a failed connection was retried")
	}
}

func TestUnitByte(t *testing.T) {
	for _, tc := range []struct {
		in   float64
		want uint8
	}{
		{0, 0}, {1, 255}, {0.5, 128}, {0.25, 64},
		{-0.5, 0},    // below the range: clamp, not wrap
		{1.004, 255}, // a hair above, as a value off another process's bus can be
		{0.999, 255},
	} {
		if got := unitByte(tc.in); got != tc.want {
			t.Errorf("unitByte(%v) = %d, want %d", tc.in, got, tc.want)
		}
	}
}

// The platforms differ in what they can offer, and saying so is the answer.
func TestSystemFontTTFSaysThereIsNone(t *testing.T) {
	_, err := (&Window{}).SystemFontTTF()
	if err == nil {
		t.Fatal("X11 reported a system font file")
	}
	if !errors.Is(err, errNoSystemFont) {
		t.Errorf("error = %v, want the explanation", err)
	}
}
