// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package cocoa

import (
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
	if _, err := (*Window)(nil).OpenChild(Options{}, nil, nil); err != ErrClosed {
		t.Fatalf("nil parent: %v", err)
	}
	w := &Window{closed: true}
	if _, err := w.OpenChild(Options{}, nil, nil); err != ErrClosed {
		t.Fatalf("closed parent: %v", err)
	}
}
