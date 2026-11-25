// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows && integration

package win32

import (
	"errors"
	"os"
	"runtime"
	"testing"
	"time"
	"unsafe"

	"github.com/go-mswin/win32"
	"github.com/go-widgets/toolkit"
)

var procGetWindowThreadProcessID = user32.NewProc("GetWindowThreadProcessId")

const (
	wmCloseLive    = 0x0010
	tdmClickButton = 0x0400 + 102 // WM_USER + 102; wParam is the button id
)

// otherDialog waits for a visible top-level window of THIS process other than
// the test window, and returns it with its class name.
func otherDialog(own uintptr, wait time.Duration) (uintptr, string) {
	pid := uint32(os.Getpid())
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		var found uintptr
		var class string
		win32.EnumWindows(func(h win32.HWND) bool {
			if uintptr(h) == own || !win32.IsWindowVisible(h) {
				return true
			}
			var p uint32
			procGetWindowThreadProcessID.Call(uintptr(h), uintptr(unsafe.Pointer(&p)))
			if p != pid {
				return true
			}
			found, class = uintptr(h), win32.GetClassName(h)
			return false
		})
		if found != 0 {
			return found, class
		}
		time.Sleep(50 * time.Millisecond)
	}
	return 0, ""
}

// TestLiveWin32Dialogs is the CI proof of the Dialogs capability on a real
// Windows host. Each dialog kind is requested through Window.Dialog from a
// goroutine other than the UI thread, found by the window it really creates
// (EnumWindows, same process, visible, not ours), and dismissed from outside the
// dialog's own code path: WM_CLOSE (what the close box sends) for the file
// dialogs and for the alerts' cancel, TDM_CLICK_BUTTON for a chosen alert
// button. The callback's result is asserted. Choosing a file in the file dialog
// is NOT driven here: only its cancel path is.
func TestLiveWin32Dialogs(t *testing.T) {
	skipUnlessLive(t)
	runtime.LockOSThread() // Run pumps on this thread, which New made the owner
	t.Cleanup(runtime.UnlockOSThread)

	w, err := New("go-widgets/window win32 dialogs", 320, 200, toolkit.DefaultDark())
	if err != nil {
		t.Fatalf("window: %v", err)
	}
	root := &specRoot{}

	go func() {
		defer w.Close()
		own := w.hwnd

		// ask shows req, waits for its window, dismisses it with dismiss, and
		// returns the callback's result.
		ask := func(name string, req DialogRequest, wantClass string, dismiss func(h uintptr)) (DialogResult, bool) {
			res := make(chan DialogResult, 1)
			req.Done = func(r DialogResult) { res <- r }
			if err := w.Dialog(req); err != nil {
				t.Errorf("%s: Dialog: %v", name, err)
				return DialogResult{}, false
			}
			h, class := otherDialog(own, 20*time.Second)
			if h == 0 {
				t.Errorf("%s: no dialog window appeared", name)
				return DialogResult{}, false
			}
			t.Logf("%s: dialog window class %q", name, class)
			if wantClass != "" && class != wantClass {
				t.Errorf("%s: class %q, want %q", name, class, wantClass)
			}
			dismiss(h)
			select {
			case r := <-res:
				t.Logf("%s: result %+v", name, r)
				return r, true
			case <-time.After(20 * time.Second):
				win32.PostMessage(win32.HWND(h), wmCloseLive, 0, 0)
				t.Errorf("%s: the callback never ran after the dismissal", name)
				return DialogResult{}, false
			}
		}
		closeIt := func(h uintptr) { win32.PostMessage(win32.HWND(h), wmCloseLive, 0, 0) }
		click := func(id int32) func(uintptr) {
			return func(h uintptr) { win32.PostMessage(win32.HWND(h), tdmClickButton, win32.WPARAM(id), 0) }
		}

		// Alert, a chosen button: the second of three, by its id.
		if r, ok := ask("alert click", DialogRequest{
			Kind: DialogAlert, Message: "Save changes?", Detail: "detail", Buttons: []string{"Save", "Don't save", "Cancel"},
		}, "#32770", click(buttonIDBase+1)); ok && (r.Err != nil || r.Cancelled || r.Button != 1) {
			t.Errorf("alert click = %+v, want button 1", r)
		}
		// Alert, the close box with no cancel button of its own: a cancel.
		if r, ok := ask("alert close", DialogRequest{
			Kind: DialogAlert, Message: "Sure?", Buttons: []string{"Yes", "No"},
		}, "#32770", closeIt); ok && (r.Err != nil || !r.Cancelled) {
			t.Errorf("alert close = %+v, want cancelled", r)
		}
		// Alert, the close box with a Cancel button: that button's index.
		if r, ok := ask("alert close on Cancel", DialogRequest{
			Kind: DialogAlert, Message: "Sure?", Buttons: []string{"Yes", "No", "Cancel"},
		}, "#32770", closeIt); ok && (r.Err != nil || r.Cancelled || r.Button != 2) {
			t.Errorf("alert close on Cancel = %+v, want button 2", r)
		}
		// Alert, no buttons given: a single OK, index 0.
		if r, ok := ask("alert default", DialogRequest{Kind: DialogAlert, Message: "Done"},
			"#32770", click(buttonIDBase)); ok && (r.Err != nil || r.Cancelled || r.Button != 0) {
			t.Errorf("alert default = %+v, want button 0", r)
		}

		// File dialogs, dismissed with the close box: ERROR_CANCELLED.
		for _, c := range []struct {
			name string
			req  DialogRequest
		}{
			{"open", DialogRequest{Kind: DialogOpen, Title: "Open it", Filters: []DialogFilter{{Name: "Images", Exts: []string{"png", "jpg"}}}}},
			{"open multiple", DialogRequest{Kind: DialogOpen, Multiple: true}},
			{"open directories", DialogRequest{Kind: DialogOpen, Directories: true}},
			{"save", DialogRequest{Kind: DialogSave, Title: "Save it", Name: "out.png", Filters: []DialogFilter{{Name: "PNG", Exts: []string{"png"}}}}},
		} {
			if r, ok := ask(c.name, c.req, "", closeIt); ok && (r.Err != nil || !r.Cancelled || len(r.Paths) != 0) {
				t.Errorf("%s = %+v, want a clean cancel", c.name, r)
			}
		}

		// Two requests queued at once run one after the other, each answered once.
		a, b := make(chan DialogResult, 1), make(chan DialogResult, 1)
		for i, ch := range []chan DialogResult{a, b} {
			ch := ch
			if err := w.Dialog(DialogRequest{Kind: DialogAlert, Message: "queued", Buttons: []string{"A", "B"},
				Done: func(r DialogResult) { ch <- r }}); err != nil {
				t.Errorf("queued %d: %v", i, err)
			}
		}
		for i, ch := range []chan DialogResult{a, b} {
			h, _ := otherDialog(own, 20*time.Second)
			if h == 0 {
				t.Errorf("queued %d: no dialog window", i)
				return
			}
			win32.PostMessage(win32.HWND(h), tdmClickButton, win32.WPARAM(buttonIDBase+i), 0)
			select {
			case r := <-ch:
				if r.Err != nil || r.Button != i {
					t.Errorf("queued %d = %+v, want button %d", i, r, i)
				}
			case <-time.After(20 * time.Second):
				t.Errorf("queued %d never answered", i)
				return
			}
			time.Sleep(200 * time.Millisecond)
		}
	}()

	if err := w.Run(root); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// After the window is gone a request is refused rather than lost.
	if err := w.Dialog(DialogRequest{Kind: DialogAlert, Message: "late", Done: func(DialogResult) {}}); !errors.Is(err, ErrClosed) {
		t.Errorf("Dialog on a closed window = %v, want ErrClosed", err)
	}
}
