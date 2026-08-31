// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin && integration

package cocoa

import (
	"os"
	"testing"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// nativeRoot is a minimal childContainer that stacks its children and exposes
// them to WalkNative.
type nativeRoot struct {
	toolkit.Base
	kids []toolkit.Widget
}

func (r *nativeRoot) SetBounds(b toolkit.Rect) {
	r.Base.SetBounds(b)
	y := b.Y + 10
	for _, k := range r.kids {
		k.SetBounds(toolkit.Rect{X: b.X + 10, Y: y, W: 200, H: 24})
		y += 40
	}
}
func (r *nativeRoot) Draw(p painter.Painter, th *toolkit.Theme) {
	for _, k := range r.kids {
		k.Draw(p, th)
	}
}
func (r *nativeRoot) Children() []toolkit.Widget { return r.kids }

// TestLiveCocoaNativeControls is the on-device proof of the toolkit.Native seam:
// it opens a real window with a Native secure field and a Native button, verifies
// the backend embedded live AppKit controls over the framebuffer, claimed the
// regions, bound the value both ways, and reconciled a control away when its
// Native left the tree. Gated behind WINDOW_COCOA_INTEGRATION.
func TestLiveCocoaNativeControls(t *testing.T) {
	if os.Getenv("WINDOW_COCOA_INTEGRATION") == "" {
		t.Skip("set WINDOW_COCOA_INTEGRATION=1 to run the live macOS native-control proof")
	}

	theme := toolkit.DefaultDark()
	var (
		win      *Window
		setupErr error
		pw       *toolkit.Native
		ok       *toolkit.Native

		count0        int
		pwValue       string
		pwClaimed     bool
		afterSetValue string
		count1        int
		pwGone        bool
		buttonClicks  int
	)

	callOnMain(func() {
		win, setupErr = New("go-widgets/window native-control proof", 360, 200, theme)
		if setupErr != nil {
			return
		}
		pw = toolkit.NewNativeSecureEntry("hunter2")
		pw.Key = "pw"
		ok = toolkit.NewNativeButton("OK", func() { buttonClicks++ })
		ok.Key = "ok"
		root := &nativeRoot{kids: []toolkit.Widget{pw, ok}}

		win.bindAndSeed(root)
		spin(0.4)

		count0 = len(win.nativeControls)
		if lc := win.nativeControls["pw"]; lc != nil {
			pwValue = lc.ctl.StringValue()
		}
		pwClaimed = pw.Claimed().Get()

		// app -> native: the model changes, the app repaints, and the next frame's
		// descriptor carries the new value, which syncNative pushes into the field
		// (it differs from what the control last reported). paintFrame drives that
		// frame here, standing in for the app's own repaint-on-change.
		pw.Text().Set("newsecret")
		win.paintFrame(false)
		spin(0.2)
		if lc := win.nativeControls["pw"]; lc != nil {
			afterSetValue = lc.ctl.StringValue()
		}

		// Reconcile away: a tree without pw drops its control, keeps ok's.
		win.bindAndSeed(&nativeRoot{kids: []toolkit.Widget{ok}})
		spin(0.2)
		count1 = len(win.nativeControls)
		_, present := win.nativeControls["pw"]
		pwGone = !present
	})

	if setupErr != nil {
		t.Fatalf("native-control setup failed: %v", setupErr)
	}
	if count0 != 2 {
		t.Fatalf("embedded controls = %d, want 2", count0)
	}
	if pwValue != "hunter2" {
		t.Errorf("secure field initial value = %q, want hunter2", pwValue)
	}
	if !pwClaimed {
		t.Error("secure-field Native was not marked claimed")
	}
	if afterSetValue != "newsecret" {
		t.Errorf("after model Set, field value = %q, want newsecret (toolkit->native binding)", afterSetValue)
	}
	if count1 != 1 || !pwGone {
		t.Errorf("after reconcile: %d controls, pwGone=%v; want 1 and true", count1, pwGone)
	}
}

// TestLiveCocoaNativeControlsProvider proves the Surface provider path — the one
// a self-rendering app (the news reader) uses: a toolkit.Surface publishes its
// native controls through its Controls field, and the backend embeds and binds
// them exactly as it does for a widget tree. Gated behind WINDOW_COCOA_INTEGRATION.
func TestLiveCocoaNativeControlsProvider(t *testing.T) {
	if os.Getenv("WINDOW_COCOA_INTEGRATION") == "" {
		t.Skip("set WINDOW_COCOA_INTEGRATION=1 to run the live macOS provider proof")
	}
	theme := toolkit.DefaultDark()

	// The app's own state — what a Scene would hold.
	pw := "hunter2"

	var (
		win            *Window
		setupErr       error
		count          int
		initialValue   string
		afterAppChange string
	)

	callOnMain(func() {
		win, setupErr = New("go-widgets/window native provider proof", 320, 180, theme)
		if setupErr != nil {
			return
		}
		surf := toolkit.NewSurface(func() ([]byte, int, int) {
			return make([]byte, 320*180*4), 320, 180
		})
		surf.Controls = func() []toolkit.NativeControl {
			return []toolkit.NativeControl{{
				Kind:    toolkit.NativeSecureEntry,
				Key:     "pw",
				Rect:    toolkit.Rect{X: 10, Y: 10, W: 200, H: 24},
				Visible: true,
				Text:    pw,
				OnText:  func(s string) { pw = s },
			}}
		}

		win.bindAndSeed(surf)
		spin(0.3)
		count = len(win.nativeControls)
		if lc := win.nativeControls["pw"]; lc != nil {
			initialValue = lc.ctl.StringValue()
		}

		// The app changes its own state and repaints; the next descriptor carries
		// the new value and the backend pushes it into the field.
		pw = "changed"
		win.paintFrame(false)
		spin(0.2)
		if lc := win.nativeControls["pw"]; lc != nil {
			afterAppChange = lc.ctl.StringValue()
		}
	})

	if setupErr != nil {
		t.Fatalf("provider setup failed: %v", setupErr)
	}
	if count != 1 {
		t.Fatalf("controls from provider = %d, want 1", count)
	}
	if initialValue != "hunter2" {
		t.Errorf("initial field value = %q, want hunter2", initialValue)
	}
	if afterAppChange != "changed" {
		t.Errorf("after app change, field = %q, want changed (provider descriptor push)", afterAppChange)
	}
}
