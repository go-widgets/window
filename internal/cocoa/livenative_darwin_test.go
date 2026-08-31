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

		// toolkit -> native: setting the model observable updates the real field.
		pw.Text().Set("newsecret")
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
