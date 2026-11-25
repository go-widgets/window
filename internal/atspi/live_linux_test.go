// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package atspi

import (
	"os"
	"testing"

	"github.com/go-widgets/toolkit"
	"github.com/godbus/dbus/v5"
)

// TestLiveATSPI is the proof the unit tests cannot give: a REAL accessibility
// bus, reached the way a screen reader reaches it (session bus -> org.a11y.Bus
// -> GetAddress, no AT_SPI_BUS_ADDRESS override), carrying a tree published by
// Publish. A second, independent client connection then does what Orca does:
// reads the whole tree through the Cache, finds a push button by its label,
// reads its state words, and calls Action "click".
//
// It needs dbus, at-spi2-core and a running at-spi-bus-launcher, so it is gated
// on WINDOW_ATSPI_LIVE=1 and run by the live-atspi CI lane, which fails when
// the test did not reach PASS.
func TestLiveATSPI(t *testing.T) {
	if os.Getenv("WINDOW_ATSPI_LIVE") != "1" {
		t.Skip("set WINDOW_ATSPI_LIVE=1 under dbus-run-session with at-spi-bus-launcher")
	}
	if os.Getenv("AT_SPI_BUS_ADDRESS") != "" {
		t.Fatal("AT_SPI_BUS_ADDRESS must be unset: the proof is the real discovery path")
	}

	save := toolkit.NewButton("Save", func() {})
	save.SetBounds(toolkit.Rect{X: 10, Y: 10, W: 100, H: 30})
	del := toolkit.NewButton("Delete", func() {})
	del.SetBounds(toolkit.Rect{X: 10, Y: 50, W: 100, H: 30})
	del.Disabled().Set(true)
	box := toolkit.NewContainer(nil)
	box.AddWidget(save)
	box.AddWidget(del)

	Publish(box, "live app", 0, 0)
	startMu.Lock()
	b := bridge
	startMu.Unlock()
	if b == nil {
		t.Fatal("the bridge did not come up: no accessibility bus reachable")
	}

	// The reader: a separate connection with its own unique name.
	sess, err := dbus.ConnectSessionBus()
	if err != nil {
		t.Fatal(err)
	}
	defer sess.Close()
	var addr string
	if err := sess.Object("org.a11y.Bus", "/org/a11y/bus").
		Call("org.a11y.Bus.GetAddress", 0).Store(&addr); err != nil {
		t.Fatal(err)
	}
	cl, err := dbus.Dial(addr)
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	if err := cl.Auth(nil); err != nil {
		t.Fatal(err)
	}
	if err := cl.Hello(); err != nil {
		t.Fatal(err)
	}
	if cl.Names()[0] == b.name {
		t.Fatal("reader and application share a connection: not an independent client")
	}

	items := func() []cacheItem {
		var out []cacheItem
		if err := cl.Object(b.name, cachePath).
			Call(ifaceCache+".GetItems", 0).Store(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}
	find := func(its []cacheItem, name string) cacheItem {
		for _, it := range its {
			if it.Name == name {
				return it
			}
		}
		t.Fatalf("no element named %q in the tree: %+v", name, its)
		return cacheItem{}
	}

	its := items()
	for _, it := range its {
		t.Logf("tree: role=%d (%s) name=%q state=%#x path=%s",
			it.Role, RoleName(it.Role), it.Name, it.State, it.Ref.Path)
	}

	const operable = 1<<StateEnabled | 1<<StateSensitive
	sv := find(its, "Save")
	if sv.Role != RolePushButton {
		t.Fatalf("Save has role %d (%s), want push button %d", sv.Role, RoleName(sv.Role), RolePushButton)
	}
	if sv.State[0]&operable != operable {
		t.Fatalf("Save state %#x lacks enabled+sensitive", sv.State[0])
	}
	dl := find(its, "Delete")
	if dl.Role != RolePushButton {
		t.Fatalf("Delete has role %d, want push button", dl.Role)
	}
	if dl.State[0]&(1<<StateEnabled) != 0 || dl.State[0]&(1<<StateSensitive) != 0 {
		t.Fatalf("disabled Delete state %#x still reports enabled/sensitive", dl.State[0])
	}
	if dl.State[0]&(1<<StateShowing) == 0 {
		t.Fatalf("disabled Delete state %#x must stay showing", dl.State[0])
	}

	// Action "click" fires: DoAction must queue exactly one activation, at the
	// button's centre, for the back-end to replay as a real click.
	TakePending()
	var ok bool
	if err := cl.Object(b.name, sv.Ref.Path).
		Call(ifaceAction+".DoAction", 0, int32(0)).Store(&ok); err != nil || !ok {
		t.Fatalf("DoAction ok=%v err=%v", ok, err)
	}
	pend := TakePending()
	if len(pend) != 1 || pend[0].X != 60 || pend[0].Y != 25 {
		t.Fatalf("pending after DoAction = %+v, want one click at (60,25)", pend)
	}
	var name string
	if err := cl.Object(b.name, sv.Ref.Path).
		Call(ifaceAction+".GetName", 0, int32(0)).Store(&name); err != nil || name != "click" {
		t.Fatalf("action 0 name = %q err=%v, want click", name, err)
	}

	// Re-enabling is seen through GetState on the object itself, not only the
	// cache snapshot the reader already holds.
	del.Disabled().Set(false)
	Publish(box, "live app", 0, 0)
	var st []uint32
	if err := cl.Object(b.name, dl.Ref.Path).
		Call(ifaceAccessible+".GetState", 0).Store(&st); err != nil {
		t.Fatal(err)
	}
	if st[0]&operable != operable {
		t.Fatalf("after re-enabling, Delete state %#x lacks enabled+sensitive", st[0])
	}
}
