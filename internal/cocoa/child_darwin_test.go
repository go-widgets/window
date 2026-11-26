// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package cocoa

import (
	"errors"
	"testing"

	"github.com/go-macos/objc"
)

func objID(v uint64) objc.ID { return objc.ID(v) }

// A fake window with receiver ids that are not real objects: the registry and
// the close bookkeeping never message them (win is left zero so closeChildren
// sends nothing).
func fakeWin(view, deleg uint64) *Window {
	w := &Window{}
	w.view, w.deleg = objID(view), objID(deleg)
	return w
}

func TestRegistryRoutesAReceiverToItsWindow(t *testing.T) {
	old := active
	defer func() { active = old }()
	a, b := fakeWin(1, 2), fakeWin(3, 4)
	register(a)
	register(b)
	defer unregister(a)
	defer unregister(b)

	if lookup(objID(3)) != b || lookup(objID(2)) != a || lookup(objID(99)) != nil {
		t.Fatal("lookup does not map a receiver to its window")
	}
	if got := route(objID(3)); got != b || active != b {
		t.Fatalf("route(view of b) = %p, active %p; want b", got, active)
	}
	active = a
	if got := route(objID(99)); got != a {
		t.Fatal("an unknown receiver must fall back to active")
	}
	unregister(b)
	if lookup(objID(3)) != nil {
		t.Fatal("unregister left the view behind")
	}
}

func TestChildClosedIsIdempotentAndTellsTheOwnerOnce(t *testing.T) {
	old := active
	defer func() { active = old }()
	p, c := fakeWin(1, 2), fakeWin(3, 4)
	calls := 0
	c.parent, c.onClose = p, func() { calls++ }
	p.children = []*Window{c}
	register(c)
	active = c

	c.childClosed()
	c.childClosed()
	if calls != 1 {
		t.Fatalf("onClose called %d times, want 1", calls)
	}
	if len(p.children) != 0 || active != p || !c.closed || !c.gone.Load() {
		t.Fatalf("child not retired: children=%d active=%p closed=%v", len(p.children), active, c.closed)
	}
	if lookup(objID(3)) != nil {
		t.Fatal("child still routable")
	}
	// A second goodbye must do nothing at all, not merely stay quiet: put the
	// child back in its parent's list and see it left there.
	p.children = []*Window{c}
	c.childClosed()
	if len(p.children) != 1 {
		t.Fatal("a second childClosed retired the child again")
	}
}

func TestAChildsCloseCallbacksLeaveTheParentRunning(t *testing.T) {
	old := active
	defer func() { active = old }()
	for name, closeIt := range map[string]func(*Window){
		"windowShouldClose": func(c *Window) {
			if !windowShouldClose(c.view, 0, 0) {
				t.Error("a child's close must be allowed")
			}
		},
		"viewCloseNow": func(c *Window) { viewCloseNow(c.view, 0) },
	} {
		p, c := fakeWin(1, 2), fakeWin(3, 4)
		n := 0
		c.child, c.parent, c.onClose = true, p, func() { n++ }
		p.children = []*Window{c}
		register(p)
		register(c)
		closeIt(c)
		if n != 1 || !c.closed || p.closed || p.gone.Load() || len(p.children) != 0 {
			t.Errorf("%s: onClose %d, child closed %v, parent closed %v, children %d", name, n, c.closed, p.closed, len(p.children))
		}
		unregister(p)
		unregister(c)
	}
}

func TestOpenChildReportsAFailedCreation(t *testing.T) {
	old := makeWindow
	defer func() { makeWindow = old }()
	boom := errors.New("no window")
	// A window comes back WITH the error, so that ignoring the error is seen as
	// a wrong answer rather than as a nil dereference.
	makeWindow = func(Options, bool) (*Window, error) { return fakeWin(7, 8), boom }
	p := fakeWin(1, 2)
	if _, err := p.OpenChild(Options{}, nil, nil); err != boom {
		t.Fatalf("err = %v, want the creation error", err)
	}
	if len(p.children) != 0 {
		t.Fatal("a window that was never made was listed")
	}
}

func TestCloseChildrenRetiresEveryChild(t *testing.T) {
	p := fakeWin(1, 2)
	n := 0
	for i := uint64(0); i < 3; i++ {
		c := fakeWin(10+i, 20+i)
		c.parent, c.onClose = p, func() { n++ }
		p.children = append(p.children, c)
	}
	p.closeChildren()
	if n != 3 || len(p.children) != 0 {
		t.Fatalf("closed %d, left %d", n, len(p.children))
	}
}

func TestOpenChildOfAClosedWindowIsRefused(t *testing.T) {
	old := makeWindow
	defer func() { makeWindow = old }()
	makeWindow = func(Options, bool) (*Window, error) {
		t.Error("a window was made for a parent that is closed")
		return nil, errors.New("unreachable")
	}
	if _, err := (*Window)(nil).OpenChild(Options{}, nil, nil); err != ErrClosed {
		t.Fatalf("nil parent: %v", err)
	}
	w := &Window{closed: true}
	if _, err := w.OpenChild(Options{}, nil, nil); err != ErrClosed {
		t.Fatalf("closed parent: %v", err)
	}
}
