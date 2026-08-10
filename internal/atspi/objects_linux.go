// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// The objects exported on the accessibility bus: the application root, one
// object per element, and the cache a client actually reads the tree through.
//
// The interface signatures were taken from a running GTK application over the
// same bus rather than from documentation, so they match what a client really
// calls.
//
//go:build linux

package atspi

import (
	"github.com/go-freedesktop/dbus"
	"github.com/go-widgets/toolkit"
)

// accRoot is the application object: the thing a screen reader finds when it
// enumerates what is running, and the parent of every element.
type accRoot struct{ b *Bridge }

func (r *accRoot) GetRole() (uint32, *dbus.Error)     { return RoleApplication, nil }
func (r *accRoot) GetRoleName() (string, *dbus.Error) { return "application", nil }
func (r *accRoot) GetLocalizedRoleName() (string, *dbus.Error) {
	return "application", nil
}

func (r *accRoot) GetChildAtIndex(i int32) (Ref, *dbus.Error) {
	if _, ok := r.b.at(int(i)); !ok {
		return nullRef, nil
	}
	return r.b.ref(childPath(int(i))), nil
}

func (r *accRoot) GetChildren() ([]Ref, *dbus.Error) {
	n := len(r.b.snapshot())
	out := make([]Ref, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, r.b.ref(childPath(i)))
	}
	return out, nil
}

func (r *accRoot) GetIndexInParent() (int32, *dbus.Error) { return 0, nil }
func (r *accRoot) GetState() ([]uint32, *dbus.Error)      { return stateSet(), nil }

func (r *accRoot) GetApplicationBusAddress() (string, *dbus.Error) { return "", nil }

// Get answers org.freedesktop.DBus.Properties for the root. A client reads the
// name, role and child count this way rather than by calling the methods.
func (r *accRoot) Get(iface, prop string) (dbus.Variant, *dbus.Error) {
	r.b.mu.Lock()
	title, n, parent := r.b.title, len(r.b.nodes), r.b.parent
	r.b.mu.Unlock()
	switch prop {
	case "Name", "Description":
		if prop == "Description" {
			return dbus.MakeVariant(""), nil
		}
		return dbus.MakeVariant(title), nil
	case "ChildCount":
		return dbus.MakeVariant(int32(n)), nil
	case "Parent":
		return dbus.MakeVariant(parent), nil
	case "ToolkitName":
		return dbus.MakeVariant("go-widgets"), nil
	case "Version", "AtspiVersion":
		return dbus.MakeVariant("2.1"), nil
	}
	return dbus.MakeVariant(""), nil
}

// accChild is one element of the published tree.
type accChild struct {
	b   *Bridge
	idx int
}

func (c *accChild) node() (nodeInfo, bool) {
	n, ok := c.b.at(c.idx)
	if !ok {
		return nodeInfo{}, false
	}
	c.b.mu.Lock()
	ox, oy := c.b.originX, c.b.originY
	c.b.mu.Unlock()
	return nodeInfo{name: n.Name, role: Role(n.Role), node: n, ox: ox, oy: oy}, true
}

type nodeInfo struct {
	name   string
	role   uint32
	node   toolkit.A11yNode
	ox, oy int
}

func (c *accChild) GetRole() (uint32, *dbus.Error) {
	n, ok := c.node()
	if !ok {
		// A path beyond the current tree answers "invalid" rather than
		// pretending to be a panel: a client holding a stale reference should
		// learn it is stale.
		return RoleInvalid, nil
	}
	return n.role, nil
}

func (c *accChild) GetRoleName() (string, *dbus.Error) {
	n, ok := c.node()
	if !ok {
		return "invalid", nil
	}
	return RoleName(n.role), nil
}

func (c *accChild) GetLocalizedRoleName() (string, *dbus.Error) { return c.GetRoleName() }

func (c *accChild) GetChildAtIndex(int32) (Ref, *dbus.Error) { return nullRef, nil }
func (c *accChild) GetChildren() ([]Ref, *dbus.Error)        { return nil, nil }
func (c *accChild) GetIndexInParent() (int32, *dbus.Error)   { return int32(c.idx), nil }
func (c *accChild) GetState() ([]uint32, *dbus.Error)        { return stateSet(), nil }

// Get answers the Properties interface for one element.
func (c *accChild) Get(iface, prop string) (dbus.Variant, *dbus.Error) {
	n, ok := c.node()
	if !ok {
		return dbus.MakeVariant(""), nil
	}
	switch prop {
	case "Name":
		return dbus.MakeVariant(n.name), nil
	case "Description":
		return dbus.MakeVariant(""), nil
	case "ChildCount":
		return dbus.MakeVariant(int32(0)), nil
	case "Parent":
		return dbus.MakeVariant(c.b.ref(rootPath)), nil
	}
	return dbus.MakeVariant(""), nil
}

// GetExtents reports where the element is. coordType 0 is screen coordinates
// and 1 is window-relative; AT-SPI asks for both on the same method, and a
// bridge that answers one for the other sends a screen reader to the wrong
// place without any error appearing.
func (c *accChild) GetExtents(coordType uint32) (struct{ X, Y, W, H int32 }, *dbus.Error) {
	var out struct{ X, Y, W, H int32 }
	n, ok := c.node()
	if !ok {
		return out, nil
	}
	ox, oy := n.ox, n.oy
	if coordType != 0 {
		ox, oy = 0, 0
	}
	out.X, out.Y, out.W, out.H = ScreenRect(n.node, ox, oy)
	return out, nil
}

func (c *accChild) GetPosition(coordType uint32) (int32, int32, *dbus.Error) {
	e, _ := c.GetExtents(coordType)
	return e.X, e.Y, nil
}

func (c *accChild) GetSize() (int32, int32, *dbus.Error) {
	e, _ := c.GetExtents(1)
	return e.W, e.H, nil
}

// Contains and GetAccessibleAtPoint let a client hit-test the tree.
func (c *accChild) Contains(x, y int32, coordType uint32) (bool, *dbus.Error) {
	e, _ := c.GetExtents(coordType)
	return x >= e.X && x < e.X+e.W && y >= e.Y && y < e.Y+e.H, nil
}

// --- org.a11y.atspi.Action ------------------------------------------------

func (c *accChild) GetNActions() (int32, *dbus.Error) {
	if n, ok := c.node(); ok && n.role == RolePushButton {
		return 1, nil
	}
	return 0, nil
}

func (c *accChild) GetName(int32) (string, *dbus.Error)        { return "click", nil }
func (c *accChild) GetDescription(int32) (string, *dbus.Error) { return "", nil }
func (c *accChild) GetKeyBinding(int32) (string, *dbus.Error)  { return "", nil }

// DoAction activates the element by replaying an ordinary click at its centre,
// through the SAME path a real click takes — so every behaviour a click has is
// had by an AT-SPI action, with no second implementation to drift.
func (c *accChild) DoAction(int32) (bool, *dbus.Error) {
	n, ok := c.node()
	if !ok {
		return false, nil
	}
	c.b.mu.Lock()
	activate := c.b.activate
	c.b.mu.Unlock()
	if activate == nil {
		return false, nil
	}
	x, y, ok := ParsePressPoint(PressPoint(n.node))
	if !ok {
		return false, nil
	}
	activate(x, y)
	return true, nil
}

// --- org.a11y.atspi.Cache -------------------------------------------------

// accCache is how a client actually reads the tree: it fetches every element in
// one call rather than walking. Without this interface the application is
// discovered and then appears empty.
type accCache struct{ b *Bridge }

// cacheItem is POSITIONAL — the field order IS the wire format
// a((so)(so)(so)iiassusau), read off a live GTK application.
type cacheItem struct {
	Ref         Ref
	App         Ref
	Parent      Ref
	Index       int32
	ChildCount  int32
	Interfaces  []string
	Name        string
	Role        uint32
	Description string
	State       []uint32
}

func (c *accCache) item(i int) (cacheItem, bool) {
	n, ok := c.b.at(i)
	if !ok {
		return cacheItem{}, false
	}
	return cacheItem{
		Ref:        c.b.ref(childPath(i)),
		App:        c.b.ref(rootPath),
		Parent:     c.b.ref(rootPath),
		Index:      int32(i),
		ChildCount: 0,
		Interfaces: []string{ifaceAccessible, ifaceComponent, ifaceAction},
		Name:       n.Name,
		Role:       Role(n.Role),
		State:      stateSet(),
	}, true
}

func (c *accCache) GetItems() ([]cacheItem, *dbus.Error) {
	nodes := c.b.snapshot()
	out := make([]cacheItem, 0, len(nodes)+1)
	c.b.mu.Lock()
	title := c.b.title
	c.b.mu.Unlock()
	// The root's parent in the cache is the NULL reference, not what Embed
	// returned: reporting the registry there makes a client look for a parent
	// it cannot reach and drop the whole application.
	out = append(out, cacheItem{
		Ref:        c.b.ref(rootPath),
		App:        c.b.ref(rootPath),
		Parent:     nullRef,
		Index:      -1,
		ChildCount: int32(len(nodes)),
		Interfaces: []string{ifaceAccessible, ifaceApplication},
		Name:       title,
		Role:       RoleApplication,
		State:      stateSet(),
	})
	for i := range nodes {
		if it, ok := c.item(i); ok {
			out = append(out, it)
		}
	}
	return out, nil
}

// stateSet is the state bitset every element reports: enabled, sensitive,
// showing and visible. An element that omits showing/visible is announced as
// hidden and skipped by the reader.
func stateSet() []uint32 {
	var lo uint32
	for _, s := range []uint32{StateEnabled, StateSensitive, StateShowing, StateVisible} {
		lo |= 1 << s
	}
	return []uint32{lo, 0}
}
