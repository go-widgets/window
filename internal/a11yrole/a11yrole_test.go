// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package a11yrole

import (
	"testing"

	"github.com/go-widgets/toolkit"
)

// roles is every role the toolkit defines, each with whether it can take focus
// or input. Keep it whole: the count below is the guard that makes a role added
// upstream get decided here instead of being silently dropped when nameless.
var roles = []struct {
	role        toolkit.Role
	interactive bool
}{
	{toolkit.RoleAlert, false}, {toolkit.RoleBanner, false},
	{toolkit.RoleButton, true}, {toolkit.RoleCheckbox, true},
	{toolkit.RoleCombobox, true}, {toolkit.RoleDialog, false},
	{toolkit.RoleDocument, false}, {toolkit.RoleGrid, true},
	{toolkit.RoleGroup, false}, {toolkit.RoleHeading, false},
	{toolkit.RoleImg, false}, {toolkit.RoleLink, true},
	{toolkit.RoleList, false}, {toolkit.RoleListItem, false},
	{toolkit.RoleListbox, true}, {toolkit.RoleLog, false},
	{toolkit.RoleMenu, false}, {toolkit.RoleMenuBar, false},
	{toolkit.RoleMeter, false}, {toolkit.RoleNavigation, false},
	{toolkit.RolePresentation, false}, {toolkit.RoleProgressbar, false},
	{toolkit.RoleRadio, true}, {toolkit.RoleSearchbox, true},
	{toolkit.RoleSeparator, true}, {toolkit.RoleSlider, true},
	{toolkit.RoleSpinbutton, true}, {toolkit.RoleStatus, false},
	{toolkit.RoleSwitch, true}, {toolkit.RoleTab, true},
	{toolkit.RoleTablist, false}, {toolkit.RoleText, false},
	{toolkit.RoleTextbox, true}, {toolkit.RoleToolbar, false},
	{toolkit.RoleTooltip, false}, {toolkit.RoleTree, true},
	{toolkit.Role("no-such-role"), false},
	{toolkit.Role(""), false},
}

func node(r toolkit.Role, name, value string, rect toolkit.Rect) toolkit.A11yNode {
	return toolkit.A11yNode{A11yInfo: toolkit.A11yInfo{Role: r, Name: name, Value: value}, Rect: rect}
}

func TestEveryRoleIsDecided(t *testing.T) {
	const known = 38 // 36 toolkit roles + an unknown one + the empty one
	if len(roles) != known {
		t.Fatalf("the table holds %d roles, not %d: keep it whole", len(roles), known)
	}
	seen := map[toolkit.Role]bool{}
	for _, c := range roles {
		if seen[c.role] {
			t.Errorf("role %q listed twice", c.role)
		}
		seen[c.role] = true
		if got := Interactive(c.role); got != c.interactive {
			t.Errorf("Interactive(%q) = %v, want %v", c.role, got, c.interactive)
		}
	}
}

func TestSkipEveryRoleWithAndWithoutName(t *testing.T) {
	full := toolkit.Rect{X: 1, Y: 2, W: 10, H: 20}
	for _, c := range roles {
		if Skip(node(c.role, "label", "", full)) {
			t.Errorf("%q named with area was skipped", c.role)
		}
		if got := Skip(node(c.role, "", "text", full)); got != !c.interactive {
			t.Errorf("%q nameless: Skip = %v, want %v", c.role, got, !c.interactive)
		}
		// No area always skips, named or not, interactive or not.
		for _, r := range []toolkit.Rect{{W: 0, H: 20}, {W: 10, H: 0}, {W: -1, H: -1}} {
			if !Skip(node(c.role, "label", "", r)) {
				t.Errorf("%q with rect %+v was kept", c.role, r)
			}
		}
	}
}

func TestLabel(t *testing.T) {
	full := toolkit.Rect{W: 1, H: 1}
	for _, c := range roles {
		if got := Label(node(c.role, "Name", "val", full)); got != "Name" {
			t.Errorf("%q named: Label = %q, want the name", c.role, got)
		}
		want := ""
		if c.interactive {
			want = "val"
		}
		if got := Label(node(c.role, "", "val", full)); got != want {
			t.Errorf("%q nameless: Label = %q, want %q", c.role, got, want)
		}
	}
}

func TestNodesKeepsNamelessEntry(t *testing.T) {
	box := toolkit.NewContainer(nil)
	e := toolkit.NewEntry("hello")
	e.SetBounds(toolkit.Rect{W: 80, H: 20})
	lbl := toolkit.NewLabel("")
	lbl.SetBounds(toolkit.Rect{Y: 30, W: 80, H: 20})
	box.AddWidget(e).AddWidget(lbl)
	got := Nodes(box)
	if len(got) != 1 || got[0].Role != toolkit.RoleTextbox {
		t.Fatalf("Nodes = %+v, want only the nameless textbox", got)
	}
}
