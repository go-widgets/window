// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package wayland

import "fmt"

// This file implements the client side of two wlroots/unstable input
// protocols used to inject real input into a compositor seat:
//
//   - zwp_virtual_keyboard_manager_v1 / zwp_virtual_keyboard_v1
//   - zwlr_virtual_pointer_manager_v1 / zwlr_virtual_pointer_v1
//
// Creating a virtual device attaches it to the seat, which makes the seat
// advertise the matching capability (keyboard / pointer) to every client on
// it — so a plain client (our own window) then receives the injected key and
// pointer events through the ordinary wl_keyboard / wl_pointer path. This is
// what lets the live integration test drive real input end to end with no
// external injector command: the test is both the application and, over a
// second connection, its own sovereign input source.
//
// Both protocols are request-only from the client's perspective (the devices
// emit no events), so the objects register no event handler; an unexpected
// event for one of them is discarded by Conn.Dispatch like any event for an
// object with no handler.

// Interface names of the two manager globals.
const (
	ifaceVirtualKeyboardManager = "zwp_virtual_keyboard_manager_v1"
	ifaceVirtualPointerManager  = "zwlr_virtual_pointer_manager_v1"
)

const (
	virtualKeyboardManagerVersion = 1
	virtualPointerManagerVersion  = 2
)

// --- zwp_virtual_keyboard_manager_v1 --------------------------------------

// VirtualKeyboardManager is the zwp_virtual_keyboard_manager_v1 global: it
// manufactures virtual keyboards bound to a seat.
type VirtualKeyboardManager struct {
	conn *Conn
	id   uint32
}

// virtual_keyboard_manager request opcode.
const vkbdMgrReqCreate = 0

// VirtualKeyboardManager binds the zwp_virtual_keyboard_manager_v1 global.
func (r *Registry) VirtualKeyboardManager() (*VirtualKeyboardManager, error) {
	g, ok := r.Find(ifaceVirtualKeyboardManager)
	if !ok {
		return nil, fmt.Errorf("wayland: compositor advertises no %s", ifaceVirtualKeyboardManager)
	}
	ver := min32(g.Version, virtualKeyboardManagerVersion)
	id, err := r.bind(g.Name, ifaceVirtualKeyboardManager, ver)
	if err != nil {
		return nil, err
	}
	return &VirtualKeyboardManager{conn: r.conn, id: id}, nil
}

// CreateKeyboard creates a virtual keyboard attached to seat.
func (m *VirtualKeyboardManager) CreateKeyboard(seat *Seat) (*VirtualKeyboard, error) {
	id := m.conn.allocID()
	e := newEncoder(m.conn.order)
	e.putU32(seat.id)
	e.putU32(id)
	if err := m.conn.send(m.id, vkbdMgrReqCreate, e.buf, nil); err != nil {
		return nil, err
	}
	return &VirtualKeyboard{conn: m.conn, id: id}, nil
}

// VirtualKeyboard is a zwp_virtual_keyboard_v1: a client-driven keyboard on
// the seat. A keymap must be uploaded before any key event.
type VirtualKeyboard struct {
	conn *Conn
	id   uint32
}

// virtual_keyboard request opcodes.
const (
	vkbdReqKeymap    = 0
	vkbdReqKey       = 1
	vkbdReqModifiers = 2
	vkbdReqDestroy   = 3
)

// Keymap uploads the xkb keymap the virtual keyboard's key codes are
// interpreted against, passing the (read-only) descriptor over SCM_RIGHTS.
// The compositor forwards this same keymap to focused clients.
func (k *VirtualKeyboard) Keymap(format uint32, fd int, size uint32) error {
	e := newEncoder(k.conn.order)
	e.putU32(format)
	e.putU32(size)
	return k.conn.send(k.id, vkbdReqKeymap, e.buf, []int{fd})
}

// Key injects a key press or release. key is the Linux evdev keycode (e.g.
// 30 for KEY_A); state is StatePressed or StateReleased.
func (k *VirtualKeyboard) Key(time, key, state uint32) error {
	e := newEncoder(k.conn.order)
	e.putU32(time)
	e.putU32(key)
	e.putU32(state)
	return k.conn.send(k.id, vkbdReqKey, e.buf, nil)
}

// Modifiers sets the active modifier masks (depressed/latched/locked/group).
func (k *VirtualKeyboard) Modifiers(depressed, latched, locked, group uint32) error {
	e := newEncoder(k.conn.order)
	e.putU32(depressed)
	e.putU32(latched)
	e.putU32(locked)
	e.putU32(group)
	return k.conn.send(k.id, vkbdReqModifiers, e.buf, nil)
}

// Destroy releases the virtual keyboard (removing the seat's keyboard
// capability if it was the only one).
func (k *VirtualKeyboard) Destroy() error {
	err := k.conn.send(k.id, vkbdReqDestroy, nil, nil)
	k.conn.unregister(k.id)
	return err
}

// --- zwlr_virtual_pointer_manager_v1 --------------------------------------

// VirtualPointerManager is the zwlr_virtual_pointer_manager_v1 global: it
// manufactures virtual pointers bound to a seat.
type VirtualPointerManager struct {
	conn *Conn
	id   uint32
}

// virtual_pointer_manager request opcode.
const vptrMgrReqCreate = 0

// VirtualPointerManager binds the zwlr_virtual_pointer_manager_v1 global.
func (r *Registry) VirtualPointerManager() (*VirtualPointerManager, error) {
	g, ok := r.Find(ifaceVirtualPointerManager)
	if !ok {
		return nil, fmt.Errorf("wayland: compositor advertises no %s", ifaceVirtualPointerManager)
	}
	ver := min32(g.Version, virtualPointerManagerVersion)
	id, err := r.bind(g.Name, ifaceVirtualPointerManager, ver)
	if err != nil {
		return nil, err
	}
	return &VirtualPointerManager{conn: r.conn, id: id}, nil
}

// CreatePointer creates a virtual pointer attached to seat.
func (m *VirtualPointerManager) CreatePointer(seat *Seat) (*VirtualPointer, error) {
	id := m.conn.allocID()
	e := newEncoder(m.conn.order)
	e.putU32(seat.id)
	e.putU32(id)
	if err := m.conn.send(m.id, vptrMgrReqCreate, e.buf, nil); err != nil {
		return nil, err
	}
	return &VirtualPointer{conn: m.conn, id: id}, nil
}

// VirtualPointer is a zwlr_virtual_pointer_v1: a client-driven pointer on the
// seat. Absolute motion maps a coordinate within an extent onto the output.
type VirtualPointer struct {
	conn *Conn
	id   uint32
}

// virtual_pointer request opcodes.
const (
	vptrReqMotion         = 0
	vptrReqMotionAbsolute = 1
	vptrReqButton         = 2
	vptrReqAxis           = 3
	vptrReqFrame          = 4
	vptrReqDestroy        = 8
)

// MotionAbsolute moves the pointer to (x, y) interpreted within the extent
// (xExtent, yExtent); the compositor scales it onto the output geometry.
func (p *VirtualPointer) MotionAbsolute(time, x, y, xExtent, yExtent uint32) error {
	e := newEncoder(p.conn.order)
	e.putU32(time)
	e.putU32(x)
	e.putU32(y)
	e.putU32(xExtent)
	e.putU32(yExtent)
	return p.conn.send(p.id, vptrReqMotionAbsolute, e.buf, nil)
}

// Button injects a pointer button press or release. button is a Linux evdev
// button code (e.g. BtnLeft); state is StatePressed or StateReleased.
func (p *VirtualPointer) Button(time, button, state uint32) error {
	e := newEncoder(p.conn.order)
	e.putU32(time)
	e.putU32(button)
	e.putU32(state)
	return p.conn.send(p.id, vptrReqButton, e.buf, nil)
}

// Frame groups the preceding pointer requests into one logical event, as the
// compositor requires before it dispatches them.
func (p *VirtualPointer) Frame() error {
	return p.conn.send(p.id, vptrReqFrame, nil, nil)
}

// Destroy releases the virtual pointer.
func (p *VirtualPointer) Destroy() error {
	err := p.conn.send(p.id, vptrReqDestroy, nil, nil)
	p.conn.unregister(p.id)
	return err
}
