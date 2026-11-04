// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package win32

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

func TestA11ySkipEveryRoleWithAndWithoutName(t *testing.T) {
	full := toolkit.Rect{X: 1, Y: 2, W: 10, H: 20}
	for _, c := range everyRole {
		if A11ySkip(a11yNode(c.role, "label", full)) {
			t.Errorf("%q named was skipped", c.role)
		}
		if got := A11ySkip(a11yNode(c.role, "", full)); got != !c.keep {
			t.Errorf("%q nameless: A11ySkip = %v, want %v", c.role, got, !c.keep)
		}
	}
}

// A nameless text field is published, with its value, so UI Automation can
// reach it and its Value pattern can answer.
func TestA11yNodesKeepsNamelessEntry(t *testing.T) {
	box := toolkit.NewContainer(nil)
	e := toolkit.NewEntry("hello")
	e.SetBounds(toolkit.Rect{W: 80, H: 20})
	box.AddWidget(e)
	got := A11yNodes(box)
	if len(got) != 1 || got[0].Role != toolkit.RoleTextbox || got[0].Value != "hello" {
		t.Fatalf("A11yNodes = %+v, want the textbox carrying its value", got)
	}
}
