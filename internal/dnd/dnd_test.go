// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package dnd

import (
	"reflect"
	"testing"

	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/toolkit/scene"
)

// --- fake widget tree ------------------------------------------------------

// fakeSource is a leaf DragSource carrying a fixed payload (empty payload =
// "not really draggable", which the controller must ignore).
type fakeSource struct {
	toolkit.Base
	payload string
}

func (s *fakeSource) DragData() string { return s.payload }

// fakeTarget is a leaf DropTarget. accept decides whether a payload is taken;
// nil means "accept any non-empty payload".
type fakeTarget struct {
	toolkit.Base
	accept func(string) bool
}

func (t *fakeTarget) AcceptsDrop(payload string) bool {
	if t.accept != nil {
		return t.accept(payload)
	}
	return payload != ""
}

// fakeContainer exposes its children so the controller can descend it.
type fakeContainer struct {
	toolkit.Base
	kids []toolkit.Widget
}

func (c *fakeContainer) Children() []toolkit.Widget { return c.kids }

// nestedTarget is a DropTarget container: both a droppable itself AND a parent,
// used to prove the deepest match wins.
type nestedTarget struct {
	fakeTarget
	kids []toolkit.Widget
}

func (n *nestedTarget) Children() []toolkit.Widget { return n.kids }

// transparent is a container whose HitTest always fails (a scrim/box) though its
// bounds contain the point — the walk must step over it yet still consider its
// children.
type transparent struct {
	fakeContainer
}

func (t *transparent) HitTest(px, py int) bool { return false }

func rect(x, y, w, h int) toolkit.Rect { return toolkit.Rect{X: x, Y: y, W: w, H: h} }

func at(w toolkit.Widget, r toolkit.Rect) toolkit.Widget { w.SetBounds(r); return w }

// tree builds a standard scene and returns the controller bound to it plus the
// widgets tests assert against.
func newScene() (*Controller, *fakeSource, *fakeTarget, *fakeTarget, *fakeTarget) {
	src := &fakeSource{payload: "file:/a"}
	src.SetBounds(rect(0, 0, 50, 50))
	empty := &fakeSource{payload: ""} // draggable interface, but no payload
	empty.SetBounds(rect(0, 100, 50, 50))
	tgtA := &fakeTarget{}
	tgtA.SetBounds(rect(100, 0, 50, 50))
	tgtB := &fakeTarget{}
	tgtB.SetBounds(rect(100, 100, 50, 50))
	reject := &fakeTarget{accept: func(string) bool { return false }}
	reject.SetBounds(rect(160, 160, 30, 30))
	root := &fakeContainer{kids: []toolkit.Widget{src, empty, tgtA, tgtB, reject}}
	root.SetBounds(rect(0, 0, 200, 200))
	c := New()
	c.Bind(root)
	return c, src, tgtA, tgtB, reject
}

// kinds extracts the event kinds from a slice for compact assertions.
func kinds(evs []toolkit.Event) []toolkit.EventKind {
	out := make([]toolkit.EventKind, len(evs))
	for i, e := range evs {
		out[i] = e.Kind
	}
	return out
}

func eq(t *testing.T, got, want []toolkit.EventKind) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("event kinds = %v, want %v", got, want)
	}
}

// --- tests -----------------------------------------------------------------

// TestFullDragDrop drives press → sub-threshold jitter → cross threshold onto A
// → move on A → cross onto B → drop on B, asserting the exact lifecycle and
// payload at each step.
func TestFullDragDrop(t *testing.T) {
	c, _, _, _, _ := newScene()

	// Press on the source: the click is always delivered, and the machine arms.
	press := c.Process(toolkit.Event{Kind: toolkit.EventClick, X: 10, Y: 10})
	eq(t, kinds(press), []toolkit.EventKind{toolkit.EventClick})
	if c.Dragging() {
		t.Fatal("Dragging() true before threshold crossed")
	}

	// Sub-threshold jitter: swallowed entirely.
	if got := c.Process(toolkit.Event{Kind: toolkit.EventMouseDrag, X: 11, Y: 11}); got != nil {
		t.Fatalf("sub-threshold drag delivered %v, want nil", got)
	}
	if c.Dragging() {
		t.Fatal("Dragging() true on sub-threshold move")
	}

	// Cross the threshold onto target A: EventDragStart with the payload.
	d1 := c.Process(toolkit.Event{Kind: toolkit.EventMouseDrag, X: 120, Y: 20})
	eq(t, kinds(d1), []toolkit.EventKind{toolkit.EventDragStart})
	if d1[0].Code != "file:/a" || d1[0].X != 120 || d1[0].Y != 20 {
		t.Fatalf("DragStart = %+v, want payload+pos 120,20", d1[0])
	}
	if !c.Dragging() {
		t.Fatal("Dragging() false after crossing threshold")
	}

	// Move still over A: EventDragMove.
	d2 := c.Process(toolkit.Event{Kind: toolkit.EventMouseDrag, X: 125, Y: 25})
	eq(t, kinds(d2), []toolkit.EventKind{toolkit.EventDragMove})
	if d2[0].Code != "file:/a" {
		t.Fatalf("DragMove payload = %q", d2[0].Code)
	}

	// Move onto B: leave A (routed to A's last position 125,25) then start B.
	d3 := c.Process(toolkit.Event{Kind: toolkit.EventMouseDrag, X: 120, Y: 120})
	eq(t, kinds(d3), []toolkit.EventKind{toolkit.EventDragLeave, toolkit.EventDragStart})
	if d3[0].X != 125 || d3[0].Y != 25 {
		t.Fatalf("DragLeave pos = %d,%d, want last-A 125,25", d3[0].X, d3[0].Y)
	}

	// Release over B: EventDrop with payload; machine resets.
	up := c.Process(toolkit.Event{Kind: toolkit.EventMouseUp, X: 120, Y: 120})
	eq(t, kinds(up), []toolkit.EventKind{toolkit.EventDrop})
	if up[0].Code != "file:/a" || up[0].X != 120 || up[0].Y != 120 {
		t.Fatalf("Drop = %+v", up[0])
	}
	if c.Dragging() {
		t.Fatal("Dragging() true after drop")
	}
}

// TestCancelReleaseOffTarget: a drag that starts on a target then releases over
// empty space leaves the target and drops nothing.
func TestCancelReleaseOffTarget(t *testing.T) {
	c, _, _, _, _ := newScene()
	c.Process(toolkit.Event{Kind: toolkit.EventClick, X: 10, Y: 10})
	c.Process(toolkit.Event{Kind: toolkit.EventMouseDrag, X: 120, Y: 20}) // start A
	up := c.Process(toolkit.Event{Kind: toolkit.EventMouseUp, X: 70, Y: 70})
	eq(t, kinds(up), []toolkit.EventKind{toolkit.EventDragLeave})
}

// TestDropDifferentTargetThanHovered: hover A then release directly over B —
// leave A, drop B.
func TestDropDifferentTargetThanHovered(t *testing.T) {
	c, _, _, _, _ := newScene()
	c.Process(toolkit.Event{Kind: toolkit.EventClick, X: 10, Y: 10})
	c.Process(toolkit.Event{Kind: toolkit.EventMouseDrag, X: 120, Y: 20}) // start A
	up := c.Process(toolkit.Event{Kind: toolkit.EventMouseUp, X: 120, Y: 120})
	eq(t, kinds(up), []toolkit.EventKind{toolkit.EventDragLeave, toolkit.EventDrop})
	if up[1].Code != "file:/a" {
		t.Fatalf("drop payload %q", up[1].Code)
	}
}

// TestDropNeverHoveredTarget: cross the threshold over empty space (no target),
// then release onto a target never entered — a bare drop, no leave.
func TestDropNeverHoveredTarget(t *testing.T) {
	c, _, _, _, _ := newScene()
	c.Process(toolkit.Event{Kind: toolkit.EventClick, X: 10, Y: 10})
	d := c.Process(toolkit.Event{Kind: toolkit.EventMouseDrag, X: 10, Y: 60}) // empty, crosses threshold
	if d != nil {
		t.Fatalf("drag over empty space delivered %v, want nil", d)
	}
	up := c.Process(toolkit.Event{Kind: toolkit.EventMouseUp, X: 120, Y: 20})
	eq(t, kinds(up), []toolkit.EventKind{toolkit.EventDrop})
}

// TestClickNoDrag: press+release on a source without moving is a plain click —
// the click and the mouse-up both pass through, no DnD events.
func TestClickNoDrag(t *testing.T) {
	c, _, _, _, _ := newScene()
	press := c.Process(toolkit.Event{Kind: toolkit.EventClick, X: 10, Y: 10})
	eq(t, kinds(press), []toolkit.EventKind{toolkit.EventClick})
	up := c.Process(toolkit.Event{Kind: toolkit.EventMouseUp, X: 12, Y: 12})
	eq(t, kinds(up), []toolkit.EventKind{toolkit.EventMouseUp})
}

// TestNoSourcePassthrough: pressing/dragging/releasing where no DragSource lives
// preserves the plain pointer stream untouched.
func TestNoSourcePassthrough(t *testing.T) {
	c, _, _, _, _ := newScene()
	press := c.Process(toolkit.Event{Kind: toolkit.EventClick, X: 60, Y: 60})
	eq(t, kinds(press), []toolkit.EventKind{toolkit.EventClick})
	drag := c.Process(toolkit.Event{Kind: toolkit.EventMouseDrag, X: 65, Y: 65})
	eq(t, kinds(drag), []toolkit.EventKind{toolkit.EventMouseDrag})
	up := c.Process(toolkit.Event{Kind: toolkit.EventMouseUp, X: 65, Y: 65})
	eq(t, kinds(up), []toolkit.EventKind{toolkit.EventMouseUp})
}

// TestEmptyPayloadNotDraggable: a DragSource whose DragData is empty is treated
// as non-draggable — the drag stream passes through.
func TestEmptyPayloadNotDraggable(t *testing.T) {
	c, _, _, _, _ := newScene()
	c.Process(toolkit.Event{Kind: toolkit.EventClick, X: 10, Y: 110}) // on the empty-payload source
	drag := c.Process(toolkit.Event{Kind: toolkit.EventMouseDrag, X: 120, Y: 110})
	eq(t, kinds(drag), []toolkit.EventKind{toolkit.EventMouseDrag})
}

// TestRejectingTarget: a drag over a target that rejects the payload yields no
// lifecycle, and releasing over it drops nothing.
func TestRejectingTarget(t *testing.T) {
	c, _, _, _, _ := newScene()
	c.Process(toolkit.Event{Kind: toolkit.EventClick, X: 10, Y: 10})
	d := c.Process(toolkit.Event{Kind: toolkit.EventMouseDrag, X: 170, Y: 170}) // over reject
	if d != nil {
		t.Fatalf("drag over rejecting target delivered %v, want nil", d)
	}
	up := c.Process(toolkit.Event{Kind: toolkit.EventMouseUp, X: 170, Y: 170})
	if up != nil {
		t.Fatalf("release over rejecting target delivered %v, want nil (cancel)", up)
	}
}

// TestPassthroughOtherKinds: non-pointer kinds (and pointer kinds unrelated to
// the machine) flow straight through.
func TestPassthroughOtherKinds(t *testing.T) {
	c, _, _, _, _ := newScene()
	for _, k := range []toolkit.EventKind{toolkit.EventScroll, toolkit.EventKeyDown, toolkit.EventMouseMove} {
		got := c.Process(toolkit.Event{Kind: k})
		eq(t, kinds(got), []toolkit.EventKind{k})
	}
}

// TestNegativeThresholdAxis: a drag moving up-and-left crosses the threshold via
// the negative axis, exercising abs on negative deltas.
func TestNegativeThresholdAxis(t *testing.T) {
	src := &fakeSource{payload: "p"}
	src.SetBounds(rect(100, 100, 50, 50))
	tgt := &fakeTarget{}
	tgt.SetBounds(rect(0, 0, 40, 40))
	root := &fakeContainer{kids: []toolkit.Widget{src, tgt}}
	root.SetBounds(rect(0, 0, 200, 200))
	c := New()
	c.Bind(root)
	c.Process(toolkit.Event{Kind: toolkit.EventClick, X: 120, Y: 120})
	d := c.Process(toolkit.Event{Kind: toolkit.EventMouseDrag, X: 20, Y: 20}) // moved -100,-100
	eq(t, kinds(d), []toolkit.EventKind{toolkit.EventDragStart})
}

// TestDeepestAndTopmostWin proves hit-testing picks the deepest descendant and,
// among overlapping siblings, the later-drawn one; and that a transparent
// container is stepped over while its children are still hit-tested.
func TestDeepestAndTopmostWin(t *testing.T) {
	// Nested target: outer is a DropTarget AND parent of an inner DropTarget that
	// covers the same point — the inner (deeper) must win.
	inner := &fakeTarget{}
	inner.SetBounds(rect(10, 10, 20, 20))
	outer := &nestedTarget{kids: []toolkit.Widget{inner}}
	outer.SetBounds(rect(0, 0, 50, 50))

	// A transparent scrim over the inner: HitTest fails, so it is not the target,
	// but it must not block descent to its own droppable child either.
	scrimChild := &fakeTarget{}
	scrimChild.SetBounds(rect(10, 10, 20, 20))
	scrim := &transparent{fakeContainer{kids: []toolkit.Widget{scrimChild}}}
	scrim.SetBounds(rect(0, 0, 50, 50))

	src := &fakeSource{payload: "x"}
	src.SetBounds(rect(100, 100, 10, 10))

	// A container with a nil child and an out-of-range child, to exercise both
	// skip branches of the descent.
	far := &fakeTarget{}
	far.SetBounds(rect(300, 300, 10, 10))
	holder := &fakeContainer{kids: []toolkit.Widget{nil, far, outer, scrim}}
	holder.SetBounds(rect(0, 0, 400, 400))

	c := New()
	c.Bind(&fakeContainer{Base: baseAt(rect(0, 0, 400, 400)), kids: []toolkit.Widget{src, holder}})
	c.payload = "x"

	// Point 15,15 lies in inner, outer, scrimChild and scrim. The last matching
	// node in pre-order (scrimChild, under the later sibling scrim) wins.
	got := c.dropTargetAt(15, 15)
	if got != toolkit.DropTarget(scrimChild) {
		t.Fatalf("dropTargetAt = %v, want scrimChild (deepest, topmost)", got)
	}
}

// baseAt returns a Base pre-positioned, for inline construction.
func baseAt(r toolkit.Rect) toolkit.Base {
	var b toolkit.Base
	b.SetBounds(r)
	return b
}

// TestNilRoot: a controller whose root is nil (never bound) hit-tests to nothing
// and simply passes events through.
func TestNilRoot(t *testing.T) {
	c := New()
	press := c.Process(toolkit.Event{Kind: toolkit.EventClick, X: 1, Y: 1})
	eq(t, kinds(press), []toolkit.EventKind{toolkit.EventClick})
	if c.dragSourceAt(1, 1) != nil {
		t.Fatal("dragSourceAt on nil root should be nil")
	}
	if c.dropTargetAt(1, 1) != nil {
		t.Fatal("dropTargetAt on nil root should be nil")
	}
}

// TestSceneHostRootUnwrap proves DnD works under the standard damage-aware
// desktop root: scene.HostRoot hides its wrapped tree (it exposes no Children),
// so the controller must unwrap it via Scene() to hit-test the real widgets. A
// full press→drag→drop through a HostRoot must still deliver EventDrop with the
// payload.
func TestSceneHostRootUnwrap(t *testing.T) {
	src := &fakeSource{payload: "host:/f"}
	src.SetBounds(rect(0, 0, 60, 40))
	tgt := &fakeTarget{}
	tgt.SetBounds(rect(0, 40, 60, 40))
	inner := &fakeContainer{kids: []toolkit.Widget{src, tgt}}
	inner.SetBounds(rect(0, 0, 60, 80))

	host := scene.NewHostRoot(inner)
	host.SetBounds(rect(0, 0, 60, 80))

	c := New()
	c.Bind(host)

	c.Process(toolkit.Event{Kind: toolkit.EventClick, X: 30, Y: 20}) // press on source
	d := c.Process(toolkit.Event{Kind: toolkit.EventMouseDrag, X: 30, Y: 60})
	eq(t, kinds(d), []toolkit.EventKind{toolkit.EventDragStart})
	up := c.Process(toolkit.Event{Kind: toolkit.EventMouseUp, X: 30, Y: 60})
	eq(t, kinds(up), []toolkit.EventKind{toolkit.EventDrop})
	if up[0].Code != "host:/f" {
		t.Fatalf("drop through HostRoot carried %q, want host:/f", up[0].Code)
	}
}

// TestBindReplacesRoot: Bind can be called again to point at a new tree.
func TestBindReplacesRoot(t *testing.T) {
	c := New()
	first := &fakeContainer{}
	first.SetBounds(rect(0, 0, 10, 10))
	c.Bind(first)
	second, src, _, _, _ := newScene()
	_ = second
	c.Bind(src) // rebinding to the source alone
	// A press directly on the source (now the root) arms a drag.
	c.Process(toolkit.Event{Kind: toolkit.EventClick, X: 10, Y: 10})
	d := c.Process(toolkit.Event{Kind: toolkit.EventMouseDrag, X: 10, Y: 40})
	// No target in this single-widget tree, so the drag is swallowed (armed, past
	// threshold, no DropTarget).
	if d != nil {
		t.Fatalf("drag with no target delivered %v, want nil", d)
	}
	if !c.Dragging() {
		t.Fatal("expected an active drag after rebinding to the source")
	}
}
