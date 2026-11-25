// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin && integration

package cocoa

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// These tests open the REAL NSOpenPanel, NSSavePanel and NSAlert and dismiss
// them the way a person would, through System Events (accessibility must be
// granted to the terminal running the tests). The answer asserted is the one
// the panel hands back to Go -- not a screenshot.

// drive runs an AppleScript against this process after delay, reporting any
// failure to the test.
func drive(t *testing.T, delay time.Duration, body string) {
	t.Helper()
	go func() {
		time.Sleep(delay)
		script := fmt.Sprintf(`tell application "System Events"
	set p to first process whose unix id is %d
	set frontmost of p to true
	delay 0.5
	%s
end tell`, os.Getpid(), body)
		out, err := exec.Command("osascript", "-e", script).CombinedOutput()
		fmt.Fprintf(os.Stderr, "drive: err=%v out=%s\n", err, out)
		if err != nil {
			t.Errorf("System Events: %v: %s", err, out)
		}
	}()
}

// await turns the main run loop (where the dialog runs) until f's channel
// answers.
func await[T any](t *testing.T, ch <-chan T) T {
	t.Helper()
	deadline := time.After(40 * time.Second)
	for {
		select {
		case v := <-ch:
			return v
		case <-deadline:
			t.Fatal("the dialog never answered")
		default:
			callOnMain(func() { spin(0.1) })
		}
	}
}

func liveDialogWindow(t *testing.T) *Window {
	t.Helper()
	var win *Window
	callOnMain(func() {
		var err error
		if win, err = NewScaled("dialogs", 240, 160, nil, 0); err != nil {
			t.Fatalf("open: %v", err)
		}
		spin(0.2)
	})
	t.Cleanup(func() { callOnMain(func() { _ = win.Close(); spin(0.1) }) })
	return win
}

func sameFile(t *testing.T, got, want string) {
	t.Helper()
	g, err1 := filepath.EvalSymlinks(got)
	w, err2 := filepath.EvalSymlinks(want)
	if err1 != nil || err2 != nil || g != w {
		t.Errorf("panel returned %q, want %q", got, want)
	}
}

func TestLiveOpenPanelReturnsTheTypedPath(t *testing.T) {
	skipUnlessIntegration(t)
	win := liveDialogWindow(t)
	f := filepath.Join(t.TempDir(), "chosen.txt")
	if err := os.WriteFile(f, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan DialogResult, 1)
	if err := win.Dialog(DialogRequest{Kind: DialogOpen, Title: "pick", Exts: []string{"txt"}, Done: func(r DialogResult) { done <- r }}); err != nil {
		t.Fatal(err)
	}
	// Cmd-Shift-G is "Go to the folder"; a file path typed there selects the
	// file, and Return opens it.
	drive(t, 2*time.Second, fmt.Sprintf(`keystroke "g" using {command down, shift down}
	delay 1
	keystroke %q
	delay 1
	key code 36
	delay 1
	key code 36`, f))
	r := await(t, done)
	if r.Cancelled || len(r.Paths) != 1 {
		t.Fatalf("result = %+v", r)
	}
	sameFile(t, r.Paths[0], f)
}

func TestLiveOpenPanelCancel(t *testing.T) {
	skipUnlessIntegration(t)
	win := liveDialogWindow(t)
	done := make(chan DialogResult, 1)
	if err := win.Dialog(DialogRequest{Kind: DialogOpen, Directories: true, Done: func(r DialogResult) { done <- r }}); err != nil {
		t.Fatal(err)
	}
	drive(t, 2*time.Second, `key code 53`) // Escape
	if r := await(t, done); !r.Cancelled || len(r.Paths) != 0 {
		t.Fatalf("result = %+v, want cancelled", r)
	}
}

func TestLiveSavePanelReturnsTheSuggestedName(t *testing.T) {
	skipUnlessIntegration(t)
	win := liveDialogWindow(t)
	done := make(chan DialogResult, 1)
	if err := win.Dialog(DialogRequest{Kind: DialogSave, Name: "report-dialogs-test.txt", Exts: []string{"txt"}, Done: func(r DialogResult) { done <- r }}); err != nil {
		t.Fatal(err)
	}
	drive(t, 2*time.Second, `key code 36`) // Return accepts the suggested name
	r := await(t, done)
	if r.Cancelled || len(r.Paths) != 1 || filepath.Base(r.Paths[0]) != "report-dialogs-test.txt" {
		t.Fatalf("result = %+v", r)
	}
	if _, err := os.Stat(r.Paths[0]); err == nil {
		t.Errorf("the save panel must not create %s", r.Paths[0])
	}
}

func TestLiveAlertReturnsThePressedButton(t *testing.T) {
	skipUnlessIntegration(t)
	win := liveDialogWindow(t)
	// This process is not exposed through accessibility (no windows are
	// listed), so the buttons are reached by AppKit's own key equivalents:
	// Return is the default (first) button, Escape the one titled "Cancel",
	// and Command-D the one titled "Don't Save".
	for _, tc := range []struct {
		keys string
		want int
	}{
		{`key code 36`, 0},                        // Return
		{`key code 53`, 1},                        // Escape
		{`keystroke "d" using {command down}`, 2}, // Command-D
	} {
		done := make(chan DialogResult, 1)
		if err := win.Dialog(DialogRequest{Kind: DialogAlert, Message: "Message", Detail: "Detail",
			Buttons: []string{"Save", "Cancel", "Don't Save"}, Done: func(r DialogResult) { done <- r }}); err != nil {
			t.Fatal(err)
		}
		drive(t, 2*time.Second, tc.keys)
		if r := await(t, done); r.Cancelled || r.Button != tc.want {
			t.Errorf("%s: result = %+v, want button %d", tc.keys, r, tc.want)
		}
	}
}

func TestLiveDialogAfterCloseIsRefused(t *testing.T) {
	skipUnlessIntegration(t)
	var win *Window
	callOnMain(func() {
		var err error
		if win, err = NewScaled("dialogs-closed", 240, 160, nil, 0); err != nil {
			t.Fatal(err)
		}
		_ = win.Close()
		spin(0.1)
	})
	if err := win.Dialog(DialogRequest{Kind: DialogAlert, Message: "m", Done: func(DialogResult) { t.Error("called") }}); err != ErrClosed {
		t.Errorf("err = %v, want ErrClosed", err)
	}
}
