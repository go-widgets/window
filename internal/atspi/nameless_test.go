// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package atspi

import (
	"testing"

	"github.com/go-widgets/toolkit"
)

// everyRole pairs each toolkit role with whether a nameless node of that role
// stays in the published tree (it can take focus or input).
var everyRole = []struct {
	role toolkit.Role
	keep bool
}{
	{toolkit.RoleAlert, false}, {toolkit.RoleBanner, false}, {toolkit.RoleButton, true},
	{toolkit.RoleCheckbox, true}, {toolkit.RoleCombobox, true}, {toolkit.RoleDialog, false},
	{toolkit.RoleDocument, false}, {toolkit.RoleGrid, true}, {toolkit.RoleGroup, false},
	{toolkit.RoleHeading, false}, {toolkit.RoleImg, false}, {toolkit.RoleLink, true},
	{toolkit.RoleList, false}, {toolkit.RoleListItem, false}, {toolkit.RoleListbox, true},
	{toolkit.RoleLog, false}, {toolkit.RoleMenu, false}, {toolkit.RoleMenuBar, false},
	{toolkit.RoleMeter, false}, {toolkit.RoleNavigation, false}, {toolkit.RolePresentation, false},
	{toolkit.RoleProgressbar, false}, {toolkit.RoleRadio, true}, {toolkit.RoleSearchbox, true},
	{toolkit.RoleSeparator, true}, {toolkit.RoleSlider, true}, {toolkit.RoleSpinbutton, true},
	{toolkit.RoleStatus, false}, {toolkit.RoleSwitch, true}, {toolkit.RoleTab, true},
	{toolkit.RoleTablist, false}, {toolkit.RoleText, false}, {toolkit.RoleTextbox, true},
	{toolkit.RoleToolbar, false}, {toolkit.RoleTooltip, false}, {toolkit.RoleTree, true},
}

func TestSkipAndNameEveryRoleWithAndWithoutName(t *testing.T) {
	full := toolkit.Rect{X: 1, Y: 2, W: 10, H: 20}
	mk := func(r toolkit.Role, name string) toolkit.A11yNode {
		return toolkit.A11yNode{A11yInfo: toolkit.A11yInfo{Role: r, Name: name, Value: "val"}, Rect: full}
	}
	for _, c := range everyRole {
		if Skip(mk(c.role, "label")) {
			t.Errorf("%q named was skipped", c.role)
		}
		if got := Name(mk(c.role, "label")); got != "label" {
			t.Errorf("%q named: Name = %q, want the name", c.role, got)
		}
		if got := Skip(mk(c.role, "")); got != !c.keep {
			t.Errorf("%q nameless: Skip = %v, want %v", c.role, got, !c.keep)
		}
		want := ""
		if c.keep {
			want = "val" // no Text interface yet: the value is what Orca can read
		}
		if got := Name(mk(c.role, "")); got != want {
			t.Errorf("%q nameless: Name = %q, want %q", c.role, got, want)
		}
	}
}

func TestNodesKeepsNamelessEntry(t *testing.T) {
	box := toolkit.NewContainer(nil)
	e := toolkit.NewEntry("hello")
	e.SetBounds(toolkit.Rect{W: 80, H: 20})
	box.AddWidget(e)
	got := Nodes(box)
	if len(got) != 1 || got[0].Role != toolkit.RoleTextbox || got[0].Value != "hello" {
		t.Fatalf("Nodes = %+v, want the textbox carrying its value", got)
	}
}
