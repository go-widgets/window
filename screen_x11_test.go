// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package window

import (
	"bytes"
	"strings"
	"testing"

	"github.com/go-widgets/window/internal/x11"

	xproto "github.com/go-freedesktop/x11"
)

// X11 states a display in device pixels, on a screen with no notion of a
// usable area and no per-panel scale. [Screen] is in logical points with the
// desktop's panels excluded. Everything below is about that projection, which
// is where a caller switching platforms would otherwise have to change how it
// reasons.

// monSpec describes one RANDR monitor for the scripted server.
type monSpec struct {
	NameAtom      uint32
	Name          string
	Primary       bool
	X, Y          int16
	Width, Height uint16
	Outputs       []uint32
	Model         string // the EDID product name its output publishes, if any
}

// randrScreenScript builds the server's whole side of a screensOn exchange:
// the RANDR enumeration, the resource database the scale comes from, and the
// work area. Passing db == "" means the desktop publishes no Xft.dpi, and
// work == nil means no window manager has claimed any of the screen.
func randrScreenScript(mons []monSpec, db string, work []uint32) []byte {
	var script []byte
	add := func(b []byte) { script = append(script, b...) }

	add(extensionReply(true, 140)) // QueryExtension(RANDR)
	add(rrVersionReply(1, 5))      // RRQueryVersion
	add(monitorsReply(mons))       // RRGetMonitors
	for _, m := range mons {       // GetAtomName, one per named monitor
		if m.NameAtom != 0 {
			add(atomNameReply(m.Name))
		}
	}
	add(internReply(0x51)) // InternAtom("EDID")
	for _, m := range mons {
		for range m.Outputs {
			add(edidPropertyReply(m.Model))
		}
	}

	if db == "" {
		add(internReply(0)) // RESOURCE_MANAGER has never been interned
	} else {
		add(internReply(atomResourceManager))
		add(propertyReply(db))
	}

	if work == nil {
		add(internReply(0)) // _NET_WORKAREA likewise
	} else {
		add(internReply(0x60))
		add(cardinalsReply(work...))
	}
	return script
}

// extensionReply builds a QueryExtension reply.
func extensionReply(present bool, major byte) []byte {
	pkt := make([]byte, 32)
	pkt[0] = 1
	if present {
		pkt[8] = 1
	}
	pkt[9] = major
	return pkt
}

// rrVersionReply builds an RRQueryVersion reply.
func rrVersionReply(major, minor uint32) []byte {
	pkt := make([]byte, 32)
	pkt[0] = 1
	le.PutUint32(pkt[8:12], major)
	le.PutUint32(pkt[12:16], minor)
	return pkt
}

// monitorsReply builds an RRGetMonitors reply, which is the MONITORINFO list.
func monitorsReply(mons []monSpec) []byte {
	e := xproto.NewEncoder(le)
	for _, m := range mons {
		e.Put32(m.NameAtom)
		var p byte
		if m.Primary {
			p = 1
		}
		e.Put8(p)
		e.Put8(1) // automatic
		e.Put16(uint16(len(m.Outputs)))
		e.Put16(uint16(m.X))
		e.Put16(uint16(m.Y))
		e.Put16(m.Width)
		e.Put16(m.Height)
		e.Put32(uint32(m.Width) * 254 / 960) // a plausible physical size
		e.Put32(uint32(m.Height) * 254 / 960)
		for _, o := range m.Outputs {
			e.Put32(o)
		}
	}
	body := e.Bytes()
	pkt := make([]byte, 32+len(body))
	pkt[0] = 1
	le.PutUint32(pkt[4:8], uint32(len(body)/4))
	le.PutUint32(pkt[12:16], uint32(len(mons)))
	copy(pkt[32:], body)
	return pkt
}

// atomNameReply builds a GetAtomName reply.
func atomNameReply(name string) []byte {
	e := xproto.NewEncoder(le)
	e.PutString(name)
	body := e.Bytes()
	pkt := make([]byte, 32+len(body))
	pkt[0] = 1
	le.PutUint32(pkt[4:8], uint32(len(body)/4))
	le.PutUint16(pkt[8:10], uint16(len(name)))
	copy(pkt[32:], body)
	return pkt
}

// edidPropertyReply builds an RRGetOutputProperty reply carrying a base EDID
// naming model, or an absent property when model is "".
func edidPropertyReply(model string) []byte {
	if model == "" {
		return make32ByteReply(0)
	}
	edid := make([]byte, 128)
	copy(edid, []byte{0x00, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x00})
	d := edid[54:72]
	d[3] = 0xfc
	for i := 5; i < 18; i++ {
		d[i] = ' '
	}
	if n := copy(d[5:], model); 5+n < 18 {
		d[5+n] = '\n'
	}
	pkt := make([]byte, 32+len(edid))
	pkt[0] = 1
	pkt[1] = 8 // format
	le.PutUint32(pkt[4:8], uint32(len(edid)/4))
	le.PutUint32(pkt[8:12], 19) // INTEGER
	le.PutUint32(pkt[16:20], uint32(len(edid)))
	copy(pkt[32:], edid)
	return pkt
}

// make32ByteReply builds a bare reply whose format byte is f — format 0 being
// how the server says the property is not there.
func make32ByteReply(f byte) []byte {
	pkt := make([]byte, 32)
	pkt[0] = 1
	pkt[1] = f
	return pkt
}

// cardinalsReply builds a GetProperty reply carrying 32-bit CARDINALs, which
// is how EWMH publishes _NET_WORKAREA.
func cardinalsReply(values ...uint32) []byte {
	data := make([]byte, 4*len(values))
	for i, v := range values {
		le.PutUint32(data[i*4:], v)
	}
	pkt := make([]byte, 32+len(data))
	pkt[0] = 1
	pkt[1] = 32 // format
	le.PutUint32(pkt[4:8], uint32(len(data)/4))
	le.PutUint32(pkt[8:12], 6) // CARDINAL
	le.PutUint32(pkt[16:20], uint32(len(values)))
	copy(pkt[32:], data)
	return pkt
}

// dialScripted hands screensOn a connection whose server answers from script.
func dialScripted(t *testing.T, script []byte) *x11.Conn {
	t.Helper()
	ft := &fakeTransport{in: bytes.NewReader(append(setupReply(), script...))}
	conn, err := x11.Handshake(ft, le, "", nil)
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	return conn
}

func TestScreensOnProjectsPixelsOntoPoints(t *testing.T) {
	mons := []monSpec{
		{NameAtom: 0x40, Name: "eDP-1", Primary: true, Width: 1920, Height: 1080,
			Outputs: []uint32{0x42}, Model: "VITURE Beast"},
		{NameAtom: 0x41, Name: "HDMI-1", X: 1920, Width: 1920, Height: 1080},
	}
	// A 192 dpi desktop with a 27-pixel panel across the top.
	conn := dialScripted(t, randrScreenScript(mons, "Xft.dpi:\t192\n",
		[]uint32{0, 27, 3840, 1053}))

	screens, err := screensOn(conn, 0)
	if err != nil {
		t.Fatalf("screensOn: %v", err)
	}
	if len(screens) != 2 {
		t.Fatalf("got %d screens, want 2", len(screens))
	}
	want := []Screen{
		{Name: "VITURE Beast", X: 0, Y: 0, Width: 960, Height: 540,
			VisibleX: 0, VisibleY: 13, VisibleWidth: 960, VisibleHeight: 526,
			Scale: 2, Primary: true},
		{Name: "HDMI-1", X: 960, Y: 0, Width: 960, Height: 540,
			VisibleX: 960, VisibleY: 13, VisibleWidth: 960, VisibleHeight: 526,
			Scale: 2},
	}
	for i := range want {
		if screens[i] != want[i] {
			t.Errorf("screen %d = %+v\n            want %+v", i, screens[i], want[i])
		}
	}
	// The panel's own name is what a user recognises; the connector is the
	// fallback for an output that publishes no EDID.
	if screens[0].Name == "eDP-1" {
		t.Error("the connector was reported for a display that publishes a model")
	}
}

func TestScreensOnWithNoWindowManagerAndNoScale(t *testing.T) {
	mons := []monSpec{{NameAtom: 0x40, Name: "screen", Width: 1920, Height: 1080}}
	conn := dialScripted(t, randrScreenScript(mons, "", nil))

	screens, err := screensOn(conn, 0)
	if err != nil {
		t.Fatalf("screensOn: %v", err)
	}
	want := Screen{Name: "screen", Width: 1920, Height: 1080,
		VisibleWidth: 1920, VisibleHeight: 1080, Scale: 1, Primary: true}
	if len(screens) != 1 || screens[0] != want {
		t.Fatalf("got %+v, want %+v", screens, want)
	}
}

func TestScreensOnSurvivesAServerThatAnswersNothing(t *testing.T) {
	// No RANDR, no XINERAMA, no resource manager, no work area: the screen
	// itself is still a display, and a caller that asked for a list must not
	// get an empty one.
	conn := dialScripted(t, nil)
	screens, err := screensOn(conn, 0)
	if err != nil {
		t.Fatalf("screensOn: %v", err)
	}
	if len(screens) != 1 {
		t.Fatalf("got %d screens, want the screen itself", len(screens))
	}
	s := screens[0]
	if s.Width != 800 || s.Height != 600 || s.Scale != 1 || !s.Primary || s.Name != "" {
		t.Fatalf("got %+v, want the setup screen's 800x600 at 1:1, primary, nameless", s)
	}
}

func TestScreensOnRefusesAScreenTheServerDoesNotHave(t *testing.T) {
	conn := dialScripted(t, nil)
	if _, err := screensOn(conn, 3); err == nil {
		t.Fatal("screensOn accepted screen 3 of a one-screen server")
	}
}

// primaryFirst carries two promises of the Screen contract that nothing on
// X11 provides on its own.
func TestPrimaryFirst(t *testing.T) {
	for _, tc := range []struct {
		name    string
		in      []Screen
		wantIdx []string // the names, in the order they must come back
		primary string
	}{
		{"already first",
			[]Screen{{Name: "a", Primary: true}, {Name: "b"}},
			[]string{"a", "b"}, "a"},
		{"the second display is the primary one",
			[]Screen{{Name: "a"}, {Name: "b", Primary: true}, {Name: "c"}},
			[]string{"b", "a", "c"}, "b"},
		// A bare X server marks nothing primary — an Xvfb reports its single
		// monitor as automatic and not primary — and a caller looking for the
		// main display would find none.
		{"nobody claims it",
			[]Screen{{Name: "a"}, {Name: "b"}},
			[]string{"a", "b"}, "a"},
		// XINERAMA's first screen and a RANDR primary could both be set if the
		// two ever disagreed; exactly one may survive.
		{"two claim it",
			[]Screen{{Name: "a", Primary: true}, {Name: "b", Primary: true}},
			[]string{"a", "b"}, "a"},
		{"no displays at all", nil, nil, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := primaryFirst(append([]Screen(nil), tc.in...))
			if len(got) != len(tc.wantIdx) {
				t.Fatalf("got %d screens, want %d", len(got), len(tc.wantIdx))
			}
			n := 0
			for i, s := range got {
				if s.Name != tc.wantIdx[i] {
					t.Errorf("position %d is %q, want %q", i, s.Name, tc.wantIdx[i])
				}
				if s.Primary {
					n++
					if s.Name != tc.primary {
						t.Errorf("%q carries the primary flag, want %q", s.Name, tc.primary)
					}
				}
			}
			if len(got) > 0 && n != 1 {
				t.Errorf("%d screens carry the primary flag, want exactly 1", n)
			}
		})
	}
}

func TestIntersect(t *testing.T) {
	for _, tc := range []struct {
		name string
		a, b [4]int
		want [4]int
		ok   bool
	}{
		{"a panel across the top", [4]int{0, 0, 1920, 1080}, [4]int{0, 27, 1920, 1053},
			[4]int{0, 27, 1920, 1053}, true},
		{"the second monitor of a row", [4]int{1920, 0, 1920, 1080}, [4]int{0, 27, 3840, 1053},
			[4]int{1920, 27, 1920, 1053}, true},
		{"a work area that does not reach this monitor", [4]int{1920, 0, 1920, 1080},
			[4]int{0, 0, 1920, 1080}, [4]int{}, false},
		{"touching edges are not an overlap", [4]int{0, 0, 100, 100}, [4]int{100, 0, 100, 100},
			[4]int{}, false},
		{"a negative origin", [4]int{-1920, 0, 1920, 1080}, [4]int{-1920, 27, 3840, 1053},
			[4]int{-1920, 27, 1920, 1053}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			x, y, w, h, ok := intersect(tc.a[0], tc.a[1], tc.a[2], tc.a[3],
				tc.b[0], tc.b[1], tc.b[2], tc.b[3])
			if ok != tc.ok || (ok && [4]int{x, y, w, h} != tc.want) {
				t.Errorf("intersect = %v,%v,%v,%v,%v want %v,%v", x, y, w, h, ok, tc.want, tc.ok)
			}
		})
	}
}

func TestPoints(t *testing.T) {
	for _, tc := range []struct {
		px, scale, want int
	}{
		{1920, 1, 1920},
		{3840, 2, 1920},
		{27, 2, 13},
		{100, 0, 100},  // a scale nobody set is 1:1, not a division by zero
		{100, -1, 100}, // likewise
	} {
		if got := points(tc.px, tc.scale); got != tc.want {
			t.Errorf("points(%d, %d) = %d, want %d", tc.px, tc.scale, got, tc.want)
		}
	}
}

func TestScreensNeedsADisplayServer(t *testing.T) {
	t.Setenv("DISPLAY", "")
	t.Setenv("WAYLAND_DISPLAY", "")
	if _, err := Screens(); err == nil {
		t.Fatal("Screens succeeded with no display server in the environment")
	}
}

// Screens must ask the SAME server Open would dial, or a caller could pick a
// display off one and open a window on another.
func TestScreensPrefersWaylandExactlyAsOpenDoes(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	t.Setenv("WAYLAND_DISPLAY", "wayland-nothing-is-listening")
	t.Setenv("DISPLAY", ":987")
	_, err := Screens()
	if err == nil {
		t.Fatal("Screens succeeded with neither server actually running")
	}
	// The failure has to come from the Wayland dial, not the X one.
	if !strings.Contains(err.Error(), "Wayland") {
		t.Errorf("error %q does not come from the Wayland back-end", err)
	}
}

func TestScreensReportsABadDisplay(t *testing.T) {
	t.Setenv("DISPLAY", "not a display")
	if _, err := Screens(); err == nil {
		t.Fatal("Screens accepted a DISPLAY with no colon in it")
	}
	// A well-formed DISPLAY that nothing is listening on fails at the dial.
	t.Setenv("DISPLAY", ":987")
	if _, err := Screens(); err == nil {
		t.Fatal("Screens succeeded against a display with no server on it")
	}
}

// A model names the MODEL, and two identical monitors say the identical thing
// about themselves. Which display is which is then a question only the
// connector can answer.
func TestResolveNames(t *testing.T) {
	for _, tc := range []struct {
		name string
		ids  []displayName
		want []string
	}{
		{"a model that identifies one display",
			[]displayName{{Model: "DELL U2720Q", Connector: "DP-2"}},
			[]string{"DELL U2720Q"}},
		{"two identical monitors",
			[]displayName{
				{Model: "DELL U2720Q", Connector: "DP-1"},
				{Model: "DELL U2720Q", Connector: "DP-2"},
			},
			[]string{"DP-1", "DP-2"}},
		// wlroots publishes the literal "Unknown" for every headless output,
		// which is a placeholder and not a name.
		{"a compositor with one placeholder for everything",
			[]displayName{
				{Model: "Unknown", Connector: "HEADLESS-1", Vendor: "Unknown"},
				{Model: "Unknown", Connector: "HEADLESS-2", Vendor: "Unknown"},
			},
			[]string{"HEADLESS-1", "HEADLESS-2"}},
		{"no model at all",
			[]displayName{{Connector: "HDMI-1"}}, []string{"HDMI-1"}},
		{"nothing but a manufacturer",
			[]displayName{{Vendor: "DELL"}}, []string{"DELL"}},
		// An ambiguous model with no connector to fall back on is still all
		// the display has said.
		{"ambiguous, and nothing to disambiguate with",
			[]displayName{{Model: "Panel"}, {Model: "Panel"}},
			[]string{"Panel", "Panel"}},
		{"a display that says nothing", []displayName{{}}, []string{""}},
		{"no displays", nil, []string{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := resolveNames(tc.ids)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d names, want %d", len(got), len(tc.want))
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("name %d = %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}
