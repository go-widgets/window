// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// Package a11yrole holds the one decision the three native accessibility
// bridges (cocoa, win32, atspi) must agree on: which nodes of the toolkit's
// accessibility tree are published when they carry no accessible name.
//
// It is pure Go with no OS dependency, so the rule is unit-tested at 100% on
// the Linux lane whatever platform a bridge targets.
//
// The rule. A nameless node is dropped only when its role cannot take focus or
// input. A static, nameless element says nothing a reader could announce and is
// an unlabelled stop for nothing. A nameless FIELD is different: the user can
// still focus it and type into it, and each platform announces such a control
// from its role plus its value. Sources read when this was written:
//
//   - macOS: AppKit's NSAccessibilityProtocols.h (macOS SDK) declares
//     accessibilityLabel as nullable and carries a control's content in
//     accessibilityValue (NSAccessibilityStaticText, Slider, Stepper). The
//     cocoa bridge therefore sets the label only when there is a name, and
//     always sets the value.
//   - Windows: "UI Automation Support for the Edit Control Type"
//     (MicrosoftDocs/win32, uiauto-supporteditcontroltype.md) says the Name
//     of an edit control comes from its static label, and "should never
//     contain the textual contents of the edit control"; the contents are the
//     Value pattern's Value. The win32 bridge keeps Name empty and answers
//     the Value property.
//   - Linux: at-spi2-core's Accessible.xml says an interactive object with no
//     Name or labelled-by relation means "something is missing from your
//     application". The atspi bridge publishes no Text interface yet, so
//     Label falls back to the value rather than leave Orca with a bare role.
package a11yrole

import "github.com/go-widgets/toolkit"

// Interactive reports whether a node of this role can take focus or input, so
// that it must stay in the published tree even without a name. It follows the
// WAI-ARIA roles the toolkit's roles are named after: widgets and composite
// widgets are focusable; structure, landmarks and static content are not.
func Interactive(r toolkit.Role) bool {
	switch r {
	case toolkit.RoleTextbox, toolkit.RoleSearchbox, toolkit.RoleCombobox,
		toolkit.RoleSlider, toolkit.RoleSpinbutton,
		toolkit.RoleButton, toolkit.RoleCheckbox, toolkit.RoleRadio,
		toolkit.RoleSwitch, toolkit.RoleLink, toolkit.RoleTab,
		toolkit.RoleListbox, toolkit.RoleTree, toolkit.RoleGrid,
		toolkit.RoleSeparator:
		return true
	}
	return false
}

// Skip reports whether a node is left out of the published tree: it has no area
// to point at, or it has no name and cannot take focus or input.
func Skip(n toolkit.A11yNode) bool {
	if n.Rect.W <= 0 || n.Rect.H <= 0 {
		return true
	}
	return n.Name == "" && !Interactive(n.Role)
}

// Nodes returns the nodes of root that Skip keeps, in visual order.
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

// Label is the accessible name a bridge that has no separate value channel
// publishes: the node's Name, or, for an interactive node with none, its Value,
// so the field's content is still announced. A static node never borrows its
// value as a name.
func Label(n toolkit.A11yNode) string {
	if n.Name != "" || !Interactive(n.Role) {
		return n.Name
	}
	return n.Value
}
