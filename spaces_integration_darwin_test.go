// Copyright (c) 2026 the go-widgets/window authors. All rights reserved.
// Use of this source code is governed by a BSD-3-Clause license that can be
// found in the LICENSE file at the root of this repository.

//go:build darwin

package window_test

import (
	"os"
	"runtime"
	"testing"

	"github.com/ebitengine/purego"
	"github.com/go-macos/objc"
	"github.com/go-widgets/window"
)

// TestAnImmersiveWindowJoinsEverySpace opens a real window, so it runs only
// under WINDOW_DARWIN_INTEGRATION.
//
// THE DEFECT IT PINS. A window belongs to the ONE Space it was opened in unless
// it says otherwise, and macOS slides it away whenever that display changes
// Space -- which clicking a desktop can do. For an ordinary document window
// that is correct. For a surface deliberately put OVER a display it is not: the
// viewer sees the picture slide off to the side and the real desktop underneath
// it, which is how it was reported from a pair of display glasses -- "on voit
// que tu superposes une image sur le bureau car elle glisse sur le cote".
func TestAnImmersiveWindowJoinsEverySpace(t *testing.T) {
	if os.Getenv("WINDOW_DARWIN_INTEGRATION") == "" {
		t.Skip("set WINDOW_DARWIN_INTEGRATION=1 to open a real window")
	}
	runtime.LockOSThread()

	w, err := window.Open(window.Config{
		Title: "immersive spaces", Fullscreen: true, Immersive: true, Passive: true,
	})
	if err != nil {
		t.Fatalf("Open = %v", err)
	}
	defer w.Close()

	const (
		canJoinAllSpaces    = 1 << 0
		stationary          = 1 << 4
		fullScreenAuxiliary = 1 << 8
	)
	for _, b := range collectionBehaviours(t) {
		if b&canJoinAllSpaces == 0 {
			t.Errorf("collectionBehavior %d does not join every Space", b)
		}
		if b&stationary == 0 {
			t.Errorf("collectionBehavior %d moves with the Spaces", b)
		}
		if b&fullScreenAuxiliary == 0 {
			t.Errorf("collectionBehavior %d is hidden by a full-screen application", b)
		}
	}
}

// collectionBehaviours asks AppKit itself, rather than trusting what was sent
// to it: the point of the test is that the window ended up with the behaviour,
// not that a message was sent.
func collectionBehaviours(t *testing.T) []uint {
	t.Helper()
	if err := objc.Load(objc.AppKit); err != nil {
		t.Fatalf("AppKit: %v", err)
	}
	h, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation",
		purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	if err != nil {
		t.Fatalf("CoreFoundation: %v", err)
	}
	var count func(uintptr) int
	var at func(uintptr, int) uintptr
	purego.RegisterLibFunc(&count, h, "CFArrayGetCount")
	purego.RegisterLibFunc(&at, h, "CFArrayGetValueAtIndex")

	app := objc.App()
	wins := uintptr(app.Send(objc.RegisterName("windows")))
	n := count(wins)
	if n == 0 {
		t.Fatal("no windows are open")
	}
	out := make([]uint, 0, n)
	for i := 0; i < n; i++ {
		id := objc.ID(at(wins, i))
		out = append(out, uint(id.Send(objc.RegisterName("collectionBehavior"))))
	}
	return out
}
