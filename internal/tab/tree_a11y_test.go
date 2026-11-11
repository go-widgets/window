// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package tab

import (
	"testing"

	"github.com/go-widgets/toolkit"
)

// A Tree is what the browser tab hands webcanvas, so what it publishes is what
// a screen reader gets: the buttons by name and a text view as one textbox with
// its text layer.
func TestTreePublishesItsWidgets(t *testing.T) {
	btn := toolkit.NewButton("Run", func() {})
	tv := toolkit.NewTextView("print(1)")
	tv.AccessibleName = "Cell 1"
	root := toolkit.NewVBox()
	root.AddFixed(btn, 24)
	root.AddFixed(tv, 90)
	tr := NewTree(root, 320, 200, nil)
	root.SetBounds(toolkit.Rect{W: 320, H: 200})

	var button, text int
	var tvKey string
	for _, n := range tr.A11yNodes() {
		switch {
		case n.Role == "button" && n.Name == "Run":
			button++
		case n.Role == "textbox" && n.Name == "Cell 1":
			text++
			tvKey = n.Key
		}
	}
	if button != 1 || text != 1 {
		t.Fatalf("buttons %d, textboxes %d; want 1 and 1 (%+v)", button, text, tr.A11yNodes())
	}
	if c := tr.TextInputFor(tvKey); c == nil {
		t.Error("the text view has no text layer")
	}
	if c := tr.TextInputFor("nope"); c != nil {
		t.Error("an unknown key got a text layer")
	}
}
