// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package window

import (
	"testing"

	"github.com/go-widgets/window/internal/wayland"
)

// outSpec is one wl_output for the scripted compositor to advertise.
type outSpec struct {
	X, Y                int32
	PhysWMM, PhysHMM    int32
	Make, Model         string
	Connector, Descr    string
	Transform           uint32
	ModeW, ModeH        int32
	Refresh             int32
	Scale               uint32
	SkipDone            bool // announce the burst but never close it
	AlsoNonCurrentModes bool // list modes the output is NOT running
}

// fakeOutputCompositor plays a compositor that advertises the given outputs
// and describes each one when it is bound. It is the display-enumeration half
// of fakeCompositor, kept separate because nothing here needs a surface.
func fakeOutputCompositor(sc *srvConn, outs []outSpec) {
	var registryID uint32
	bound := map[uint32]int{} // object id -> index into outs

	for {
		obj, op, body, err := sc.read()
		if err != nil {
			return // client closed
		}
		switch {
		case obj == 1 && op == 1: // wl_display.get_registry
			registryID = no.Uint32(body[0:4])
			for i := range outs {
				_ = sc.send(registryID, 0, cat(eU32(uint32(10+i)), eStr("wl_output"), eU32(4)))
			}
		case obj == 1 && op == 0: // wl_display.sync
			_ = sc.send(no.Uint32(body[0:4]), 0, eU32(0)) // wl_callback.done
		case obj == registryID && op == 0: // wl_registry.bind
			name := no.Uint32(body[0:4])
			iface, rest := decStr(body[4:])
			newid := no.Uint32(rest[4:8])
			if iface != "wl_output" {
				continue
			}
			i := int(name - 10)
			bound[newid] = i
			o := outs[i]
			_ = sc.send(newid, 0, cat( // geometry
				eU32(uint32(o.X)), eU32(uint32(o.Y)),
				eU32(uint32(o.PhysWMM)), eU32(uint32(o.PhysHMM)),
				eU32(1), // subpixel: horizontal RGB
				eStr(o.Make), eStr(o.Model),
				eU32(o.Transform)))
			if o.AlsoNonCurrentModes {
				// Every mode the panel supports is announced; only the one
				// flagged current says how big the screen is right now.
				_ = sc.send(newid, 1, cat(eU32(0), eU32(640), eU32(480), eU32(60000)))
			}
			_ = sc.send(newid, 1, cat(eU32(1), // mode, flags = current
				eU32(uint32(o.ModeW)), eU32(uint32(o.ModeH)), eU32(uint32(o.Refresh))))
			if o.Scale > 0 {
				_ = sc.send(newid, 3, eU32(o.Scale)) // scale
			}
			if o.Connector != "" {
				_ = sc.send(newid, 4, eStr(o.Connector)) // name (wl_output 4)
			}
			if o.Descr != "" {
				_ = sc.send(newid, 5, eStr(o.Descr)) // description (wl_output 4)
			}
			if !o.SkipDone {
				_ = sc.send(newid, 2, nil) // done
			}
		}
	}
}

// dialFakeOutputs runs the scripted compositor over a socket pair and returns
// what screensOnWayland made of it.
func dialFakeOutputs(t *testing.T, outs []outSpec) ([]Screen, error) {
	t.Helper()
	cli, srv := socketPairWin(t)
	t.Cleanup(func() { _ = srv.Close() })
	go fakeOutputCompositor(&srvConn{c: srv}, outs)
	return screensOnWayland(wayland.New(cli))
}

func TestWaylandScreensReadTheOutputBurst(t *testing.T) {
	screens, err := dialFakeOutputs(t, []outSpec{
		// A 2x laptop panel: 2560x1440 device pixels are 1280x720 points.
		{Make: "Sharp", Model: "LQ133M1", Connector: "eDP-1", Descr: "the built-in panel",
			PhysWMM: 294, PhysHMM: 165,
			ModeW: 2560, ModeH: 1440, Refresh: 60000, Scale: 2, AlsoNonCurrentModes: true},
		// An external 1x screen to its right, in the compositor's LOGICAL
		// space, which is where the first one ends.
		{X: 1280, Make: "DELL", Model: "U2720Q", Connector: "DP-2",
			PhysWMM: 597, PhysHMM: 336,
			ModeW: 1920, ModeH: 1080, Refresh: 59951, Scale: 1},
	})
	if err != nil {
		t.Fatalf("screensOnWayland: %v", err)
	}
	want := []Screen{
		{Name: "LQ133M1", Width: 1280, Height: 720,
			VisibleWidth: 1280, VisibleHeight: 720, Scale: 2, Primary: true},
		{Name: "U2720Q", X: 1280, Width: 1920, Height: 1080,
			VisibleX: 1280, VisibleWidth: 1920, VisibleHeight: 1080, Scale: 1},
	}
	if len(screens) != len(want) {
		t.Fatalf("got %d screens, want %d", len(screens), len(want))
	}
	for i := range want {
		if screens[i] != want[i] {
			t.Errorf("screen %d = %+v\n            want %+v", i, screens[i], want[i])
		}
	}
}

func TestWaylandScreensSwapTheAxesOfARotatedPanel(t *testing.T) {
	// transform 1 is a quarter turn: a 1080x1920 panel in portrait is a
	// 1920x1080 mode with its axes swapped, and reporting it unswapped would
	// overlap whatever sits beside it with nothing saying so.
	screens, err := dialFakeOutputs(t, []outSpec{
		{Model: "Portrait", Transform: 1, ModeW: 1920, ModeH: 1080, Scale: 1},
	})
	if err != nil {
		t.Fatalf("screensOnWayland: %v", err)
	}
	if len(screens) != 1 || screens[0].Width != 1080 || screens[0].Height != 1920 {
		t.Fatalf("got %+v, want a 1080x1920 portrait screen", screens)
	}
}

// The panel's own product name first, its connector next, its manufacturer
// last — the same order the X11 back-end uses, so a caller reading Screen.Name
// need not know which session it is in.
func TestWaylandScreenNamePrefersTheModel(t *testing.T) {
	for _, tc := range []struct {
		name string
		out  outSpec
		want string
	}{
		{"a panel that names itself",
			outSpec{Make: "DELL", Model: "U2720Q", Connector: "DP-2"}, "U2720Q"},
		{"no model: the connector says which socket",
			outSpec{Make: "DELL", Connector: "DP-2"}, "DP-2"},
		{"a placeholder model, which wlroots publishes for every headless output",
			outSpec{Make: "Unknown", Model: "Unknown", Connector: "HEADLESS-1"}, "Unknown"},
		{"no connector either: a manufacturer beats nothing",
			outSpec{Make: "DELL"}, "DELL"},
		{"a compositor that says nothing at all", outSpec{}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.out.ModeW, tc.out.ModeH, tc.out.Scale = 800, 600, 1
			screens, err := dialFakeOutputs(t, []outSpec{tc.out})
			if err != nil {
				t.Fatalf("screensOnWayland: %v", err)
			}
			if len(screens) != 1 || screens[0].Name != tc.want {
				t.Fatalf("got %+v, want the name %q", screens, tc.want)
			}
		})
	}
}

func TestWaylandScreensWithNoOutputAtAll(t *testing.T) {
	// A compositor with no output is not a desktop with an unnamed one: there
	// is nothing to put a window on, and saying so is the only honest answer.
	if _, err := dialFakeOutputs(t, nil); err == nil {
		t.Fatal("screensOnWayland invented a screen for a compositor with none")
	}
}

func TestWaylandScreensIgnoreAnUnfinishedBurst(t *testing.T) {
	// Properties published without a closing done describe nothing yet: acting
	// on half a burst would place the output where the compositor never said.
	screens, err := dialFakeOutputs(t, []outSpec{
		{X: 500, Model: "Half", ModeW: 1920, ModeH: 1080, Scale: 2, SkipDone: true},
	})
	if err != nil {
		t.Fatalf("screensOnWayland: %v", err)
	}
	if len(screens) != 1 {
		t.Fatalf("got %d screens, want 1", len(screens))
	}
	if s := screens[0]; s.Name != "" || s.X != 0 || s.Width != 0 || s.Scale != 1 {
		t.Errorf("got %+v, want an output that has said nothing complete", s)
	}
}

func TestWaylandScreensNeedARunningCompositor(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	if _, err := waylandScreens("wayland-nothing-is-listening"); err == nil {
		t.Fatal("waylandScreens succeeded against a socket nothing is on")
	}
	// A bare name with nowhere to resolve it against is a different failure,
	// and one whose message has to name the variable that is missing.
	t.Setenv("XDG_RUNTIME_DIR", "")
	if _, err := waylandScreens("wayland-0"); err == nil {
		t.Fatal("waylandScreens resolved a bare name with no XDG_RUNTIME_DIR")
	}
}
