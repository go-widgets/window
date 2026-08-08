// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build integration && linux

// This is the live X11 proof. It runs only under -tags=integration with
// WINDOW_X11_INTEGRATION set and a reachable X server (Xvfb in CI). It opens
// a real window, presents a known four-quadrant colour pattern, captures the
// window with ImageMagick's `import`, and asserts the sampled pixels; then it
// synthesises a pointer click and a key press with xdotool and asserts the
// toolkit.Event actually dispatched into the widget tree.
package window

import (
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// patternRoot paints four solid quadrants (TL red, TR green, BL blue, BR
// white) and records every event delivered to it.
type patternRoot struct {
	toolkit.Base
	mu     sync.Mutex
	events []toolkit.Event
}

func (r *patternRoot) Draw(p painter.Painter, _ *toolkit.Theme) {
	b := r.Bounds()
	hw, hh := b.W/2, b.H/2
	p.FillRect(painter.Rect{X: b.X, Y: b.Y, W: hw, H: hh}, painter.RGBA{R: 255, A: 255})
	p.FillRect(painter.Rect{X: b.X + hw, Y: b.Y, W: b.W - hw, H: hh}, painter.RGBA{G: 255, A: 255})
	p.FillRect(painter.Rect{X: b.X, Y: b.Y + hh, W: hw, H: b.H - hh}, painter.RGBA{B: 255, A: 255})
	p.FillRect(painter.Rect{X: b.X + hw, Y: b.Y + hh, W: b.W - hw, H: b.H - hh}, painter.RGBA{R: 255, G: 255, B: 255, A: 255})
}

func (r *patternRoot) OnEvent(ev toolkit.Event) {
	r.mu.Lock()
	r.events = append(r.events, ev)
	r.mu.Unlock()
}

func (r *patternRoot) snapshot() []toolkit.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]toolkit.Event(nil), r.events...)
}

func requireTool(t *testing.T, name string) {
	t.Helper()
	if _, err := exec.LookPath(name); err != nil {
		t.Fatalf("required tool %q not found: %v", name, err)
	}
}

func mustRun(t *testing.T, name string, args ...string) string {
	t.Helper()
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func waitForWindowID(t *testing.T, title string) string {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		out, err := exec.Command("xdotool", "search", "--name", title).CombinedOutput()
		if err == nil {
			if id := strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0]); id != "" {
				return id
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	t.Fatalf("window %q never appeared", title)
	return ""
}

func decodePNG(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open capture: %v", err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decode capture: %v", err)
	}
	return img
}

func assertPixel(t *testing.T, img image.Image, x, y int, wr, wg, wb uint8, label string) {
	t.Helper()
	r, g, b, _ := img.At(x, y).RGBA()
	gr, gg, gb := uint8(r>>8), uint8(g>>8), uint8(b>>8)
	if gr != wr || gg != wg || gb != wb {
		t.Errorf("%s pixel (%d,%d) = (%d,%d,%d) want (%d,%d,%d)", label, x, y, gr, gg, gb, wr, wg, wb)
	}
}

func TestLiveX11(t *testing.T) {
	if os.Getenv("WINDOW_X11_INTEGRATION") == "" {
		t.Skip("set WINDOW_X11_INTEGRATION=1 (and run under an X server) to enable")
	}
	requireTool(t, "xdotool")
	requireTool(t, "import")

	const W, H = 200, 160
	title := fmt.Sprintf("gwwindow-live-%d", os.Getpid())

	w, err := Open(Config{Title: title, Class: "gwwindow", Width: W, Height: H})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	root := &patternRoot{}
	done := make(chan error, 1)
	go func() { done <- w.Run(root) }()

	id := waitForWindowID(t, title)
	// Give the server time to process the initial map + PutImage.
	time.Sleep(700 * time.Millisecond)

	// --- Capture and assert the presented pattern. ---------------------------
	dir := t.TempDir()
	capture := filepath.Join(dir, "capture.png")
	mustRun(t, "import", "-window", id, capture)
	img := decodePNG(t, capture)

	// Sample the centre of each quadrant.
	assertPixel(t, img, W/4, H/4, 255, 0, 0, "top-left(red)")
	assertPixel(t, img, 3*W/4, H/4, 0, 255, 0, "top-right(green)")
	assertPixel(t, img, W/4, 3*H/4, 0, 0, 255, "bottom-left(blue)")
	assertPixel(t, img, 3*W/4, 3*H/4, 255, 255, 255, "bottom-right(white)")

	// Persist the capture as a build artifact.
	saved := "live-capture.png"
	if data, err := os.ReadFile(capture); err == nil {
		_ = os.WriteFile(saved, data, 0o644)
		t.Logf("saved capture to %s", saved)
	}

	// --- Synthesise input and assert the dispatched toolkit events. ----------
	const clickX, clickY = 150, 120 // inside the bottom-right quadrant
	mustRun(t, "xdotool", "mousemove", "--window", id, fmt.Sprint(clickX), fmt.Sprint(clickY),
		"click", "--window", id, "1")
	mustRun(t, "xdotool", "key", "--window", id, "a")

	// Wait for the events to be dispatched.
	var gotClick, gotChar bool
	var cx, cy int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		for _, ev := range root.snapshot() {
			switch {
			case ev.Kind == toolkit.EventClick:
				gotClick, cx, cy = true, ev.X, ev.Y
			case ev.Kind == toolkit.EventChar && ev.Code == "a":
				gotChar = true
			}
		}
		if gotClick && gotChar {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}

	if !gotClick {
		t.Errorf("no EventClick dispatched; events=%+v", root.snapshot())
	} else if abs(cx-clickX) > 3 || abs(cy-clickY) > 3 {
		t.Errorf("click coords = (%d,%d) want ~(%d,%d)", cx, cy, clickX, clickY)
	}
	if !gotChar {
		t.Errorf("no EventChar 'a' dispatched; events=%+v", root.snapshot())
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

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
