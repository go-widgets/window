// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package atspi

import (
	"testing"

	"github.com/go-widgets/toolkit"
)

func node(role toolkit.Role, name string, r toolkit.Rect) toolkit.A11yNode {
	return toolkit.A11yNode{A11yInfo: toolkit.A11yInfo{Role: role, Name: name}, Rect: r}
}

func TestRole(t *testing.T) {
	cases := []struct {
		in   toolkit.Role
		want uint32
	}{
		{toolkit.RoleButton, RolePushButton},
		{toolkit.RoleText, RoleLabel},
		{toolkit.RoleStatus, RoleStatusBar},
		{toolkit.RoleTextbox, RoleEntry},
		{toolkit.RoleSearchbox, RoleEntry},
		{toolkit.RoleCheckbox, RoleCheckBox},
		{toolkit.RoleSwitch, RoleToggleButton},
		{toolkit.RoleRadio, RoleRadioButton},
		{toolkit.RoleCombobox, RoleComboBox},
		{toolkit.RoleSlider, RoleSlider},
		{toolkit.RoleSpinbutton, RoleSpinButton},
		{toolkit.RoleImg, RoleImage},
		{toolkit.RoleList, RoleList},
		{toolkit.RoleListbox, RoleList},
		{toolkit.RoleGrid, RoleTable},
		{toolkit.RoleToolbar, RoleToolBar},
		{toolkit.RoleMenu, RoleMenu},
		{toolkit.RoleMenuBar, RoleMenuBar},
		{toolkit.RoleProgressbar, RoleProgressBar},
		{toolkit.RoleMeter, RoleProgressBar},
		{toolkit.RoleTablist, RolePageTab},
		{toolkit.RoleTree, RoleTree},
		{toolkit.RoleAlert, RoleAlert},
		{toolkit.RoleDialog, RoleAlert},
		{toolkit.RoleDocument, RoleDocFrame},
		// Unmapped and unknown both become a panel rather than a wrong
		// announcement.
		{toolkit.RoleGroup, RolePanel},
		{toolkit.RoleTooltip, RolePanel},
		{toolkit.Role("no-such-role"), RolePanel},
	}
	for _, c := range cases {
		if got := Role(c.in); got != c.want {
			t.Errorf("Role(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestRoleName(t *testing.T) {
	cases := map[uint32]string{
		RolePushButton:   "push button",
		RoleLabel:        "label",
		RoleEntry:        "entry",
		RoleCheckBox:     "check box",
		RoleToggleButton: "toggle button",
		RoleRadioButton:  "radio button",
		RoleComboBox:     "combo box",
		RoleSlider:       "slider",
		RoleSpinButton:   "spin button",
		RoleImage:        "image",
		RoleList:         "list",
		RoleListItem:     "list item",
		RoleTable:        "table",
		RoleToolBar:      "tool bar",
		RoleMenu:         "menu",
		RoleMenuBar:      "menu bar",
		RoleProgressBar:  "progress bar",
		RolePageTab:      "page tab",
		RoleTree:         "tree",
		RoleAlert:        "alert",
		RoleDocFrame:     "document frame",
		RoleStatusBar:    "status bar",
		RoleApplication:  "application",
		RoleWindow:       "window",
		RolePanel:        "panel",
		RoleFiller:       "filler",
		RoleText:         "text",
		RoleInvalid:      "unknown",
		RoleScrollBar:    "unknown",
	}
	for r, want := range cases {
		if got := RoleName(r); got != want {
			t.Errorf("RoleName(%d) = %q, want %q", r, got, want)
		}
	}
}

func TestSkip(t *testing.T) {
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
		{"negative", node(toolkit.RoleButton, "OK", toolkit.Rect{W: -3, H: -3}), true},
	}
	for _, c := range cases {
		if got := Skip(c.n); got != c.want {
			t.Errorf("%s: Skip = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestNodes(t *testing.T) {
	box := toolkit.NewContainer(nil)
	ok := toolkit.NewButton("OK", nil)
	ok.SetBounds(toolkit.Rect{X: 0, Y: 0, W: 40, H: 20})
	blank := toolkit.NewButton("", nil)
	blank.SetBounds(toolkit.Rect{X: 0, Y: 30, W: 40, H: 20})
	box.AddWidget(ok).AddWidget(blank)

	got := Nodes(box)
	if len(got) != 1 || got[0].Name != "OK" {
		t.Fatalf("Nodes = %+v, want only the named button", got)
	}
	if got := Nodes(nil); len(got) != 0 {
		t.Fatalf("Nodes(nil) = %+v, want empty", got)
	}
}

func TestScreenRect(t *testing.T) {
	n := node(toolkit.RoleButton, "OK", toolkit.Rect{X: 10, Y: 20, W: 30, H: 40})
	if x, y, w, h := ScreenRect(n, 100, 200); x != 110 || y != 220 || w != 30 || h != 40 {
		t.Errorf("ScreenRect = %d,%d %dx%d, want 110,220 30x40", x, y, w, h)
	}
	// A Wayland client cannot know where it is, so it passes a zero origin and
	// the two coordinate spaces coincide.
	if x, y, _, _ := ScreenRect(n, 0, 0); x != 10 || y != 20 {
		t.Errorf("zero origin = %d,%d, want the window-relative rect unchanged", x, y)
	}
}

func TestPressPointRoundTrip(t *testing.T) {
	n := node(toolkit.RoleButton, "OK", toolkit.Rect{X: 10, Y: 20, W: 30, H: 40})
	s := PressPoint(n)
	if s != "25,40" {
		t.Fatalf("PressPoint = %q, want %q (the element's centre)", s, "25,40")
	}
	if x, y, ok := ParsePressPoint(s); !ok || x != 25 || y != 40 {
		t.Fatalf("ParsePressPoint(%q) = %d,%d ok=%v, want 25,40 true", s, x, y, ok)
	}
}

func TestParsePressPointRefusesGarbage(t *testing.T) {
	for _, s := range []string{"", "25", "a,40", "25,b", "25 40"} {
		if x, y, ok := ParsePressPoint(s); ok {
			t.Errorf("ParsePressPoint(%q) = %d,%d ok=true, want refusal", s, x, y)
		}
	}
}
