// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin && integration

// The live proof for go-widgets/window#153: a text field has no accessible name
// (toolkit Entry.A11y reports only a Value), and the bridge used to drop every
// nameless node, so VoiceOver never saw the field. It opens a real NSWindow, so
// it runs only under -tags=integration with WINDOW_COCOA_INTEGRATION set.
//
// Like the surface proof beside it, it reads back what was PUSHED to AppKit
// (accessibilityChildren of the content view): the role, label and value of
// each element an accessibility client would receive.
package cocoa

import (
	"testing"

	"github.com/go-macos/objc"
	"github.com/go-widgets/toolkit"
)

func TestLiveNamelessEntryIsPublishedToAppKit(t *testing.T) {
	skipUnlessIntegration(t)

	const w, h = 320, 200
	box := toolkit.NewContainer(nil)
	entry := toolkit.NewEntry("hello")
	entry.SetBounds(toolkit.Rect{X: 10, Y: 10, W: 200, H: 28})
	box.AddWidget(entry)
	box.SetBounds(toolkit.Rect{W: w, H: h})

	selChildren := objc.RegisterName("accessibilityChildren")
	selCount := objc.RegisterName("count")
	selAt := objc.RegisterName("objectAtIndex:")
	selRole := objc.RegisterName("accessibilityRole")
	selLabel := objc.RegisterName("accessibilityLabel")
	selValue := objc.RegisterName("accessibilityValue")

	var count int
	var role, label, value string
	callOnMain(func() {
		win, err := NewScaled("nameless entry a11y", w, h, nil, 0)
		if err != nil {
			t.Errorf("open: %v", err)
			return
		}
		defer func() { _ = win.Close() }()
		win.root = box
		win.paintFrame(false)
		kids := win.view.Send(selChildren)
		if kids == 0 {
			return
		}
		count = int(objc.Send[uint64](kids, selCount))
		if count == 0 {
			return
		}
		el := kids.Send(selAt, uint64(0))
		role = objc.GoString(el.Send(selRole))
		label = objc.GoString(el.Send(selLabel))
		value = objc.GoString(el.Send(selValue))
	})

	t.Logf("AX children published to AppKit: %d; first: role=%q label=%q value=%q", count, role, label, value)
	if count != 1 {
		t.Fatalf("AppKit holds %d accessibility children, want 1: the nameless text field is not published", count)
	}
	if role != "AXTextField" || value != "hello" || label != "" {
		t.Errorf("element = role %q label %q value %q, want AXTextField, no label, value \"hello\"", role, label, value)
	}
}
