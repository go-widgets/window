// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package cocoa

import (
	"testing"

	"github.com/go-widgets/toolkit"
)

func node(role toolkit.Role, name string, r toolkit.Rect) toolkit.A11yNode {
	return toolkit.A11yNode{A11yInfo: toolkit.A11yInfo{Role: role, Name: name}, Rect: r}
}

func TestAXRole(t *testing.T) {
	cases := []struct {
		in   toolkit.Role
		want string
	}{
		{toolkit.RoleButton, "AXButton"},
		{toolkit.RoleText, "AXStaticText"},
		{toolkit.RoleStatus, "AXStaticText"},
		{toolkit.RoleTextbox, "AXTextField"},
		{toolkit.RoleSearchbox, "AXTextField"},
		{toolkit.RoleCheckbox, "AXCheckBox"},
		{toolkit.RoleSwitch, "AXCheckBox"},
		{toolkit.RoleRadio, "AXRadioButton"},
		{toolkit.RoleSlider, "AXSlider"},
		{toolkit.RoleSpinbutton, "AXSlider"},
		{toolkit.RoleImg, "AXImage"},
		{toolkit.RoleList, "AXList"},
		{toolkit.RoleListbox, "AXList"},
		{toolkit.RoleGrid, "AXTable"},
		{toolkit.RoleToolbar, "AXToolbar"},
		{toolkit.RoleMenu, "AXMenu"},
		{toolkit.RoleMenuBar, "AXMenuBar"},
		{toolkit.RoleProgressbar, "AXProgressIndicator"},
		{toolkit.RoleMeter, "AXProgressIndicator"},
		{toolkit.RoleCombobox, "AXComboBox"},
		{toolkit.RoleAlert, "AXSheet"},
		{toolkit.RoleDialog, "AXSheet"},
		{toolkit.RoleTooltip, "AXHelpTag"},
		// Unmapped and unknown roles both fall back to the container role
		// rather than to a wrong announcement.
		{toolkit.RoleGroup, "AXGroup"},
		{toolkit.RoleDocument, "AXGroup"},
		{toolkit.Role("no-such-role"), "AXGroup"},
	}
	for _, c := range cases {
		if got := AXRole(c.in); got != c.want {
			t.Errorf("AXRole(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestA11ySkip(t *testing.T) {
	full := toolkit.Rect{X: 1, Y: 2, W: 10, H: 20}
	cases := []struct {
		name string
		n    toolkit.A11yNode
		want bool
	}{
		{"named with area", node(toolkit.RoleButton, "OK", full), false},
		{"unnamed", node(toolkit.RoleButton, "", full), true},
		{"zero width", node(toolkit.RoleButton, "OK", toolkit.Rect{W: 0, H: 20}), true},
		{"zero height", node(toolkit.RoleButton, "OK", toolkit.Rect{W: 10, H: 0}), true},
		{"negative", node(toolkit.RoleButton, "OK", toolkit.Rect{W: -5, H: -5}), true},
	}
	for _, c := range cases {
		if got := A11ySkip(c.n); got != c.want {
			t.Errorf("%s: A11ySkip = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestA11yFrame(t *testing.T) {
	n := node(toolkit.RoleButton, "OK", toolkit.Rect{X: 20, Y: 40, W: 60, H: 24})

	// At the readable default scale the framebuffer IS points, so nothing moves.
	if x, y, w, h := A11yFrame(n, 1); x != 20 || y != 40 || w != 60 || h != 24 {
		t.Errorf("scale 1 = %v,%v %vx%v, want 20,40 60x24", x, y, w, h)
	}
	// At a 2x render scale every dimension halves, and the Y axis does NOT
	// flip: the content view is flipped to match the framebuffer.
	if x, y, w, h := A11yFrame(n, 2); x != 10 || y != 20 || w != 30 || h != 12 {
		t.Errorf("scale 2 = %v,%v %vx%v, want 10,20 30x12", x, y, w, h)
	}
	// A nonsensical scale must not divide by zero or mirror the window.
	if x, y, w, h := A11yFrame(n, 0); x != 20 || y != 40 || w != 60 || h != 24 {
		t.Errorf("scale 0 = %v,%v %vx%v, want the scale-1 result", x, y, w, h)
	}
}

func TestPressPointRoundTrip(t *testing.T) {
	n := node(toolkit.RoleButton, "OK", toolkit.Rect{X: 20, Y: 40, W: 60, H: 24})
	s := PressPoint(n)
	if s != "50,52" {
		t.Fatalf("PressPoint = %q, want %q (the element's centre)", s, "50,52")
	}
	x, y, ok := ParsePressPoint(s)
	if !ok || x != 50 || y != 52 {
		t.Fatalf("ParsePressPoint(%q) = %d,%d ok=%v, want 50,52 true", s, x, y, ok)
	}
}

// A malformed identifier must REFUSE. Falling back to (0,0) would press
// whatever sits in the window's top-left corner — a real control, so the
// failure would look like a working press of the wrong thing.
func TestParsePressPointRefusesGarbage(t *testing.T) {
	for _, s := range []string{"", "50", "a,52", "50,b", "50;52"} {
		if x, y, ok := ParsePressPoint(s); ok {
			t.Errorf("ParsePressPoint(%q) = %d,%d ok=true, want refusal", s, x, y)
		}
	}
}

func TestA11yNodesFiltersWhatNoReaderCouldUse(t *testing.T) {
	box := toolkit.NewContainer(nil)
	ok := toolkit.NewButton("OK", nil)
	ok.SetBounds(toolkit.Rect{X: 0, Y: 0, W: 40, H: 20})
	blank := toolkit.NewButton("", nil) // no name: nothing to announce
	blank.SetBounds(toolkit.Rect{X: 0, Y: 30, W: 40, H: 20})
	collapsed := toolkit.NewButton("Hidden", nil) // no area: cannot be pointed at
	collapsed.SetBounds(toolkit.Rect{X: 0, Y: 60, W: 0, H: 0})
	box.AddWidget(ok).AddWidget(blank).AddWidget(collapsed)

	got := A11yNodes(box)
	if len(got) != 1 || got[0].Name != "OK" {
		t.Fatalf("A11yNodes = %+v, want only the named button with area", got)
	}
}

func TestA11yNodesEmptyTree(t *testing.T) {
	if got := A11yNodes(nil); len(got) != 0 {
		t.Fatalf("A11yNodes(nil) = %+v, want empty", got)
	}
}

func TestA11yTreeSig(t *testing.T) {
	base := []toolkit.A11yNode{
		node(toolkit.RoleButton, "Refresh", toolkit.Rect{X: 1, Y: 2, W: 3, H: 4}),
		node(toolkit.RoleText, "Today", toolkit.Rect{X: 5, Y: 6, W: 7, H: 8}),
	}
	base[1].Value = "v" // node() leaves Value empty; exercise it too

	sig := a11yTreeSig(base)
	if sig != a11yTreeSig(base) {
		t.Fatal("signature must be stable for identical trees")
	}
	if a11yTreeSig(nil) == sig {
		t.Fatal("an empty tree must not collide with a populated one")
	}
	if a11yTreeSig(nil) != a11yTreeSig([]toolkit.A11yNode{}) {
		t.Fatal("nil and empty slices must hash the same")
	}

	mut := func(f func(n *toolkit.A11yNode)) uint64 {
		cp := append([]toolkit.A11yNode(nil), base...)
		f(&cp[0])
		return a11yTreeSig(cp)
	}
	for name, f := range map[string]func(n *toolkit.A11yNode){
		"role":  func(n *toolkit.A11yNode) { n.Role = toolkit.RoleText },
		"name":  func(n *toolkit.A11yNode) { n.Name = "X" },
		"value": func(n *toolkit.A11yNode) { n.Value = "x" },
		"rectX": func(n *toolkit.A11yNode) { n.Rect.X++ },
		"rectY": func(n *toolkit.A11yNode) { n.Rect.Y++ },
		"rectW": func(n *toolkit.A11yNode) { n.Rect.W++ },
		"rectH": func(n *toolkit.A11yNode) { n.Rect.H++ },
	} {
		if mut(f) == sig {
			t.Fatalf("changing %s must change the signature", name)
		}
	}
}
