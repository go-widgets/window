// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build integration && linux

package window

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// solidRoot fills its bounds with one colour.
type solidRoot struct {
	toolkit.Base
	c painter.RGBA
}

func (r *solidRoot) Draw(p painter.Painter, _ *toolkit.Theme) { p.FillRect(r.Bounds(), r.c) }

// visibleIDs is what the X server says is viewable under that name, which is
// what a person looking at the screen would say too.
func visibleIDs(title string) string {
	out, _ := exec.Command("xdotool", "search", "--onlyvisible", "--name", title).Output()
	return strings.TrimSpace(string(out))
}

func waitVisible(t *testing.T, title string, want bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if (visibleIDs(title) != "") == want {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("window %q visible = %v after 5s, want %v", title, !want, want)
}

var hexColour = regexp.MustCompile(`#([0-9A-Fa-f]{6})\b`)

// rootPixel samples one pixel of the whole screen, to stdout: nothing is
// written to disk. A pixel of the root is the window that is ON TOP there.
func rootPixel(t *testing.T, x, y int) string {
	t.Helper()
	out, err := exec.Command("import", "-window", "root", "-depth", "8", "-crop", fmt.Sprintf("1x1+%d+%d", x, y), "txt:-").CombinedOutput()
	if err != nil {
		t.Fatalf("import: %v\n%s", err, out)
	}
	// The first line is a "# ImageMagick pixel enumeration" header; the pixel
	// line carries the colour as #RRGGBB.
	if m := hexColour.FindStringSubmatch(string(out)); m != nil {
		return strings.ToUpper(m[1])
	}
	t.Fatalf("no colour in %q", out)
	return ""
}

// TestLiveX11Visibility (go-widgets/window#134): on a real X server, Hide takes
// the window off the screen while its Run carries on, Show puts it back, and
// Raise brings it in front of a window that covers it.
func TestLiveX11Visibility(t *testing.T) {
	if os.Getenv("WINDOW_X11_INTEGRATION") == "" {
		t.Skip("set WINDOW_X11_INTEGRATION=1 (and run under an X server) to enable")
	}
	requireTool(t, "xdotool")
	requireTool(t, "import")

	open := func(name string, c painter.RGBA) (Backend, chan error) {
		w, err := Open(Config{Title: name, Width: 120, Height: 90})
		if err != nil {
			t.Fatalf("Open %s: %v", name, err)
		}
		done := make(chan error, 1)
		go func() { done <- w.Run(&solidRoot{c: c}) }()
		waitVisible(t, name, true)
		return w, done
	}
	pid := os.Getpid()
	redName, blueName := fmt.Sprintf("gw-vis-red-%d", pid), fmt.Sprintf("gw-vis-blue-%d", pid)
	red, redDone := open(redName, painter.RGBA{R: 255, A: 255})
	defer red.Close()

	if err := Hide(red); err != nil {
		t.Fatalf("Hide: %v", err)
	}
	waitVisible(t, redName, false)
	select {
	case err := <-redDone:
		t.Fatalf("Hide ended Run (%v); hiding is not closing", err)
	case <-time.After(300 * time.Millisecond):
	}
	if err := Show(red); err != nil {
		t.Fatalf("Show: %v", err)
	}
	waitVisible(t, redName, true)

	// A second window, opened after the first and so on top of it.
	blue, blueDone := open(blueName, painter.RGBA{B: 255, A: 255})
	defer blue.Close()
	// Put both at the same place, so one covers the other wherever a window
	// manager first placed them; then the pixel in the middle of that place is
	// the window on top.
	for _, name := range []string{redName, blueName} {
		mustRun(t, "xdotool", "windowmove", strings.Fields(visibleIDs(name))[0], "100", "100")
	}
	time.Sleep(300 * time.Millisecond) // the move and the first PutImage
	sample := func() string { return rootPixel(t, 160, 145) }
	waitColour := func(want, what string) {
		t.Helper()
		var got string
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			if got = sample(); got == want {
				return
			}
			time.Sleep(100 * time.Millisecond)
		}
		t.Fatalf("%s: the screen shows #%s at (160,145), want #%s", what, got, want)
	}
	waitColour("0000FF", "the window opened last")
	if err := Raise(red); err != nil {
		t.Fatalf("Raise: %v", err)
	}
	waitColour("FF0000", "after Raise(red)")
	if err := Raise(blue); err != nil {
		t.Fatalf("Raise: %v", err)
	}
	waitColour("0000FF", "after Raise(blue)")

	// Raise brings back a hidden window too.
	if err := Hide(red); err != nil {
		t.Fatal(err)
	}
	waitVisible(t, redName, false)
	if err := Raise(red); err != nil {
		t.Fatal(err)
	}
	// Not sampled: a window manager places a window it is given back as it
	// places a new one (openbox puts it somewhere else), so where it went is
	// its business; that it is back is ours.
	waitVisible(t, redName, true)

	_ = red.Close()
	_ = blue.Close()
	for _, d := range []chan error{redDone, blueDone} {
		select {
		case <-d:
		case <-time.After(2 * time.Second):
			t.Error("a run loop did not return after Close")
		}
	}
}
