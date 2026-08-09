// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// This deterministic in-process test locks in the dynamic input hot-plug
// path: a seat that advertises NO input devices at bring-up, then gains a
// keyboard and pointer capability later (as a device-less headless seat does
// the moment a virtual input device is attached to it). The window must pick
// the devices up on the later wl_seat.capabilities event and dispatch the
// subsequently injected key and click — the same sequence the live sway lane
// exercises with the sovereign virtual-input injector, proven here with no
// display server.
package window

import (
	"syscall"
	"testing"
	"time"

	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/window/internal/wayland"
)

// hotplugCompositor is a scripted xdg-shell compositor whose seat starts with
// no capabilities and hot-plugs keyboard+pointer only after the first present.
func hotplugCompositor(t *testing.T, sc *srvConn, kmFD int) {
	var registryID, compID, shmID, wmID, seatID, ptrID, kbID uint32
	var surfID, xdgSurfID, tlID uint32
	serial := uint32(1)
	sawAttach := false
	configured := false
	hotplugged := false

	for {
		obj, op, body, err := sc.read()
		if err != nil {
			return
		}
		switch {
		case obj == 1 && op == 1: // get_registry
			registryID = no.Uint32(body[0:4])
			_ = sc.send(registryID, 0, cat(eU32(1), eStr("wl_compositor"), eU32(4)))
			_ = sc.send(registryID, 0, cat(eU32(2), eStr("wl_shm"), eU32(1)))
			_ = sc.send(registryID, 0, cat(eU32(3), eStr("xdg_wm_base"), eU32(4)))
			_ = sc.send(registryID, 0, cat(eU32(4), eStr("wl_seat"), eU32(5)))
		case obj == 1 && op == 0: // sync
			_ = sc.send(no.Uint32(body[0:4]), 0, eU32(0))
		case obj == registryID && op == 0: // bind
			iface, rest := decStr(body[4:])
			newid := no.Uint32(rest[4:8])
			switch iface {
			case "wl_compositor":
				compID = newid
			case "wl_shm":
				shmID = newid
				_ = sc.send(shmID, 0, eU32(wayland.ShmFormatARGB8888))
			case "xdg_wm_base":
				wmID = newid
			case "wl_seat":
				seatID = newid
				// No devices yet.
				_ = sc.send(seatID, 0, eU32(0))
				_ = sc.send(seatID, 1, eStr("seat0"))
			}
		case obj == seatID && op == 0: // get_pointer
			ptrID = no.Uint32(body[0:4])
		case obj == seatID && op == 1: // get_keyboard (last device the client binds)
			kbID = no.Uint32(body[0:4])
			_ = sc.send(kbID, 0, cat(eU32(wayland.KeymapFormatXkbV1), eU32(uint32(len(testKeymap)+1))), kmFD)
			// Both devices are now bound; inject a click and a key, then close.
			_ = sc.send(ptrID, 0, cat(eU32(serial), eU32(surfID), eFixed(30), eFixed(40)))
			serial++
			_ = sc.send(ptrID, 3, cat(eU32(serial), eU32(0), eU32(wayland.BtnLeft), eU32(wayland.StatePressed)))
			serial++
			_ = sc.send(kbID, 3, cat(eU32(serial), eU32(0), eU32(evA), eU32(wayland.StatePressed)))
			serial++
			_ = sc.send(tlID, 1, nil) // xdg_toplevel.close
		case obj == compID && op == 0:
			surfID = no.Uint32(body[0:4])
		case obj == wmID && op == 2:
			xdgSurfID = no.Uint32(body[0:4])
		case obj == xdgSurfID && op == 1:
			tlID = no.Uint32(body[0:4])
		case obj == shmID && op == 0:
			if fd := sc.popFD(); fd >= 0 {
				_ = syscall.Close(fd)
			}
		case obj == surfID && op == 1: // attach
			sawAttach = true
		case obj == surfID && op == 6: // commit
			switch {
			case !configured:
				_ = sc.send(tlID, 0, cat(eU32(200), eU32(160), eArr(nil)))
				_ = sc.send(xdgSurfID, 0, eU32(serial))
				serial++
				configured = true
			case sawAttach && !hotplugged:
				// Hot-plug the devices now; the client binds them on the
				// capabilities event and injection follows on get_keyboard.
				hotplugged = true
				_ = sc.send(seatID, 0, eU32(wayland.SeatCapabilityPointer|wayland.SeatCapabilityKeyboard))
			}
			sawAttach = false
		}
	}
}

func TestWaylandDynamicInputHotplug(t *testing.T) {
	cli, srv := socketPairWin(t)
	defer srv.Close()

	km := keymapFile(t)
	defer km.Close()

	sc := &srvConn{c: srv}
	go hotplugCompositor(t, sc, int(km.Fd()))

	conn := wayland.New(cli)
	w, err := newWaylandWindow(conn, Config{Title: "wl-hotplug", Width: 100, Height: 80})
	if err != nil {
		t.Fatalf("newWaylandWindow: %v", err)
	}
	// No devices at bring-up.
	if w.pointer != nil || w.keyboard != nil {
		t.Fatal("devices should be absent before hot-plug")
	}

	root := &recWidget{}
	done := make(chan error, 1)
	go func() { done <- w.Run(root) }()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run did not exit after compositor close")
	}

	// The devices were bound dynamically on the later capabilities event.
	if w.pointer == nil || w.keyboard == nil {
		t.Fatalf("devices not hot-plugged: pointer=%v keyboard=%v", w.pointer != nil, w.keyboard != nil)
	}

	var gotClick, gotChar bool
	var cx, cy int
	for _, ev := range root.events {
		switch {
		case ev.Kind == toolkit.EventClick:
			gotClick, cx, cy = true, ev.X, ev.Y
		case ev.Kind == toolkit.EventChar && ev.Code == "a":
			gotChar = true
		}
	}
	if !gotClick || cx != 30 || cy != 40 {
		t.Errorf("hot-plugged click = (%d,%d) present=%v, want (30,40)", cx, cy, gotClick)
	}
	if !gotChar {
		t.Errorf("no EventChar 'a' after hot-plug; events=%+v", root.events)
	}
	_ = w.Close()
}
