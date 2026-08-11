// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// Bringing the AT-SPI bridge up and keeping the published tree current.
//
// # The two buses
//
// AT-SPI does not live on the session bus. The session bus only carries
// org.a11y.Bus, whose GetAddress hands back the address of a SECOND bus that
// the accessibility traffic actually flows over. Every toolkit follows that
// indirection and so does this: exporting on the session bus would look
// perfectly correct and be invisible to every screen reader.
//
// # Registration
//
// Objects are exported under /org/a11y/atspi/accessible/, then
// org.a11y.atspi.Socket.Embed hands the registry our (bus name, root path) and
// returns the parent to report. An application that skips Embed is exported,
// reachable, and never discovered.
//
// Every step is allowed to fail quietly. A machine with no accessibility stack
// running — which is most of them — has no org.a11y.Bus to answer, and a
// windowing library must not refuse to open a window over that.
//
//go:build linux

package atspi

import (
	"os"
	"strconv"
	"sync"

	"github.com/go-widgets/toolkit"
	"github.com/godbus/dbus/v5"
	"github.com/godbus/dbus/v5/prop"
)

// AT-SPI object paths. Children live under pathPrefix + their index, so a path
// is enough to find the element it describes and no lookup table has to be kept
// in step with the tree.
const (
	rootPath   = "/org/a11y/atspi/accessible/root"
	pathPrefix = "/org/a11y/atspi/accessible/"
	cachePath  = "/org/a11y/atspi/cache"

	ifaceAccessible  = "org.a11y.atspi.Accessible"
	ifaceApplication = "org.a11y.atspi.Application"
	ifaceComponent   = "org.a11y.atspi.Component"
	ifaceAction      = "org.a11y.atspi.Action"
	ifaceCache       = "org.a11y.atspi.Cache"
	ifaceEventObject = "org.a11y.atspi.Event.Object"
)

// Ref is AT-SPI's object reference: the bus name that owns it and its path —
// the "(so)" every AT-SPI method passes around.
//
// Name is a plain string, and MUST stay one. godbus's dbus.Sender is an
// ANNOTATION type meaning "inject the caller's name" rather than "a string on
// the wire": using it here silently changes this struct's marshalled signature
// so the registry never lists the application. The signature is checked rather
// than assumed -- dbus.SignatureOf reports (so) for this and
// a((so)(so)(so)iiassusau) for the cache item.
type Ref struct {
	Name string
	Path dbus.ObjectPath
}

// nullRef is the reference AT-SPI uses for "nothing".
var nullRef = Ref{Name: "org.a11y.atspi.Registry", Path: "/org/a11y/atspi/null"}

// Bridge is the live connection and the snapshot being described.
type Bridge struct {
	conn *dbus.Conn
	name string // our unique bus name on the accessibility bus

	mu       sync.Mutex
	nodes    []toolkit.A11yNode
	originX  int
	originY  int
	title    string
	exported int // how many child paths have been exported so far
	parent   Ref

	// pending holds activations requested by a client but not yet applied.
	//
	// An AT-SPI action arrives on a D-Bus goroutine, and the widget tree
	// belongs to the thread that draws it: touching it from here would race
	// with rendering. So the point is only RECORDED, and the back-end applies
	// it on its own thread — which is also what makes the click take the exact
	// same path as a real one.
	pending []struct{ X, Y int }
}

var (
	bridge  *Bridge
	startMu sync.Mutex
	started bool
)

// childPath is the object path for the nth element.
func childPath(n int) dbus.ObjectPath {
	return dbus.ObjectPath(pathPrefix + strconv.Itoa(n))
}

func (b *Bridge) snapshot() []toolkit.A11yNode {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.nodes
}

func (b *Bridge) at(n int) (toolkit.A11yNode, bool) {
	s := b.snapshot()
	if n < 0 || n >= len(s) {
		return toolkit.A11yNode{}, false
	}
	return s[n], true
}

func (b *Bridge) ref(p dbus.ObjectPath) Ref { return Ref{Name: b.name, Path: p} }

// Publish republishes the tree for the frame just drawn. The back-end calls it
// from the same place that presents pixels, so the description never lags them.
//
// activate is how an AT-SPI action reaches the widget tree; it is stored on
// every call because the back-end owns it and this package must not import a
// back-end.
func Publish(root toolkit.Widget, title string, originX, originY int) {
	nodes := Nodes(root)

	startMu.Lock()
	if !started {
		// Register only once the tree is POPULATED. A client reads the whole
		// cache the moment an application appears and does not read it again
		// unaided, so registering on the first empty frame hands it a permanent
		// snapshot of an empty application — which is exactly what it showed.
		if len(nodes) == 0 {
			startMu.Unlock()
			return
		}
		bridge = start(nodes, title, originX, originY)
		started = true
		startMu.Unlock()
		return
	}
	b := bridge
	startMu.Unlock()
	if b == nil {
		return
	}

	b.mu.Lock()
	b.nodes, b.title = nodes, title
	b.originX, b.originY = originX, originY
	need, have := len(nodes), b.exported
	b.mu.Unlock()

	for i := have; i < need; i++ {
		if !b.exportChild(i) {
			return
		}
	}
	if need > have {
		b.mu.Lock()
		b.exported = need
		b.mu.Unlock()
		b.announceNew(have, need)
	}
	b.publishProps()
}

// publishProps registers the properties a client reads an element through.
//
// The D-Bus library serves org.freedesktop.DBus.Properties ITSELF (godbus via
// its prop subpackage) and never dispatches to an exported Get method, so
// answering there is not enough: the values have to be registered. Measured,
// before this: the cache returned the whole correct tree while every Name
// lookup failed with "no property Name on interface org.a11y.atspi.Accessible",
// and a client showed the application unnamed.
//
// A fresh property set is published on every change rather than mutated in
// place. prop.Export builds a brand-new *prop.Properties (with its own copies
// of every value) and re-registers the org.freedesktop.DBus.Properties handler
// for the path atomically through conn.Export, so a value already handed to a
// client is never mutated underneath it -- no data race. Because prop.Export
// registers ONE handler covering every interface at a path, all of a path's
// interfaces are published in a single call (godbus would otherwise have the
// second call replace the first).
func (b *Bridge) publishProps() {
	b.mu.Lock()
	title, nodes, parent := b.title, b.nodes, b.parent
	b.mu.Unlock()

	_, _ = prop.Export(b.conn, rootPath, prop.Map{
		ifaceAccessible: {
			"Name":        {Value: title},
			"Description": {Value: ""},
			"ChildCount":  {Value: int32(len(nodes))},
			"Parent":      {Value: parent},
		},
		ifaceApplication: {
			"ToolkitName":  {Value: "go-widgets"},
			"Version":      {Value: "2.1"},
			"AtspiVersion": {Value: "2.1"},
			"Id":           {Value: int32(0)},
		},
	})
	root := b.ref(rootPath)
	for i, n := range nodes {
		// NActions is a PROPERTY of the Action interface, not only a method.
		// Answering the method alone leaves a client showing the element with
		// an empty action list: readable, and impossible to operate. Measured
		// exactly that way before this line existed.
		actions := int32(0)
		if Role(n.Role) == RolePushButton {
			actions = 1
		}
		_, _ = prop.Export(b.conn, childPath(i), prop.Map{
			ifaceAccessible: {
				"Name":        {Value: n.Name},
				"Description": {Value: ""},
				"ChildCount":  {Value: int32(0)},
				"Parent":      {Value: root},
			},
			ifaceAction: {
				"NActions": {Value: actions},
			},
		})
	}
}

// exportChild publishes one element's object under its own path. Paths are
// exported as the tree grows and then KEPT: a client may hold a reference
// across frames, and unexporting underneath it would turn a live reference into
// an error. Paths beyond the current tree report an invalid role and are not
// listed as children, so they are unreachable rather than wrong.
func (b *Bridge) exportChild(i int) bool {
	child := &accChild{b: b, idx: i}
	p := childPath(i)
	// org.freedesktop.DBus.Properties is served by prop.Export (see
	// publishProps), not by this object, so it is not in the export list.
	for _, iface := range []string{ifaceAccessible, ifaceComponent, ifaceAction} {
		if err := b.conn.Export(child, p, iface); err != nil {
			return false
		}
	}
	return true
}

// announceNew tells a client that already snapshotted the cache about elements
// added since. Without this a tree that grows after registration is invisible
// to everything that read it once.
func (b *Bridge) announceNew(from, to int) {
	cache := &accCache{b: b}
	root := b.ref(rootPath)
	for i := from; i < to; i++ {
		item, ok := cache.item(i)
		if !ok {
			continue
		}
		_ = b.conn.Emit(cachePath, ifaceCache+".AddAccessible", item)
		// A client builds its model from the event stream as much as from the
		// cache, so a tree that only appears in the cache has nothing to attach
		// it to. The signature is "siiv(so)": what changed, two indices, the
		// payload, and the source's application.
		_ = b.conn.Emit(rootPath, ifaceEventObject+".ChildrenChanged",
			"add", int32(i), int32(0),
			dbus.MakeVariant(b.ref(childPath(i))), root)
	}
}

// start brings the bridge up: find the accessibility bus, export the
// application root, and register with the registry.
func start(nodes []toolkit.A11yNode, title string, originX, originY int) *Bridge {
	addr, err := busAddress()
	if err != nil || addr == "" {
		return nil
	}
	conn, err := dbus.Dial(addr)
	if err != nil {
		return nil
	}
	// godbus's Dial returns an unauthenticated connection: the SASL handshake
	// and the org.freedesktop.DBus.Hello that assigns our unique name are
	// explicit steps (ConnectSessionBus does them for the session bus, but the
	// a11y bus is dialled by address).
	if err := conn.Auth(nil); err != nil {
		conn.Close()
		return nil
	}
	if err := conn.Hello(); err != nil {
		conn.Close()
		return nil
	}
	names := conn.Names()
	if len(names) == 0 {
		conn.Close()
		return nil
	}

	b := &Bridge{
		conn: conn, name: names[0], parent: nullRef,
		nodes: nodes, title: title, originX: originX, originY: originY,
	}
	root := &accRoot{b: b}
	// Properties is served by prop.Export in publishProps (called below), not
	// by accRoot, so it is not exported here.
	for _, iface := range []string{ifaceAccessible, ifaceApplication} {
		if err := conn.Export(root, rootPath, iface); err != nil {
			conn.Close()
			return nil
		}
	}
	// The cache is how a client actually reads the tree; without it the
	// application is discovered and then appears empty, with at-spi logging
	// exactly that.
	if err := conn.Export(&accCache{b: b}, cachePath, ifaceCache); err != nil {
		conn.Close()
		return nil
	}
	for i := range nodes {
		if !b.exportChild(i) {
			conn.Close()
			return nil
		}
	}
	b.exported = len(nodes)
	b.publishProps()

	// Embed hands the registry our root and returns the parent to report. An
	// application that exports its objects but skips this is reachable on the
	// bus and never discovered by anything.
	var parent Ref
	call := conn.Object("org.a11y.atspi.Registry", rootPath).
		Call("org.a11y.atspi.Socket.Embed", 0, b.ref(rootPath))
	if call.Err == nil {
		_ = call.Store(&parent)
		b.mu.Lock()
		b.parent = parent
		b.mu.Unlock()
	}
	return b
}

// busAddress asks the SESSION bus where the accessibility bus is.
//
// AT_SPI_BUS_ADDRESS overrides the lookup, which is how a test harness points
// an application at a bus it controls.
func busAddress() (string, error) {
	if a := os.Getenv("AT_SPI_BUS_ADDRESS"); a != "" {
		return a, nil
	}
	sess, err := dbus.ConnectSessionBus()
	if err != nil {
		return "", err
	}
	var addr string
	err = sess.Object("org.a11y.Bus", "/org/a11y/bus").
		Call("org.a11y.Bus.GetAddress", 0).Store(&addr)
	return addr, err
}

// TakePending hands the back-end the activations a client asked for since the
// last call, to be applied on the thread that owns the widget tree.
//
// KNOWN LIMITATION: the back-end drains this while painting, and the X11 loop
// blocks on the display socket, so an action taken while the application is
// otherwise idle is applied on the NEXT event rather than immediately. The
// click itself lands correctly — measured, clicks: 0 to clicks: 1 — the tree
// simply republishes a beat later. Closing that needs the loop woken from
// outside, e.g. a self-addressed X ClientMessage.
//
// It returns nothing when the bridge never came up, which is the normal case on
// a machine with no accessibility stack running.
func TakePending() []struct{ X, Y int } {
	startMu.Lock()
	b := bridge
	startMu.Unlock()
	if b == nil {
		return nil
	}
	b.mu.Lock()
	out := b.pending
	b.pending = nil
	b.mu.Unlock()
	return out
}
