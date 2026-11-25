// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package cocoa

import "testing"

func TestDialogQueueIsFIFOAndEmptiesCleanly(t *testing.T) {
	dialogMu.Lock()
	dialogQueue = nil
	dialogQueue = append(dialogQueue, DialogRequest{Title: "one"}, DialogRequest{Title: "two"})
	dialogMu.Unlock()
	for _, want := range []string{"one", "two"} {
		r, ok := popDialog()
		if !ok || r.Title != want {
			t.Fatalf("pop = %q, %v; want %q", r.Title, ok, want)
		}
	}
	if r, ok := popDialog(); ok {
		t.Fatalf("empty queue popped %+v", r)
	}
}

func TestDialogOnAClosedOrNilWindowIsRefused(t *testing.T) {
	var nilWin *Window
	if err := nilWin.Dialog(DialogRequest{}); err != ErrClosed {
		t.Errorf("nil window: %v", err)
	}
	if err := (&Window{}).Dialog(DialogRequest{}); err != ErrClosed {
		t.Errorf("window with no view: %v", err)
	}
	dialogMu.Lock()
	n := len(dialogQueue)
	dialogMu.Unlock()
	if n != 0 {
		t.Errorf("a refused request was queued (%d)", n)
	}
}
