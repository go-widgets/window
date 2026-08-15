// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build integration && linux

// The live HiDPI proof on X11, against a server whose desktop says it is a
// 192-dpi one.
//
// X11 has no compositor scaling anything, so the question is simply whether the
// window came out twice as big in pixels — which is the whole of HiDPI here. It
// is measured on the SERVER's own idea of the window's geometry rather than on
// ours, and the stripes are captured to show the pixels are really there and not
// merely allocated.
//
// It is not named TestLiveX11..., because the ordinary live lane runs against a
// server with no Xft.dpi at all and would fail this for the environment rather
// than for the code. It runs in a lane of its own and filters on TestLiveXHiDPI.
package window

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-widgets/window/internal/x11"
)

// publishXftDPI writes an X resource database naming the dpi onto the root
// window, which is where a desktop environment publishes it and where every
// toolkit reads it.
//
// The connection is held open for the rest of the test, and that is the whole
// lesson of this file. An X server RESETS when its last client disconnects,
// destroying every atom and property created since the previous reset -- so a
// tool that sets a resource and exits leaves nothing behind. xrdb was blamed
// first, then xprop; both had done exactly what they were asked, and the server
// had thrown it away between commands. A real session keeps a client alive for
// the same reason.
func publishXftDPI(t *testing.T, dpi int) {
	t.Helper()
	conn, err := dialAuthenticated("")
	if err != nil {
		t.Fatalf("dialling the server to publish the resource: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	atom, err := conn.InternAtom(resourceManagerAtom, false)
	if err != nil {
		t.Fatalf("intern %s: %v", resourceManagerAtom, err)
	}
	db := fmt.Sprintf("Xft.dpi:\t%d\n", dpi)
	root := conn.Setup().Screens[0].Root
	if err := conn.ChangeProperty(root, atom, x11.AtomString, 8, len(db), []byte(db)); err != nil {
		t.Fatalf("publishing %s: %v", resourceManagerAtom, err)
	}
	// Read it back through a round trip, so a failure here is about the property
	// and not about a request still sitting in a buffer.
	_, _, got, err := conn.GetProperty(root, atom, 0, false, 4096)
	if err != nil {
		t.Fatalf("reading %s back: %v", resourceManagerAtom, err)
	}
	if !strings.Contains(string(got), "Xft.dpi") {
		t.Fatalf("the server holds %q after publishing, want an Xft.dpi resource", got)
	}
	t.Logf("the desktop now publishes: %q", strings.TrimSpace(string(got)))

	// The same question, asked the way the back-end asks it. If these two
	// disagree the fault is in the reading and not in the desktop, and saying so
	// here costs one line and saves a round trip through CI.
	exists, err := conn.InternAtom(resourceManagerAtom, true)
	t.Logf("intern(only-if-exists) = %d (err %v)", exists, err)
	_, format, raw, err := conn.GetProperty(root, exists, 0, false, 16*1024)
	t.Logf("GetProperty: format=%d bytes=%d err=%v value=%q", format, len(raw), err, raw)
	t.Logf("desktopScale on this connection = %d", desktopScale(conn, root))
}

// xdotoolGeometry asks the SERVER how big the window is, which is a different
// route to the answer than the one under test.
func xdotoolGeometry(t *testing.T, id string) (int, int) {
	t.Helper()
	out, err := exec.Command("xdotool", "getwindowgeometry", "--shell", id).Output()
	if err != nil {
		t.Fatalf("xdotool getwindowgeometry: %v", err)
	}
	var w, h int
	for _, line := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(line), "=")
		if !ok {
			continue
		}
		n, err := strconv.Atoi(v)
		if err != nil {
			continue
		}
		switch k {
		case "WIDTH":
			w = n
		case "HEIGHT":
			h = n
		}
	}
	if w == 0 || h == 0 {
		t.Fatalf("xdotool reported no geometry for window %s: %q", id, out)
	}
	return w, h
}

// openStriped opens a window at the given render scale, waits for it to be on
// screen and returns its id.
func openStriped(t *testing.T, name string, renderScale float64) (Backend, string, *stripeRoot) {
	t.Helper()
	title := fmt.Sprintf("gw-xhidpi-%s-%d", name, os.Getpid())
	b, err := Open(Config{Title: title, Class: "gwwindow", Width: 200, Height: 150, RenderScale: renderScale})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	root := &stripeRoot{}
	go func() { _ = b.Run(root) }()
	id := waitForWindowID(t, title)
	time.Sleep(700 * time.Millisecond)
	return b, id, root
}

// A desktop at 192 dpi asks for two framebuffer pixels per point, so a window
// asked for at 200×150 points is 400×300 pixels on the server — and the one
// that did not ask is still 200×150, on the same server, in the same run.
func TestLiveXHiDPIWindowIsBiggerInPixels(t *testing.T) {
	if os.Getenv("WINDOW_X11_HIDPI") == "" {
		t.Skip("set WINDOW_X11_HIDPI=1 (under a server with Xft.dpi set) to enable")
	}
	requireTool(t, "xdotool")
	requireTool(t, "import")

	// The test publishes the desktop's dpi itself, rather than leaving it to the
	// lane. Two attempts at having xrdb and then xprop do it stored NOTHING and
	// said nothing about it, and a proof whose setup can fail silently is a
	// proof that can pass for the wrong reason. This goes through the real
	// server -- ChangeProperty on the root window, which is exactly what xrdb
	// does -- and is then read back through the code under test.
	publishXftDPI(t, 192)

	native, nativeID, _ := openStriped(t, "native", NativeScale)
	defer native.Close()

	if s, ok := native.(Scaler); !ok {
		t.Fatal("the X11 back-end does not implement Scaler")
	} else if s.RenderScale() != 2 {
		t.Fatalf("RenderScale() = %v, want 2 on a 192 dpi desktop", s.RenderScale())
	}
	if w, h := native.Size(); w != 400 || h != 300 {
		t.Errorf("Size() = %dx%d, want the 400x300 framebuffer", w, h)
	}
	// The server's own answer, which owes nothing to the code under test.
	if w, h := xdotoolGeometry(t, nativeID); w != 400 || h != 300 {
		t.Errorf("the server says the window is %dx%d, want 400x300", w, h)
	} else {
		t.Log("live X11 HiDPI: the server sees a 400x300 window for 200x150 points")
	}

	// The pixels are really there: a capture of the window shows one stripe per
	// pixel over the full 400.
	shot := filepath.Join(t.TempDir(), "native.png")
	mustRun(t, "import", "-window", nativeID, shot)
	img := decodePNG(t, shot)
	if got := img.Bounds().Dx(); got != 400 {
		t.Errorf("the captured window is %d pixels wide, want 400", got)
	}
	if got := runsAcross(t, img, img.Bounds().Dy()/2); got != 1 {
		t.Errorf("a stripe is %d pixels wide in the capture, want 1", got)
	}
	if data, err := os.ReadFile(shot); err == nil {
		_ = os.WriteFile("live-x11-hidpi-native.png", data, 0o644)
	}

	// The control, on the same server: a window that did not ask is unchanged.
	plain, plainID, _ := openStriped(t, "plain", 0)
	defer plain.Close()
	if w, h := plain.Size(); w != 200 || h != 150 {
		t.Errorf("without NativeScale Size() = %dx%d, want 200x150", w, h)
	}
	if w, h := xdotoolGeometry(t, plainID); w != 200 || h != 150 {
		t.Errorf("without NativeScale the server says %dx%d, want 200x150 -- "+
			"the measurement above then says nothing about the request", w, h)
	} else {
		t.Log("live X11 HiDPI: without the request, the same desktop gives a 200x150 window")
	}
}
