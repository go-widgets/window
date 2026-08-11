// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// Package dnd is the backend-agnostic drag-and-drop state machine that every
// native windowing backend shares. The toolkit defines the DnD primitives — the
// DragSource / DropTarget interfaces and the EventDragStart / EventDragMove /
// EventDragLeave / EventDrop lifecycle — but deliberately runs no pointer loop
// of its own, so it cannot originate a drag. Each backend's raw pointer stream
// carries only presses, drags and releases (EventClick / EventMouseDrag /
// EventMouseUp); until now only the wasmbox compositor host synthesized the
// lifecycle from them, so a real drag in an X11, Wayland, Cocoa or Win32 window
// did nothing.
//
// A Controller closes that gap ONCE, in a place all backends call: it watches
// the raw pointer events flowing to the root widget, hit-tests the widget tree
// for a DragSource under the press, and — once the pointer crosses a small
// threshold — begins a drag carrying that source's DragData, hit-testing for a
// DropTarget on each move to deliver EventDragStart / EventDragMove /
// EventDragLeave for feedback and, on release over an accepting target,
// EventDrop with the payload. When no DragSource is under the press it is inert:
// the raw events pass through untouched, so plain mouse behaviour is preserved.
//
// The controller depends only on the toolkit's public interfaces, so it holds
// no reference to any backend and every backend embeds the same instance.
package dnd

import "github.com/go-widgets/toolkit"

// Threshold is the pointer travel, in pixels, a press must exceed before a
// candidate drag becomes a live drag. Below it a press+move+release is a plain
// click, so a slightly shaky click never starts a drag. Manhattan distance on
// each axis (not Euclidean) — cheap and indistinguishable at this scale.
const Threshold = 4

// childLister is any widget that exposes its children, letting the controller
// descend a container tree to hit-test. The toolkit's containers (VBox, HBox,
// Grid, Frame, Container, …) all satisfy it via their exported Children method;
// a leaf widget does not, and simply is not descended into.
type childLister interface {
	Children() []toolkit.Widget
}

// Controller is the shared drag-and-drop state machine. The zero value is not
// usable; construct one with New and bind the root widget with Bind before
// feeding it events. It is single-goroutine (driven from the backend's event
// loop) and keeps no locks.
type Controller struct {
	root toolkit.Widget

	// armed is set when a press landed on a DragSource with a non-empty payload,
	// making this a drag candidate; dragging is set once the pointer travels past
	// Threshold, promoting the candidate to a live drag.
	armed    bool
	dragging bool
	payload  string

	// startX/startY is where the press landed (for the threshold test); target is
	// the DropTarget the drag is currently over (nil = none), and lastX/lastY is
	// the most recent position over it, so an EventDragLeave can be routed back to
	// that target when the pointer moves off it.
	startX, startY int
	target         toolkit.DropTarget
	lastX, lastY   int
}

// New returns an unbound Controller. Call Bind with the root widget before
// feeding it events.
func New() *Controller { return &Controller{} }

// Bind sets (or replaces) the root widget the controller hit-tests against. A
// backend calls it when it binds its root, mirroring where it stores root
// itself.
func (c *Controller) Bind(root toolkit.Widget) { c.root = root }

// Dragging reports whether a live drag is in progress (past the threshold). A
// backend may consult it to, e.g., suppress a hover cue or change the cursor.
func (c *Controller) Dragging() bool { return c.dragging }

// Process consumes one raw toolkit event and returns the events the backend
// should actually deliver to the root widget, in order. Presses, drags and
// releases are interpreted against the DnD state machine; every other kind (and
// every pointer event while no DragSource is involved) passes through unchanged,
// so a controller in the event path is transparent to all non-drag input.
func (c *Controller) Process(ev toolkit.Event) []toolkit.Event {
	switch ev.Kind {
	case toolkit.EventClick:
		return c.onPress(ev)
	case toolkit.EventMouseDrag:
		return c.onDrag(ev)
	case toolkit.EventMouseUp:
		return c.onUp(ev)
	default:
		return []toolkit.Event{ev}
	}
}

// onPress records a drag candidate when the press lands on a DragSource with a
// non-empty payload. The press itself is always delivered, so a widget that is
// both clickable and draggable (a Favoris row that selects on click and reorders
// on drag) still sees the click.
func (c *Controller) onPress(ev toolkit.Event) []toolkit.Event {
	c.reset()
	if src := c.dragSourceAt(ev.X, ev.Y); src != nil {
		if data := src.DragData(); data != "" {
			c.armed = true
			c.payload = data
			c.startX, c.startY = ev.X, ev.Y
		}
	}
	return []toolkit.Event{ev}
}

// onDrag advances a candidate/live drag. With no candidate it passes the raw
// drag through (plain behaviour preserved). Below the threshold it swallows the
// move. Past it, it hit-tests for a DropTarget and emits the enter/move/leave
// feedback lifecycle.
func (c *Controller) onDrag(ev toolkit.Event) []toolkit.Event {
	if !c.armed {
		return []toolkit.Event{ev}
	}
	if !c.dragging {
		if abs(ev.X-c.startX) < Threshold && abs(ev.Y-c.startY) < Threshold {
			return nil
		}
		c.dragging = true
	}
	var out []toolkit.Event
	tgt := c.dropTargetAt(ev.X, ev.Y)
	// Left the previous target (moved off it, or onto a different one): tell it so
	// it can clear its hover cue, routing the leave back to where it last was.
	if c.target != nil && tgt != c.target {
		out = append(out, toolkit.Event{Kind: toolkit.EventDragLeave, X: c.lastX, Y: c.lastY, Code: c.payload})
		c.target = nil
	}
	if tgt != nil {
		if c.target == nil {
			out = append(out, toolkit.Event{Kind: toolkit.EventDragStart, X: ev.X, Y: ev.Y, Code: c.payload})
			c.target = tgt
		} else {
			out = append(out, toolkit.Event{Kind: toolkit.EventDragMove, X: ev.X, Y: ev.Y, Code: c.payload})
		}
		c.lastX, c.lastY = ev.X, ev.Y
	}
	return out
}

// onUp completes the gesture. A candidate that never crossed the threshold is a
// plain click, so the release is delivered as-is. A live drag drops on the
// target under the release (leaving any different hovered target first) or, over
// no target, cancels with a leave. Either way the machine resets.
func (c *Controller) onUp(ev toolkit.Event) []toolkit.Event {
	if !c.armed {
		return []toolkit.Event{ev}
	}
	var out []toolkit.Event
	if c.dragging {
		tgt := c.dropTargetAt(ev.X, ev.Y)
		if c.target != nil && tgt != c.target {
			out = append(out, toolkit.Event{Kind: toolkit.EventDragLeave, X: c.lastX, Y: c.lastY, Code: c.payload})
		}
		if tgt != nil {
			out = append(out, toolkit.Event{Kind: toolkit.EventDrop, X: ev.X, Y: ev.Y, Code: c.payload})
		}
	} else {
		out = append(out, ev)
	}
	c.reset()
	return out
}

// reset returns the machine to its idle state.
func (c *Controller) reset() {
	c.armed = false
	c.dragging = false
	c.payload = ""
	c.target = nil
}

// dragSourceAt returns the deepest DragSource (with a non-empty-capable payload
// contract) under the point, or nil.
func (c *Controller) dragSourceAt(x, y int) toolkit.DragSource {
	w := deepestHit(c.root, x, y, func(n toolkit.Widget) bool {
		_, ok := n.(toolkit.DragSource)
		return ok
	})
	if w == nil {
		return nil
	}
	return w.(toolkit.DragSource)
}

// dropTargetAt returns the deepest DropTarget under the point that accepts the
// current payload, or nil.
func (c *Controller) dropTargetAt(x, y int) toolkit.DropTarget {
	payload := c.payload
	w := deepestHit(c.root, x, y, func(n toolkit.Widget) bool {
		dt, ok := n.(toolkit.DropTarget)
		return ok && dt.AcceptsDrop(payload)
	})
	if w == nil {
		return nil
	}
	return w.(toolkit.DropTarget)
}

// deepestHit walks the tree rooted at root and returns the deepest, topmost
// widget that both passes HitTest at the point and satisfies ok — the same
// widget the toolkit's own container routing would deliver a pointer event to.
// It descends into a child only when the child's bounds geometrically contain
// the point (following containment down the tree), and records a node only when
// its HitTest passes (so a transparent container that reports no hit is stepped
// over while its children are still considered). Pre-order assignment means a
// later sibling or a deeper descendant — drawn on top — overrides an earlier
// match.
func deepestHit(root toolkit.Widget, x, y int, ok func(toolkit.Widget) bool) toolkit.Widget {
	var found toolkit.Widget
	var walk func(toolkit.Widget)
	walk = func(n toolkit.Widget) {
		if n == nil {
			return
		}
		if n.HitTest(x, y) && ok(n) {
			found = n
		}
		if cl, isCont := n.(childLister); isCont {
			for _, child := range cl.Children() {
				if child != nil && child.Bounds().Contains(x, y) {
					walk(child)
				}
			}
		}
	}
	walk(root)
	return found
}

// abs is the integer absolute value used by the threshold test.
func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
