// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package win32

import (
	"testing"

	"github.com/go-widgets/toolkit"
)

func a11yNode(role toolkit.Role, name string, r toolkit.Rect) toolkit.A11yNode {
	return toolkit.A11yNode{A11yInfo: toolkit.A11yInfo{Role: role, Name: name}, Rect: r}
}

func TestUIAControlType(t *testing.T) {
	cases := []struct {
		in   toolkit.Role
		want int32
	}{
		{toolkit.RoleButton, CtButton},
		{toolkit.RoleText, CtText},
		{toolkit.RoleStatus, CtText},
		{toolkit.RoleTextbox, CtEdit},
		{toolkit.RoleSearchbox, CtEdit},
		{toolkit.RoleCheckbox, CtCheckBox},
		{toolkit.RoleSwitch, CtCheckBox},
		{toolkit.RoleRadio, CtRadio},
		{toolkit.RoleCombobox, CtComboBox},
		{toolkit.RoleSlider, CtSlider},
		{toolkit.RoleSpinbutton, CtSpinner},
		{toolkit.RoleImg, CtImage},
		{toolkit.RoleList, CtList},
		{toolkit.RoleListbox, CtList},
		{toolkit.RoleGrid, CtDataGrid},
		{toolkit.RoleToolbar, CtToolBar},
		{toolkit.RoleMenu, CtMenu},
		{toolkit.RoleMenuBar, CtMenuBar},
		{toolkit.RoleProgressbar, CtProgress},
		{toolkit.RoleMeter, CtProgress},
		{toolkit.RoleTablist, CtTab},
		{toolkit.RoleTree, CtTree},
		{toolkit.RoleDocument, CtDocument},
		{toolkit.RoleTab, CtTabItem},
		{toolkit.RoleListItem, CtListItem},
		{toolkit.RoleLink, CtHyperlink},
		{toolkit.RoleTooltip, CtToolTip},
		// A dialog IS a window to UI Automation, which has no dialog type -- and
		// a window is also what a reader announces when one opens. This row used
		// to say CtGroup: the test was pinning the gap.
		{toolkit.RoleAlert, CtWindow},
		{toolkit.RoleDialog, CtWindow},
		// UIA carries heading depth as a property of text, not as a type, and
		// CtHeader is a TABLE header -- it would rename the element.
		{toolkit.RoleHeading, CtText},
		// These return the container type DELIBERATELY. A role this table has
		// never seen returns the same one, which is why
		// TestEveryToolkitRoleIsDecided exists.
		{toolkit.RoleGroup, CtGroup},
		{toolkit.RoleBanner, CtGroup},
		{toolkit.RoleNavigation, CtGroup},
		{toolkit.RoleLog, CtGroup},
		{toolkit.RolePresentation, CtGroup},
		{toolkit.Role("no-such-role"), CtGroup},
	}
	for _, c := range cases {
		if got := UIAControlType(c.in); got != c.want {
			t.Errorf("UIAControlType(%q) = %d, want %d", c.in, got, c.want)
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
		{"named with area", a11yNode(toolkit.RoleButton, "OK", full), false},
		{"unnamed", a11yNode(toolkit.RoleButton, "", full), true},
		{"zero width", a11yNode(toolkit.RoleButton, "OK", toolkit.Rect{W: 0, H: 20}), true},
		{"zero height", a11yNode(toolkit.RoleButton, "OK", toolkit.Rect{W: 10, H: 0}), true},
		{"negative", a11yNode(toolkit.RoleButton, "OK", toolkit.Rect{W: -1, H: -1}), true},
	}
	for _, c := range cases {
		if got := A11ySkip(c.n); got != c.want {
			t.Errorf("%s: A11ySkip = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestA11yNodesFilters(t *testing.T) {
	box := toolkit.NewContainer(nil)
	ok := toolkit.NewButton("OK", nil)
	ok.SetBounds(toolkit.Rect{X: 0, Y: 0, W: 40, H: 20})
	blank := toolkit.NewButton("", nil)
	blank.SetBounds(toolkit.Rect{X: 0, Y: 30, W: 40, H: 20})
	box.AddWidget(ok).AddWidget(blank)

	got := A11yNodes(box)
	if len(got) != 1 || got[0].Name != "OK" {
		t.Fatalf("A11yNodes = %+v, want only the named button", got)
	}
	if got := A11yNodes(nil); len(got) != 0 {
		t.Fatalf("A11yNodes(nil) = %+v, want empty", got)
	}
}

// The two halves of the conversion fail in different, equally invisible ways:
// dropping the scale shrinks every element on a high-DPI display, dropping the
// origin reports it relative to the window while the client places it on the
// desktop.
func TestScreenRect(t *testing.T) {
	n := a11yNode(toolkit.RoleButton, "OK", toolkit.Rect{X: 10, Y: 20, W: 30, H: 40})

	if x, y, w, h := ScreenRect(n, 1, 100, 200); x != 110 || y != 220 || w != 30 || h != 40 {
		t.Errorf("scale 1 = %v,%v %vx%v, want 110,220 30x40", x, y, w, h)
	}
	if x, y, w, h := ScreenRect(n, 2, 100, 200); x != 120 || y != 240 || w != 60 || h != 80 {
		t.Errorf("scale 2 = %v,%v %vx%v, want 120,240 60x80", x, y, w, h)
	}
	// A nonsensical scale must not collapse the window onto its origin.
	if x, y, w, h := ScreenRect(n, 0, 0, 0); x != 10 || y != 20 || w != 30 || h != 40 {
		t.Errorf("scale 0 = %v,%v %vx%v, want the scale-1 result", x, y, w, h)
	}
}

func TestPressPointRoundTrip(t *testing.T) {
	n := a11yNode(toolkit.RoleButton, "OK", toolkit.Rect{X: 10, Y: 20, W: 30, H: 40})
	s := PressPoint(n)
	if s != "25,40" {
		t.Fatalf("PressPoint = %q, want %q (the element's centre)", s, "25,40")
	}
	if x, y, ok := ParsePressPoint(s); !ok || x != 25 || y != 40 {
		t.Fatalf("ParsePressPoint(%q) = %d,%d ok=%v, want 25,40 true", s, x, y, ok)
	}
}

func TestParsePressPointRefusesGarbage(t *testing.T) {
	for _, s := range []string{"", "25", "a,40", "25,b", "25;40"} {
		if x, y, ok := ParsePressPoint(s); ok {
			t.Errorf("ParsePressPoint(%q) = %d,%d ok=true, want refusal", s, x, y)
		}
	}
}

// containerByDesign are the roles UIAControlType answers with the container type
// on purpose: ARIA landmarks and a log, for which UI Automation has no control
// type at all (it carries liveness as a PROPERTY), a plain group, and
// presentation -- which never reaches a reader, carrying no name.
var containerByDesign = map[toolkit.Role]bool{
	toolkit.RoleGroup:        true,
	toolkit.RoleBanner:       true,
	toolkit.RoleNavigation:   true,
	toolkit.RoleLog:          true,
	toolkit.RolePresentation: true,
}

// TestEveryToolkitRoleIsDecided is the guard that would have caught RoleListItem
// -- which this package had even DECLARED a control type for (CtListItem, the
// documented 50007) and never returned. The constant sat there unused while
// every row of every list was announced as a group.
//
// A missing case and a deliberate container answer return the same number, so
// nothing could tell them apart. This test can: every role the toolkit defines
// must either map to a real UIA control type or be listed above as deliberate,
// and the count is asserted so a role added upstream fails here rather than
// disappearing into the default.
func TestEveryToolkitRoleIsDecided(t *testing.T) {
	all := []toolkit.Role{
		toolkit.RoleAlert, toolkit.RoleBanner, toolkit.RoleButton,
		toolkit.RoleCheckbox, toolkit.RoleCombobox, toolkit.RoleDialog,
		toolkit.RoleDocument, toolkit.RoleGrid, toolkit.RoleGroup,
		toolkit.RoleHeading, toolkit.RoleImg, toolkit.RoleLink,
		toolkit.RoleList, toolkit.RoleListItem, toolkit.RoleListbox,
		toolkit.RoleLog, toolkit.RoleMenu, toolkit.RoleMenuBar,
		toolkit.RoleMeter, toolkit.RoleNavigation, toolkit.RolePresentation,
		toolkit.RoleProgressbar, toolkit.RoleRadio, toolkit.RoleSearchbox,
		toolkit.RoleSlider, toolkit.RoleSpinbutton, toolkit.RoleStatus,
		toolkit.RoleSwitch, toolkit.RoleTab, toolkit.RoleTablist,
		toolkit.RoleText, toolkit.RoleTextbox, toolkit.RoleToolbar,
		toolkit.RoleTooltip, toolkit.RoleTree,
	}
	const known = 35
	if len(all) != known {
		t.Fatalf("this list holds %d roles, not %d: keep it whole, it is the "+
			"only thing standing between a new role and a silent CtGroup", len(all), known)
	}
	seen := make(map[toolkit.Role]bool, len(all))
	for _, r := range all {
		if seen[r] {
			t.Errorf("role %q is listed twice, so the count means less than it looks", r)
		}
		seen[r] = true
		if UIAControlType(r) == CtGroup && !containerByDesign[r] {
			t.Errorf("role %q announces as a plain group: either give it a UIA "+
				"control type or say in containerByDesign that UIA has none", r)
		}
	}
}
