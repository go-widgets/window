// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build integration && linux

// Live proof for the damage-aware present path on real display servers (Xvfb
// for X11, headless sway for Wayland). A scene.HostRoot drives a four-quadrant
// toggle pattern; a single injected click flips ONE quadrant, which the backend
// presents incrementally (only that quadrant's rectangle). The captured output
// must then show the clicked quadrant in its NEW colour and every OTHER quadrant
// UNCHANGED — the end-to-end pixel-correctness guarantee of incremental present
// (a dropped or corrupted pixel outside the damage would fail this).
//
// These reuse requireTool, mustRun, decodePNG, assertPixel, abs and
// waitForWindowID (X11) plus dialCompositor and the virtual-pointer bindings
// (Wayland) from the sibling live tests, and toggleCell/wholeBox from
// damage_test.go.
package window

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/toolkit/scene"
	"github.com/go-widgets/window/internal/wayland"
)

// quad colours: the initial colour and the colour a quadrant flips to on click.
var (
	quadCol = [4]toolkit.RGBA{
		toolkit.RGB(255, 0, 0),     // TL red
		toolkit.RGB(0, 255, 0),     // TR green
		toolkit.RGB(0, 0, 255),     // BL blue
		toolkit.RGB(255, 255, 255), // BR white
	}
	quadAlt = [4]toolkit.RGBA{
		toolkit.RGB(255, 255, 0), // TL -> yellow
		toolkit.RGB(0, 255, 255), // TR -> cyan
		toolkit.RGB(255, 0, 255), // BL -> magenta
		toolkit.RGB(20, 20, 20),  // BR -> near-black
	}
)

// quadBox lays its four children (TL, TR, BL, BR) into the quadrants of its
// bounds on every SetBounds, so the pattern fills whatever size the compositor
// configures the toplevel to (headless sway resizes it to the 800x600 output).
type quadBox struct {
	toolkit.Base
	kids []toolkit.Widget
}

func (b *quadBox) Children() []toolkit.Widget { return b.kids }
func (b *quadBox) Draw(p painter.Painter, th *toolkit.Theme) {
	for _, k := range b.kids {
		k.Draw(p, th)
	}
}
func (b *quadBox) OnEvent(ev toolkit.Event) {
	for _, k := range b.kids {
		if k.HitTest(ev.X, ev.Y) {
			k.OnEvent(ev)
		}
	}
}
func (b *quadBox) SetBounds(r toolkit.Rect) {
	b.Base.SetBounds(r)
	hw, hh := r.W/2, r.H/2
	b.kids[0].SetBounds(toolkit.Rect{X: r.X, Y: r.Y, W: hw, H: hh})
	b.kids[1].SetBounds(toolkit.Rect{X: r.X + hw, Y: r.Y, W: r.W - hw, H: hh})
	b.kids[2].SetBounds(toolkit.Rect{X: r.X, Y: r.Y + hh, W: hw, H: r.H - hh})
	b.kids[3].SetBounds(toolkit.Rect{X: r.X + hw, Y: r.Y + hh, W: r.W - hw, H: r.H - hh})
}

// buildQuadApp builds a four-quadrant toggle pattern over a WxH surface and its
// scene-backed root. cells are indexed TL, TR, BL, BR and re-flow on resize.
func buildQuadApp(W, H int) (*scene.HostRoot, []*toggleCell) {
	var cells []*toggleCell
	var kids []toolkit.Widget
	for i := 0; i < 4; i++ {
		c := &toggleCell{col: quadCol[i], alt: quadAlt[i]}
		cells = append(cells, c)
		kids = append(kids, c)
	}
	app := &quadBox{kids: kids}
	app.SetBounds(toolkit.Rect{X: 0, Y: 0, W: W, H: H})
	hr := scene.NewHostRoot(app)
	for _, c := range cells {
		c.hr = hr
	}
	return hr, cells
}

func TestLiveX11Damage(t *testing.T) {
	if os.Getenv("WINDOW_X11_INTEGRATION") == "" {
		t.Skip("set WINDOW_X11_INTEGRATION=1 (and run under an X server) to enable")
	}
	requireTool(t, "xdotool")
	requireTool(t, "import")

	const W, H = 200, 160
	title := fmt.Sprintf("gwdmg-x11-%d", os.Getpid())
	hr, _ := buildQuadApp(W, H)

	w, err := Open(Config{Title: title, Class: "gwdmg", Width: W, Height: H})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- w.Run(hr) }()

	id := waitForWindowID(t, title)
	time.Sleep(700 * time.Millisecond)

	dir := t.TempDir()
	cap0 := filepath.Join(dir, "before.png")
	mustRun(t, "import", "-window", id, cap0)
	img0 := decodePNG(t, cap0)
	assertPixel(t, img0, W/4, H/4, 255, 0, 0, "TL before(red)")
	assertPixel(t, img0, 3*W/4, H/4, 0, 255, 0, "TR before(green)")
	assertPixel(t, img0, W/4, 3*H/4, 0, 0, 255, "BL before(blue)")
	assertPixel(t, img0, 3*W/4, 3*H/4, 255, 255, 255, "BR before(white)")

	// Click the bottom-right quadrant: it flips to near-black and is presented
	// incrementally (only that quadrant's rectangle).
	mustRun(t, "xdotool", "mousemove", "--window", id, fmt.Sprint(3*W/4), fmt.Sprint(3*H/4),
		"click", "--window", id, "1")
	time.Sleep(500 * time.Millisecond)

	cap1 := filepath.Join(dir, "after.png")
	mustRun(t, "import", "-window", id, cap1)
	img1 := decodePNG(t, cap1)
	// The clicked quadrant shows the NEW colour...
	assertPixel(t, img1, 3*W/4, 3*H/4, 20, 20, 20, "BR after(near-black)")
	// ...and every OTHER quadrant is UNCHANGED (incremental present did not
	// corrupt or drop pixels outside the damage).
	assertPixel(t, img1, W/4, H/4, 255, 0, 0, "TL after(red)")
	assertPixel(t, img1, 3*W/4, H/4, 0, 255, 0, "TR after(green)")
	assertPixel(t, img1, W/4, 3*H/4, 0, 0, 255, "BL after(blue)")

	if data, err := os.ReadFile(cap1); err == nil {
		_ = os.WriteFile("live-x11-damage.png", data, 0o644)
	}

	if err := w.Close(); err != nil {
		t.Logf("close: %v", err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Log("run loop did not exit promptly after close")
	}
}

func TestLiveWaylandDamage(t *testing.T) {
	if os.Getenv("WINDOW_WAYLAND_INTEGRATION") == "" {
		t.Skip("set WINDOW_WAYLAND_INTEGRATION=1 (under a Wayland compositor) to enable")
	}
	if os.Getenv("WAYLAND_DISPLAY") == "" {
		t.Fatal("WAYLAND_DISPLAY is not set")
	}
	requireTool(t, "grim")

	title := fmt.Sprintf("gwdmg-wl-%d", os.Getpid())
	hr, _ := buildQuadApp(200, 160)
	b, err := Open(Config{Title: title, Class: "gwdmgwl", Width: 200, Height: 160})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if _, ok := b.(*wlWindow); !ok {
		t.Fatalf("Open selected %T, want the Wayland backend", b)
	}
	done := make(chan error, 1)
	go func() { done <- b.Run(hr) }()
	time.Sleep(1500 * time.Millisecond)

	dir := t.TempDir()
	cap0 := filepath.Join(dir, "before.png")
	mustRun(t, "grim", cap0)
	img0 := decodePNG(t, cap0)
	ib := img0.Bounds()
	W, H := ib.Dx(), ib.Dy()
	if W < 8 || H < 8 {
		t.Fatalf("captured image too small: %dx%d", W, H)
	}
	assertPixel(t, img0, W/4, H/4, 255, 0, 0, "TL before(red)")
	assertPixel(t, img0, 3*W/4, H/4, 0, 255, 0, "TR before(green)")
	assertPixel(t, img0, W/4, 3*H/4, 0, 0, 255, "BL before(blue)")
	assertPixel(t, img0, 3*W/4, 3*H/4, 255, 255, 255, "BR before(white)")

	// Inject a click into the bottom-right quadrant over a second connection.
	if !injectWaylandClick(t, 3*W/4, 3*H/4, W, H) {
		t.Skip("compositor lacks virtual-pointer manager; incremental correctness " +
			"proven by the fake-compositor buffer-age test (pending-on-compositor)")
	}
	time.Sleep(600 * time.Millisecond)

	cap1 := filepath.Join(dir, "after.png")
	mustRun(t, "grim", cap1)
	img1 := decodePNG(t, cap1)
	assertPixel(t, img1, 3*W/4, 3*H/4, 20, 20, 20, "BR after(near-black)")
	assertPixel(t, img1, W/4, H/4, 255, 0, 0, "TL after(red)")
	assertPixel(t, img1, 3*W/4, H/4, 0, 255, 0, "TR after(green)")
	assertPixel(t, img1, W/4, 3*H/4, 0, 0, 255, "BL after(blue)")

	if data, err := os.ReadFile(cap1); err == nil {
		_ = os.WriteFile("live-wayland-damage.png", data, 0o644)
	}

	if err := b.Close(); err != nil {
		t.Logf("close: %v", err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Log("run loop did not exit promptly after close")
	}
}

// injectWaylandClick attaches a sovereign virtual pointer to the seat and clicks
// at (x, y) within an outW×outH output. It returns false when the compositor
// advertises no virtual-pointer manager (so the caller can skip honestly).
func injectWaylandClick(t *testing.T, x, y, outW, outH int) bool {
	t.Helper()
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
	if _, ok := reg.Find("zwlr_virtual_pointer_manager_v1"); !ok {
		return false
	}
	seat, err := reg.Seat()
	if err != nil {
		t.Fatalf("injector seat: %v", err)
	}
	vpm, err := reg.VirtualPointerManager()
	if err != nil {
		t.Fatalf("virtual pointer manager: %v", err)
	}
	if err := inj.Roundtrip(); err != nil {
		t.Fatalf("injector roundtrip: %v", err)
	}
	ptr, err := vpm.CreatePointer(seat)
	if err != nil {
		t.Fatalf("create virtual pointer: %v", err)
	}
	defer ptr.Destroy()
	if err := inj.Roundtrip(); err != nil {
		t.Fatalf("injector roundtrip after pointer setup: %v", err)
	}

	tms := uint32(1)
	deadline := time.Now().Add(6 * time.Second)
	for time.Now().Before(deadline) {
		_ = ptr.MotionAbsolute(tms, uint32(x), uint32(y), uint32(outW), uint32(outH))
		_ = ptr.Frame()
		_ = ptr.Button(tms+1, wayland.BtnLeft, wayland.StatePressed)
		_ = ptr.Frame()
		_ = ptr.Button(tms+2, wayland.BtnLeft, wayland.StateReleased)
		_ = ptr.Frame()
		tms += 10
		if err := inj.Roundtrip(); err != nil {
			t.Fatalf("injector roundtrip during click: %v", err)
		}
		time.Sleep(200 * time.Millisecond)
	}
	return true
}
