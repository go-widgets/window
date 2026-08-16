// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// The OS-independent half of the macOS accessibility bridge: which AX role
// stands for which toolkit role, which elements are worth publishing, where an
// element sits in the view's own coordinates, and how a press finds its way
// back to a click.
//
// It is deliberately free of AppKit so the whole decision layer is exercised by
// ordinary tests on any host — the same split cocoa uses for input mapping,
// where mapping.go is proven to 100% on the Linux lane and only the objc glue
// in cocoa_darwin.go waits for a real Mac.

package cocoa

import (
	"encoding/binary"
	"hash/fnv"
	"strconv"
	"strings"

	"github.com/go-widgets/toolkit"
)

// a11yTreeSig is a cheap, order-sensitive signature of the accessibility tree's
// structure, content and geometry: every node's role, name, value and rectangle.
// It touches no Cocoa API, so it is safe to recompute every frame; an unchanged
// signature means the NSAccessibilityElements already published are still correct
// and the per-node Cocoa rebuild (buildA11yChildren) can be skipped — the gate
// that keeps a continuously repainting window from flooding the accessibility
// server with identical trees.
func a11yTreeSig(nodes []toolkit.A11yNode) uint64 {
	h := fnv.New64a()
	var buf [8]byte
	putInt := func(v int) {
		binary.LittleEndian.PutUint64(buf[:], uint64(int64(v)))
		_, _ = h.Write(buf[:])
	}
	for _, n := range nodes {
		_, _ = h.Write([]byte(n.Role))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(n.Name))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(n.Value))
		_, _ = h.Write([]byte{0})
		putInt(n.Rect.X)
		putInt(n.Rect.Y)
		putInt(n.Rect.W)
		putInt(n.Rect.H)
	}
	return h.Sum64()
}

// AXRole maps a toolkit role to the NSAccessibility role a screen reader
// announces. Anything unrecognised becomes AXGroup — AppKit's "something whose
// meaning is its contents", the honest answer for a role this table does not
// know, and never a wrong announcement.
func AXRole(r toolkit.Role) string {
	switch r {
	case toolkit.RoleButton:
		return "AXButton"
	case toolkit.RoleText, toolkit.RoleStatus:
		return "AXStaticText"
	case toolkit.RoleTextbox, toolkit.RoleSearchbox:
		return "AXTextField"
	case toolkit.RoleCheckbox, toolkit.RoleSwitch:
		return "AXCheckBox"
	case toolkit.RoleRadio:
		return "AXRadioButton"
	case toolkit.RoleSlider, toolkit.RoleSpinbutton:
		return "AXSlider"
	case toolkit.RoleImg:
		return "AXImage"
	case toolkit.RoleList, toolkit.RoleListbox:
		return "AXList"
	case toolkit.RoleGrid:
		return "AXTable"
	case toolkit.RoleToolbar:
		return "AXToolbar"
	case toolkit.RoleMenu:
		return "AXMenu"
	case toolkit.RoleMenuBar:
		return "AXMenuBar"
	case toolkit.RoleProgressbar, toolkit.RoleMeter:
		return "AXProgressIndicator"
	case toolkit.RoleCombobox:
		return "AXComboBox"
	case toolkit.RoleAlert, toolkit.RoleDialog:
		return "AXSheet"
	case toolkit.RoleTooltip:
		return "AXHelpTag"
	default:
		return "AXGroup"
	}
}

// A11ySkip reports whether a node should be left out of the published tree.
//
// An element with no name says nothing a reader could announce, and one with no
// area cannot be pointed at; either lands in VoiceOver's rotor as an unlabelled
// stop the user has to skip past for nothing. The toolkit deliberately does not
// make this decision — it reports faithfully — because it is the platform that
// knows what its own screen reader does with an empty element.
func A11ySkip(n toolkit.A11yNode) bool {
	return n.Name == "" || n.Rect.W <= 0 || n.Rect.H <= 0
}

// A11yFrame converts a node's rectangle from the framebuffer's render pixels to
// the content view's POINTS.
//
// Only a scale division is needed, with no Y flip: the content view is flipped
// (isFlipped → top-left origin) precisely so it matches the framebuffer, which
// is also what lets DirtyRect stay this simple. The trip on to screen
// coordinates — where NSAccessibilityElement wants its frame, bottom-left
// origin and all — is left to the caller, because it needs live Cocoa state
// (the view's own convertRect: chain) and so cannot be decided here.
func A11yFrame(n toolkit.A11yNode, scale float64) (x, y, w, h float64) {
	if scale <= 0 {
		scale = 1
	}
	return float64(n.Rect.X) / scale, float64(n.Rect.Y) / scale,
		float64(n.Rect.W) / scale, float64(n.Rect.H) / scale
}

// PressPoint encodes an element's centre, in the render pixels the input path
// speaks, as the string carried on its accessibilityIdentifier.
//
// Stashing the point ON the element beats holding a Go-side table keyed by
// index: the tree is rebuilt whenever the frame changes, and an index would go
// stale the moment content scrolled under a VoiceOver user's cursor — pressing
// then activates whatever moved into that slot.
func PressPoint(n toolkit.A11yNode) string {
	return strconv.Itoa(n.Rect.X+n.Rect.W/2) + "," + strconv.Itoa(n.Rect.Y+n.Rect.H/2)
}

// ParsePressPoint reads back what PressPoint wrote.
//
// A malformed identifier REFUSES rather than defaulting to (0,0): the origin is
// a real, clickable place — usually the first control in the window — so a
// silent fallback would turn a failed lookup into a wrong button press.
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
