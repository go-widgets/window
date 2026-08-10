// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// The OS-independent half of the Windows accessibility bridge: which UI
// Automation control type stands for which toolkit role, which elements are
// worth publishing, and where an element sits once logical points have become
// the physical screen pixels UI Automation speaks.
//
// Free of COM so the whole decision layer is exercised by ordinary tests on any
// host — the same split mapping.go already uses, where the Linux lane proves
// the pure codec to 100% and only win32_windows.go waits for a real Windows
// machine.

package win32

import (
	"strconv"
	"strings"

	"github.com/go-widgets/toolkit"
)

// UI Automation control type identifiers. A wrong number renames every element
// for the user, so these are the documented values rather than a guess.
const (
	CtButton   = 50000
	CtEdit     = 50004
	CtImage    = 50006
	CtListItem = 50007
	CtList     = 50008
	CtMenu     = 50009
	CtMenuBar  = 50010
	CtProgress = 50012
	CtSlider   = 50015
	CtSpinner  = 50016
	CtTab      = 50018
	CtText     = 50020
	CtToolBar  = 50021
	CtTree     = 50023
	CtGroup    = 50026
	CtDataGrid = 50028
	CtDocument = 50030
	CtWindow   = 50032
	CtPane     = 50033
	CtCheckBox = 50002
	CtComboBox = 50003
	CtRadio    = 50013
)

// UIAControlType maps a toolkit role to its UI Automation control type.
// Anything unrecognised becomes a group — UIA's "a thing containing things",
// the honest answer for a role this table does not know, and never a wrong
// announcement.
func UIAControlType(r toolkit.Role) int32 {
	switch r {
	case toolkit.RoleButton:
		return CtButton
	case toolkit.RoleText, toolkit.RoleStatus:
		return CtText
	case toolkit.RoleTextbox, toolkit.RoleSearchbox:
		return CtEdit
	case toolkit.RoleCheckbox, toolkit.RoleSwitch:
		return CtCheckBox
	case toolkit.RoleRadio:
		return CtRadio
	case toolkit.RoleCombobox:
		return CtComboBox
	case toolkit.RoleSlider:
		return CtSlider
	case toolkit.RoleSpinbutton:
		return CtSpinner
	case toolkit.RoleImg:
		return CtImage
	case toolkit.RoleList, toolkit.RoleListbox:
		return CtList
	case toolkit.RoleGrid:
		return CtDataGrid
	case toolkit.RoleToolbar:
		return CtToolBar
	case toolkit.RoleMenu:
		return CtMenu
	case toolkit.RoleMenuBar:
		return CtMenuBar
	case toolkit.RoleProgressbar, toolkit.RoleMeter:
		return CtProgress
	case toolkit.RoleTablist:
		return CtTab
	case toolkit.RoleTree:
		return CtTree
	case toolkit.RoleDocument:
		return CtDocument
	default:
		return CtGroup
	}
}

// A11ySkip reports whether a node should be left out of the published tree. An
// element with no name says nothing a screen reader could announce, and one
// with no area cannot be pointed at; either is a stop the user has to skip past
// for nothing.
func A11ySkip(n toolkit.A11yNode) bool {
	return n.Name == "" || n.Rect.W <= 0 || n.Rect.H <= 0
}

// A11yNodes is the tree to publish for a widget root: every meaningful element,
// in visual order, with the ones no reader could use already removed.
func A11yNodes(root toolkit.Widget) []toolkit.A11yNode {
	all := toolkit.WalkA11y(root)
	out := all[:0:0]
	for _, n := range all {
		if A11ySkip(n) {
			continue
		}
		out = append(out, n)
	}
	return out
}

// ScreenRect converts a node's rectangle from the LOGICAL points the toolkit
// lays out in to the PHYSICAL screen pixels UI Automation reports, given the
// window's DPI scale and the screen position of its client area.
//
// Both halves matter and both are invisible when wrong: forgetting the scale
// puts every element at a fraction of its true size on a high-DPI display, and
// forgetting the origin reports each one relative to the window while the
// client places it on the desktop — so a screen reader's focus ring lands in
// the top-left corner of the screen instead of on the control.
func ScreenRect(n toolkit.A11yNode, scale float64, originX, originY int) (x, y, w, h float64) {
	if scale <= 0 {
		scale = 1
	}
	return float64(originX) + float64(n.Rect.X)*scale,
		float64(originY) + float64(n.Rect.Y)*scale,
		float64(n.Rect.W) * scale,
		float64(n.Rect.H) * scale
}

// PressPoint encodes an element's centre in the LOGICAL points the input path
// speaks, as the string carried on its AutomationId.
//
// Carrying the point ON the element beats a Go-side table keyed by index: the
// tree is rebuilt whenever the frame changes, and an index would go stale the
// moment content scrolled under a screen-reader user's cursor — invoking would
// then activate whatever moved into that slot.
func PressPoint(n toolkit.A11yNode) string {
	return strconv.Itoa(n.Rect.X+n.Rect.W/2) + "," + strconv.Itoa(n.Rect.Y+n.Rect.H/2)
}

// ParsePressPoint reads back what PressPoint wrote. A malformed value REFUSES
// rather than defaulting to (0,0), which is a real and usually clickable place.
func ParsePressPoint(s string) (x, y int, ok bool) {
	sx, sy, found := strings.Cut(s, ",")
	if !found {
		return 0, 0, false
	}
	px, err := strconv.Atoi(sx)
	if err != nil {
		return 0, 0, false
	}
	py, err := strconv.Atoi(sy)
	if err != nil {
		return 0, 0, false
	}
	return px, py, true
}
