// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux && !android

package gtk

import (
	"os/exec"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/go-gtk/gtk4"
	"github.com/go-widgets/toolkit"
)

// TestLiveDeadKeyComposesInAPaintedField is the on-device proof of the input
// method path: a real GTK window with one focused painted Entry, and real key
// events injected by the X server (xdotool, XTEST) — a dead circumflex, then e.
// GtkIMContext composes them, the window shows the dead key as a preedit and then
// commits ê into the Entry; a following plain x lands as x. It needs Xvfb (or any
// X display) and xdotool, and skips without them.
//
// What it does not prove: a candidate window of a real input method (ibus,
// fcitx), which needs a running daemon and a CJK engine; the compose path it
// exercises is GTK's own GtkIMContextSimple, which is what GTK falls back to when
// the session selects no input method.
func TestLiveDeadKeyComposesInAPaintedField(t *testing.T) {
	if _, err := exec.LookPath("xdotool"); err != nil {
		t.Skip("xdotool not installed")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	const title = "go-widgets ime proof"
	win, err := Open(title, 320, 160, nil, 1)
	if err != nil {
		t.Skipf("no GTK display: %v", err)
	}
	entry := toolkit.NewEntry("")
	entry.SetFocused(true)
	vbox := toolkit.NewVBox()
	vbox.Append(entry)

	type snapshot struct{ value, preedit string }
	// snap reads the entry on the GTK thread (an idle callback), the only thread
	// the widget tree belongs to, and hands the answer back.
	snap := func() snapshot {
		ch := make(chan snapshot, 1)
		gtk4.IdleAdd(func() { ch <- snapshot{entry.Value(), entry.Preedit().Text} })
		select {
		case s := <-ch:
			return s
		case <-time.After(5 * time.Second):
			return snapshot{"<timeout>", "<timeout>"}
		}
	}

	type result struct {
		afterDead, afterVowel, afterPlain snapshot
		err                               string
	}
	done := make(chan result, 1)
	go func() {
		var r result
		defer func() {
			gtk4.IdleAdd(func() { win.Close() })
			done <- r
		}()
		id := ""
		for i := 0; i < 100 && id == ""; i++ {
			out, _ := exec.Command("xdotool", "search", "--onlyvisible", "--name", title).Output()
			id = strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
			time.Sleep(100 * time.Millisecond)
		}
		if id == "" {
			r.err = "the window never became visible to the X server"
			return
		}
		time.Sleep(500 * time.Millisecond) // first frame, key controller focused
		if out, err := exec.Command("xdotool", "windowfocus", "--sync", id).CombinedOutput(); err != nil {
			r.err = "windowfocus: " + err.Error() + ": " + string(out)
			return
		}
		key := func(k string) bool {
			if out, err := exec.Command("xdotool", "key", "--delay", "50", k).CombinedOutput(); err != nil {
				r.err = "key " + k + ": " + err.Error() + ": " + string(out)
				return false
			}
			time.Sleep(300 * time.Millisecond)
			return true
		}
		if !key("dead_circumflex") {
			return
		}
		r.afterDead = snap()
		if !key("e") {
			return
		}
		r.afterVowel = snap()
		if !key("x") {
			return
		}
		r.afterPlain = snap()
	}()

	if err := win.Run(vbox); err != nil {
		t.Fatalf("Run: %v", err)
	}
	r := <-done
	if r.err != "" {
		t.Fatal(r.err)
	}
	t.Logf("after the dead key: %+v; after e: %+v; after x: %+v", r.afterDead, r.afterVowel, r.afterPlain)
	if r.afterDead.value != "" {
		t.Errorf("the dead key alone put %q in the field, want nothing", r.afterDead.value)
	}
	if r.afterDead.preedit == "" {
		t.Error("the dead key showed no preedit")
	}
	if r.afterVowel.value != "ê" || r.afterVowel.preedit != "" {
		t.Errorf("after dead key + e: value %q preedit %q, want ê and no preedit", r.afterVowel.value, r.afterVowel.preedit)
	}
	if r.afterPlain.value != "êx" {
		t.Errorf("after a plain x: value %q, want êx", r.afterPlain.value)
	}
}
