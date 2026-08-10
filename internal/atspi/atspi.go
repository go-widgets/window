// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// Package atspi is the Linux accessibility bridge: it publishes the widget tree
// on the AT-SPI bus, where Orca and every other Linux screen reader read it.
//
// A go-widgets window presents ONE surface holding a rasterised widget tree. To
// AT-SPI that is an opaque rectangle with no structure at all, so without this
// the application is unreadable and unnavigable. macOS subclasses a view and
// Windows answers WM_GETOBJECT with a COM object; here an application EXPORTS
// D-Bus objects and registers them with a registry daemon.
//
// The package is shared by the X11 and Wayland backends because AT-SPI is a
// D-Bus protocol and knows nothing about either display server.
//
// This file is the OS-independent half — role numbers, filtering, coordinates —
// so the whole decision layer is exercised by ordinary tests on any host, the
// split the x11, wayland, cocoa and win32 packages all use.
package atspi

import (
	"strconv"
	"strings"

	"github.com/go-widgets/toolkit"
)

// AT-SPI role numbers, read out of pyatspi on a live system rather than
// guessed: a client ANNOUNCES the role, so a wrong number renames every element
// for the user.
const (
	RoleInvalid      uint32 = 0
	RoleAlert        uint32 = 2
	RoleCheckBox     uint32 = 7
	RoleComboBox     uint32 = 11
	RoleFiller       uint32 = 20
	RoleImage        uint32 = 27
	RoleLabel        uint32 = 29
	RoleList         uint32 = 31
	RoleListItem     uint32 = 32
	RoleMenu         uint32 = 33
	RoleMenuBar      uint32 = 34
	RolePageTab      uint32 = 37
	RolePanel        uint32 = 39
	RoleProgressBar  uint32 = 41
	RolePushButton   uint32 = 43
	RoleRadioButton  uint32 = 44
	RoleScrollBar    uint32 = 47
	RoleSlider       uint32 = 51
	RoleSpinButton   uint32 = 52
	RoleStatusBar    uint32 = 54
	RoleTable        uint32 = 55
	RoleText         uint32 = 61
	RoleToggleButton uint32 = 62
	RoleToolBar      uint32 = 63
	RoleTree         uint32 = 66
	RoleWindow       uint32 = 69
	RoleApplication  uint32 = 75
	RoleEntry        uint32 = 79
	RoleDocFrame     uint32 = 82
)

// Role maps a toolkit role to its AT-SPI number. Anything unrecognised becomes
// a panel — AT-SPI's "a thing containing things", the honest answer for a role
// this table does not know, and never a wrong announcement.
func Role(r toolkit.Role) uint32 {
	switch r {
	case toolkit.RoleButton:
		return RolePushButton
	case toolkit.RoleText:
		return RoleLabel
	case toolkit.RoleStatus:
		return RoleStatusBar
	case toolkit.RoleTextbox, toolkit.RoleSearchbox:
		return RoleEntry
	case toolkit.RoleCheckbox:
		return RoleCheckBox
	case toolkit.RoleSwitch:
		return RoleToggleButton
	case toolkit.RoleRadio:
		return RoleRadioButton
	case toolkit.RoleCombobox:
		return RoleComboBox
	case toolkit.RoleSlider:
		return RoleSlider
	case toolkit.RoleSpinbutton:
		return RoleSpinButton
	case toolkit.RoleImg:
		return RoleImage
	case toolkit.RoleList, toolkit.RoleListbox:
		return RoleList
	case toolkit.RoleGrid:
		return RoleTable
	case toolkit.RoleToolbar:
		return RoleToolBar
	case toolkit.RoleMenu:
		return RoleMenu
	case toolkit.RoleMenuBar:
		return RoleMenuBar
	case toolkit.RoleProgressbar, toolkit.RoleMeter:
		return RoleProgressBar
	case toolkit.RoleTablist:
		return RolePageTab
	case toolkit.RoleTree:
		return RoleTree
	case toolkit.RoleAlert, toolkit.RoleDialog:
		return RoleAlert
	case toolkit.RoleDocument:
		return RoleDocFrame
	default:
		return RolePanel
	}
}

// RoleName is the human-readable role a client may read instead of the number.
// The strings are AT-SPI's own spelling, not ours.
func RoleName(r uint32) string {
	switch r {
	case RolePushButton:
		return "push button"
	case RoleLabel:
		return "label"
	case RoleEntry:
		return "entry"
	case RoleCheckBox:
		return "check box"
	case RoleToggleButton:
		return "toggle button"
	case RoleRadioButton:
		return "radio button"
	case RoleComboBox:
		return "combo box"
	case RoleSlider:
		return "slider"
	case RoleSpinButton:
		return "spin button"
	case RoleImage:
		return "image"
	case RoleList:
		return "list"
	case RoleListItem:
		return "list item"
	case RoleTable:
		return "table"
	case RoleToolBar:
		return "tool bar"
	case RoleMenu:
		return "menu"
	case RoleMenuBar:
		return "menu bar"
	case RoleProgressBar:
		return "progress bar"
	case RolePageTab:
		return "page tab"
	case RoleTree:
		return "tree"
	case RoleAlert:
		return "alert"
	case RoleDocFrame:
		return "document frame"
	case RoleStatusBar:
		return "status bar"
	case RoleApplication:
		return "application"
	case RoleWindow:
		return "window"
	case RolePanel:
		return "panel"
	case RoleFiller:
		return "filler"
	case RoleText:
		return "text"
	default:
		return "unknown"
	}
}

// AT-SPI state numbers (same provenance as the roles).
const (
	StateEnabled   uint32 = 8
	StateFocusable uint32 = 11
	StateSensitive uint32 = 24
	StateShowing   uint32 = 25
	StateVisible   uint32 = 30
)

// Skip reports whether a node should be left out of the published tree. An
// element with no name says nothing a reader could announce and one with no
// area cannot be pointed at; either is a stop the user has to skip past.
func Skip(n toolkit.A11yNode) bool {
	return n.Name == "" || n.Rect.W <= 0 || n.Rect.H <= 0
}

// Nodes is the tree to publish for a widget root: every meaningful element, in
// visual order, with the ones no reader could use already removed.
func Nodes(root toolkit.Widget) []toolkit.A11yNode {
	all := toolkit.WalkA11y(root)
	out := all[:0:0]
	for _, n := range all {
		if Skip(n) {
			continue
		}
		out = append(out, n)
	}
	return out
}

// ScreenRect converts a node's window-relative rectangle to screen
// coordinates. AT-SPI asks for extents in BOTH spaces on the same interface,
// and only the back-end knows where the window sits, so the origin arrives from
// the caller.
//
// A Wayland client cannot know its own position on screen — the protocol does
// not tell it — so that back-end passes a zero origin and the two spaces
// coincide. Reporting a made-up screen position would be worse: a screen reader
// would point somewhere real and wrong.
func ScreenRect(n toolkit.A11yNode, originX, originY int) (x, y, w, h int32) {
	return int32(originX + n.Rect.X), int32(originY + n.Rect.Y),
		int32(n.Rect.W), int32(n.Rect.H)
}

// PressPoint encodes an element's centre, in the coordinates the input path
// speaks, so an activation can be replayed as an ordinary click.
//
// Carrying the point ON the element beats a table keyed by index: the tree is
// rebuilt whenever the frame changes, and an index goes stale the moment
// content scrolls under a screen-reader user's cursor.
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
