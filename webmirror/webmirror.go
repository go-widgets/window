// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// Package webmirror feeds go-widgets/webcanvas's accessibility mirror and text
// layer from a toolkit widget tree. It is the one place a toolkit.A11yNode
// becomes a webcanvas.A11yNode, shared by the browser-tab backend of this
// module and by any other scene that draws on a webcanvas (the gallery).
//
// It is plain Go with no build tag: the conversion is tested natively, and only
// webcanvas itself touches the DOM.
//
// What it publishes follows internal/a11yrole, the rule the native bridges
// share: a node with no area is dropped, a static node with no name is dropped,
// and a nameless interactive node (a text field) is kept so a screen reader
// still announces the control. Names and values are passed through verbatim;
// webcanvas writes them with textContent and setAttribute only, so nothing here
// builds markup.
package webmirror

import (
	"github.com/go-widgets/textinput"
	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/webcanvas"
	"github.com/go-widgets/window/internal/a11yrole"
)

// Source is one widget tree a scene publishes. A scene made of several
// independent roots (the gallery draws a menu bar, three columns and a status
// bar, each its own tree) gives each a distinct Prefix, because every tree's
// own keys start "<Type>#0" and two trees of the same type would collide.
type Source struct {
	// Prefix is put before the tree's keys ("<Prefix>|<key>"). Empty for a
	// scene with a single tree: the toolkit's keys are then published as they
	// are.
	Prefix string
	// Root is the tree.
	Root toolkit.Widget
}

// Single is the sources of a scene with one tree.
func Single(root toolkit.Widget) []Source { return []Source{{Root: root}} }

// Host implements [webcanvas.A11yAware] and [webcanvas.TextInputHost] over the
// trees Sources returns. Embed it in a scene, or call it from the scene's two
// methods. Sources is asked on every call, so a scene whose roots change (a
// dialog opened, a tab switched) needs no extra wiring.
type Host struct {
	Sources func() []Source
}

var (
	_ webcanvas.A11yAware     = Host{}
	_ webcanvas.TextInputHost = Host{}
)

// A11yNodes returns the accessible nodes of every source, in reading order.
func (h Host) A11yNodes() []webcanvas.A11yNode {
	if h.Sources == nil {
		return nil
	}
	return Nodes(h.Sources()...)
}

// TextInputFor returns the text-input client of the widget published under
// key, or nil when that widget is not a text-input client (an Entry is not one
// yet) or the key is unknown.
func (h Host) TextInputFor(key string) textinput.Client {
	if h.Sources == nil {
		return nil
	}
	return TextInput(key, h.Sources()...)
}

// Nodes converts the accessibility trees of srcs into webcanvas nodes.
func Nodes(srcs ...Source) []webcanvas.A11yNode {
	var out []webcanvas.A11yNode
	for _, s := range srcs {
		if s.Root == nil {
			continue
		}
		for _, n := range a11yrole.Nodes(s.Root) {
			out = append(out, Convert(n, s.Prefix))
		}
	}
	return out
}

// Convert is the one conversion from a toolkit node to a webcanvas node. The
// role is the WAI-ARIA name the toolkit role already is; the value keeps the
// toolkit's words ("checked", "pressed", "selected"), which webcanvas maps to
// aria-checked and aria-pressed.
func Convert(n toolkit.A11yNode, prefix string) webcanvas.A11yNode {
	return webcanvas.A11yNode{
		Key:       Key(prefix, n.Key),
		Role:      string(n.Role),
		Name:      n.Name,
		Value:     n.Value,
		X:         n.Rect.X,
		Y:         n.Rect.Y,
		W:         n.Rect.W,
		H:         n.Rect.H,
		Focused:   n.Focused,
		Disabled:  n.Disabled,
		ReadOnly:  n.ReadOnly,
		Multiline: n.Multiline,
		HasRange:  n.HasRange,
		Min:       n.Min,
		Max:       n.Max,
		Now:       n.Now,
	}
}

// Key is the published key of a toolkit key under prefix.
func Key(prefix, key string) string {
	if prefix == "" {
		return key
	}
	return prefix + "|" + key
}

// TextInput returns the textinput.Client of the widget published under key in
// srcs, or nil. The toolkit walk reports nodes, not widgets, so the widgets are
// collected by the same traversal (same order, same filter) and paired with
// the nodes by position; if the two ever disagree the answer is nil and the
// node stays a plain textbox, never the wrong widget.
func TextInput(key string, srcs ...Source) textinput.Client {
	for _, s := range srcs {
		if s.Root == nil {
			continue
		}
		nodes := toolkit.WalkA11y(s.Root)
		ws := accessibleWidgets(s.Root)
		if len(ws) != len(nodes) {
			continue
		}
		for i, n := range nodes {
			if Key(s.Prefix, n.Key) != key {
				continue
			}
			if info := ws[i].(toolkit.Accessible).A11y(); info.Role != n.Role {
				return nil
			}
			c, _ := ws[i].(textinput.Client)
			return c
		}
	}
	return nil
}

// childContainer is the interface toolkit.WalkA11y descends through.
type childContainer interface{ Children() []toolkit.Widget }

// accessibleWidgets is the widgets toolkit.WalkA11y turns into nodes, in the
// same order: accessible, and not RolePresentation, descending through every
// child container.
func accessibleWidgets(root toolkit.Widget) []toolkit.Widget {
	var out []toolkit.Widget
	var walk func(w toolkit.Widget)
	walk = func(w toolkit.Widget) {
		if w == nil {
			return
		}
		if a, ok := w.(toolkit.Accessible); ok && a.A11y().Role != toolkit.RolePresentation {
			out = append(out, w)
		}
		if c, ok := w.(childContainer); ok {
			for _, k := range c.Children() {
				walk(k)
			}
		}
	}
	walk(root)
	return out
}
