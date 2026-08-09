// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build integration && linux

// This is the live Wayland proof. It runs only under -tags=integration with
// WINDOW_WAYLAND_INTEGRATION set and a reachable Wayland compositor (a
// headless wlroots compositor — sway — in CI). It opens a real toplevel via
// the sovereign Wayland backend, presents a known four-quadrant colour
// pattern through a wl_shm buffer, captures the compositor output with grim
// and asserts the sampled pixels.
//
// It then closes the live-INPUT gap end to end: over a SECOND connection to
// the same compositor it acts as its own sovereign input source, using the
// (also sovereign) zwp_virtual_keyboard_v1 / zwlr_virtual_pointer_v1 client
// bindings to attach a real keyboard and pointer to the seat and inject a key
// and a click. Attaching those devices makes the otherwise device-less
// headless seat advertise the keyboard/pointer capabilities, so our window
// picks them up (dynamic hot-plug) and the injected events arrive through the
// ordinary wl_keyboard / wl_pointer path. The test then asserts the exact
// dispatched toolkit.Event (char 'a' and a click at the injected coordinate)
// — nothing faked, no external injector command.
//
// patternRoot, requireTool, mustRun, decodePNG, assertPixel and abs are
// shared with the X11 live test (same package + build tag).
package window

import (
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/window/internal/wayland"
)

func TestLiveWayland(t *testing.T) {
	if os.Getenv("WINDOW_WAYLAND_INTEGRATION") == "" {
		t.Skip("set WINDOW_WAYLAND_INTEGRATION=1 (under a Wayland compositor) to enable")
	}
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Fatal("WAYLAND_DISPLAY is not set")
	}
	requireTool(t, "grim")

	title := fmt.Sprintf("gwwl-live-%d", os.Getpid())
	b, err := Open(Config{Title: title, Class: "gwwltest", Width: 200, Height: 160})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, ok := b.(*wlWindow); !ok {
		t.Fatalf("Open selected %T, want the Wayland backend", b)
	}
	root := &patternRoot{}
	done := make(chan error, 1)
	go func() { done <- b.Run(root) }()

	// Let the compositor map + configure the toplevel and the client present.
	time.Sleep(1500 * time.Millisecond)

	// --- Capture and assert the presented pattern. ---------------------------
	dir := t.TempDir()
	capture := filepath.Join(dir, "capture.png")
	mustRun(t, "grim", capture)
	img := decodePNG(t, capture)
	ib := img.Bounds()
	W, H := ib.Dx(), ib.Dy()
	if W < 8 || H < 8 {
		t.Fatalf("captured image too small: %dx%d", W, H)
	}
	// The single toplevel fills the headless output; sample each quadrant.
	assertPixel(t, img, W/4, H/4, 255, 0, 0, "top-left(red)")
	assertPixel(t, img, 3*W/4, H/4, 0, 255, 0, "top-right(green)")
	assertPixel(t, img, W/4, 3*H/4, 0, 0, 255, "bottom-left(blue)")
	assertPixel(t, img, 3*W/4, 3*H/4, 255, 255, 255, "bottom-right(white)")

	// Persist the capture as a build artifact.
	if data, err := os.ReadFile(capture); err == nil {
		_ = os.WriteFile("live-wayland-capture.png", data, 0o644)
		t.Logf("saved capture to live-wayland-capture.png")
	}

	// --- Live input through a sovereign virtual keyboard + pointer. ----------
	liveWaylandInput(t, root, W, H)

	if err := b.Close(); err != nil {
		t.Logf("close: %v", err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Log("run loop did not exit promptly after close")
	}
}

// liveWaylandInput opens a second connection to the compositor and drives real
// input into our window with the sovereign virtual-keyboard / virtual-pointer
// bindings, then asserts the dispatched toolkit events. If the compositor does
// not advertise those manager globals, the input assertion is skipped honestly
// (the input path stays proven by the deterministic fake-compositor test) —
// but on the CI headless wlroots compositor the managers are present and the
// assertion is enforced.
func liveWaylandInput(t *testing.T, root *patternRoot, outW, outH int) {
	inj, err := dialCompositor()
	if err != nil {
		t.Fatalf("dial compositor (injector): %v", err)
	}
	defer inj.Close()

	reg, err := inj.Display().GetRegistry()
	if err != nil {
		t.Fatalf("injector get_registry: %v", err)
	}
	if err := inj.Roundtrip(); err != nil {
		t.Fatalf("injector roundtrip: %v", err)
	}
	if _, ok := reg.Find("zwp_virtual_keyboard_manager_v1"); !ok {
		t.Log("live input: compositor has no zwp_virtual_keyboard_manager_v1; " +
			"input proven by the fake-compositor test (pending-on-compositor)")
		return
	}
	if _, ok := reg.Find("zwlr_virtual_pointer_manager_v1"); !ok {
		t.Log("live input: compositor has no zwlr_virtual_pointer_manager_v1; " +
			"input proven by the fake-compositor test (pending-on-compositor)")
		return
	}

	seat, err := reg.Seat()
	if err != nil {
		t.Fatalf("injector seat: %v", err)
	}
	vkm, err := reg.VirtualKeyboardManager()
	if err != nil {
		t.Fatalf("virtual keyboard manager: %v", err)
	}
	vpm, err := reg.VirtualPointerManager()
	if err != nil {
		t.Fatalf("virtual pointer manager: %v", err)
	}
	if err := inj.Roundtrip(); err != nil {
		t.Fatalf("injector roundtrip: %v", err)
	}

	// Attach a keyboard with a real (libxkbcommon-compiled) us keymap so the
	// compositor forwards a keymap our sovereign parser resolves, and a
	// pointer. Both stay alive for the whole assertion window, avoiding the
	// transient-device race that left the earlier attempt pending.
	kbd, err := vkm.CreateKeyboard(seat)
	if err != nil {
		t.Fatalf("create virtual keyboard: %v", err)
	}
	defer kbd.Destroy()
	fd, size := usKeymapFD(t)
	if err := kbd.Keymap(wayland.KeymapFormatXkbV1, fd, size); err != nil {
		t.Fatalf("upload keymap: %v", err)
	}
	ptr, err := vpm.CreatePointer(seat)
	if err != nil {
		t.Fatalf("create virtual pointer: %v", err)
	}
	defer ptr.Destroy()
	if err := inj.Roundtrip(); err != nil {
		t.Fatalf("injector roundtrip after device setup: %v", err)
	}

	const evKeyA = 30 // Linux KEY_A (evdev keycode)
	const clickX, clickY = 200, 150

	var gotClick, gotChar bool
	var cx, cy int
	tms := uint32(1)
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) && (!gotClick || !gotChar) {
		// Inject a key press+release. Repeated until our window has bound its
		// keyboard and received focus (the device is persistent, so a key sent
		// slightly early is simply superseded by the next iteration's key).
		_ = kbd.Key(tms, evKeyA, wayland.StatePressed)
		_ = kbd.Key(tms+1, evKeyA, wayland.StateReleased)
		// Move the pointer to an interior point and click.
		_ = ptr.MotionAbsolute(tms+2, clickX, clickY, uint32(outW), uint32(outH))
		_ = ptr.Frame()
		_ = ptr.Button(tms+3, wayland.BtnLeft, wayland.StatePressed)
		_ = ptr.Frame()
		_ = ptr.Button(tms+4, wayland.BtnLeft, wayland.StateReleased)
		_ = ptr.Frame()
		tms += 10
		if err := inj.Roundtrip(); err != nil {
			t.Fatalf("injector roundtrip during injection: %v", err)
		}
		for _, ev := range root.snapshot() {
			switch {
			case ev.Kind == toolkit.EventClick:
				gotClick, cx, cy = true, ev.X, ev.Y
			case ev.Kind == toolkit.EventChar && ev.Code == "a":
				gotChar = true
			}
		}
		time.Sleep(150 * time.Millisecond)
	}

	if !gotChar {
		t.Errorf("no EventChar 'a' dispatched from an injected key; events=%+v", root.snapshot())
	} else {
		t.Log("live input: EventChar 'a' dispatched from a real compositor key")
	}
	if !gotClick {
		t.Errorf("no EventClick dispatched from an injected button; events=%+v", root.snapshot())
	} else if abs(cx-clickX) > 4 || abs(cy-clickY) > 4 {
		t.Errorf("click coords = (%d,%d) want ~(%d,%d)", cx, cy, clickX, clickY)
	} else {
		t.Logf("live input: EventClick dispatched at (%d,%d) from a real compositor button", cx, cy)
	}
}

// dialCompositor opens a fresh sovereign connection to $WAYLAND_DISPLAY
// (resolved against $XDG_RUNTIME_DIR), the same way the window backend does.
func dialCompositor() (*wayland.Conn, error) {
	name := os.Getenv("WAYLAND_DISPLAY")
	path := name
	if !filepath.IsAbs(name) {
		path = filepath.Join(os.Getenv("XDG_RUNTIME_DIR"), name)
	}
	nc, err := net.Dial("unix", path)
	if err != nil {
		return nil, err
	}
	return wayland.New(nc.(*net.UnixConn)), nil
}

// usKeymapFD compiles the us xkb layout with libxkbcommon's xkbcli, writes it
// (NUL-terminated) to a temp file and returns the descriptor plus mapped size
// for wl/virtual_keyboard.keymap. libxkbcommon compiles the same text on the
// compositor side, so the keymap our window receives resolves KEY_A to 'a'.
func usKeymapFD(t *testing.T) (int, uint32) {
	t.Helper()
	requireTool(t, "xkbcli")
	out, err := exec.Command("xkbcli", "compile-keymap", "--layout", "us").Output()
	if err != nil {
		t.Fatalf("xkbcli compile-keymap: %v", err)
	}
	blob := append(out, 0) // NUL-terminate; the mapped size includes it
	f, err := os.CreateTemp(t.TempDir(), "gw-vkbd-keymap")
	if err != nil {
		t.Fatalf("keymap temp: %v", err)
	}
	t.Cleanup(func() { f.Close() })
	if _, err := f.Write(blob); err != nil {
		t.Fatalf("keymap write: %v", err)
	}
	return int(f.Fd()), uint32(len(blob))
}
