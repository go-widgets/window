// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package wayland

import (
	"fmt"
	"syscall"
)

// Seat capability bits (wl_seat.capability).
const (
	SeatCapabilityPointer  = 1
	SeatCapabilityKeyboard = 2
	SeatCapabilityTouch    = 4
)

// Seat is the wl_seat global: a group of input devices (pointer, keyboard,
// touch). It advertises which devices are present and manufactures the
// per-device proxies.
type Seat struct {
	conn *Conn
	id   uint32
	caps uint32
	name string
}

const seatIfaceVersion = 5

// wl_seat request opcodes.
const (
	seatReqGetPointer  = 0
	seatReqGetKeyboard = 1
	seatReqRelease     = 3
)

// wl_seat event opcodes.
const (
	seatEvtCapabilities = 0
	seatEvtName         = 1
)

// Seat finds and binds the wl_seat global.
func (r *Registry) Seat() (*Seat, error) {
	g, ok := r.Find("wl_seat")
	if !ok {
		return nil, fmt.Errorf("wayland: compositor advertises no wl_seat")
	}
	ver := min32(g.Version, seatIfaceVersion)
	id, err := r.bind(g.Name, "wl_seat", ver)
	if err != nil {
		return nil, err
	}
	s := &Seat{conn: r.conn, id: id}
	r.conn.register(id, s.handle)
	return s, nil
}

// handle records advertised capabilities and the seat name.
func (s *Seat) handle(opcode uint16, d *decoder) error {
	switch opcode {
	case seatEvtCapabilities:
		caps := d.getU32()
		if !d.ok {
			return fmt.Errorf("wayland: truncated wl_seat.capabilities")
		}
		s.caps = caps
		return nil
	case seatEvtName:
		name := d.getString()
		if !d.ok {
			return fmt.Errorf("wayland: truncated wl_seat.name")
		}
		s.name = name
		return nil
	default:
		return nil
	}
}

// Capabilities returns the advertised capability bitmask.
func (s *Seat) Capabilities() uint32 { return s.caps }

// Name returns the seat's human-readable name.
func (s *Seat) Name() string { return s.name }

// HasPointer reports whether the seat has a pointer device.
func (s *Seat) HasPointer() bool { return s.caps&SeatCapabilityPointer != 0 }

// HasKeyboard reports whether the seat has a keyboard device.
func (s *Seat) HasKeyboard() bool { return s.caps&SeatCapabilityKeyboard != 0 }

// GetPointer obtains the seat's pointer device.
func (s *Seat) GetPointer() (*Pointer, error) {
	id := s.conn.allocID()
	e := newEncoder(s.conn.order)
	e.putU32(id)
	if err := s.conn.send(s.id, seatReqGetPointer, e.buf, nil); err != nil {
		return nil, err
	}
	p := &Pointer{conn: s.conn, id: id}
	s.conn.register(id, p.handle)
	return p, nil
}

// GetKeyboard obtains the seat's keyboard device.
func (s *Seat) GetKeyboard() (*Keyboard, error) {
	id := s.conn.allocID()
	e := newEncoder(s.conn.order)
	e.putU32(id)
	if err := s.conn.send(s.id, seatReqGetKeyboard, e.buf, nil); err != nil {
		return nil, err
	}
	k := &Keyboard{conn: s.conn, id: id, keymap: &Keymap{codeSyms: map[uint32][]string{}}}
	s.conn.register(id, k.handle)
	return k, nil
}

// Release releases the seat object.
func (s *Seat) Release() error {
	err := s.conn.send(s.id, seatReqRelease, nil, nil)
	s.conn.unregister(s.id)
	return err
}

// --- wl_pointer -----------------------------------------------------------

// Linux input-event-codes button numbers reported by wl_pointer.button.
const (
	BtnLeft   = 0x110
	BtnRight  = 0x111
	BtnMiddle = 0x112
)

// wl_pointer.button / wl_keyboard.key state values.
const (
	StateReleased = 0
	StatePressed  = 1
)

// wl_pointer.axis values.
const (
	AxisVerticalScroll   = 0
	AxisHorizontalScroll = 1
)

// Pointer is a wl_pointer device. It decodes enter/leave/motion/button/axis
// events and delivers them through the callback fields the window layer sets.
type Pointer struct {
	conn *Conn
	id   uint32

	OnEnter  func(x, y Fixed)
	OnLeave  func()
	OnMotion func(x, y Fixed)
	OnButton func(button uint32, pressed bool)
	OnAxis   func(axis uint32, value Fixed)
}

// wl_pointer event opcodes.
const (
	pointerEvtEnter  = 0
	pointerEvtLeave  = 1
	pointerEvtMotion = 2
	pointerEvtButton = 3
	pointerEvtAxis   = 4
)

// wl_pointer request opcode.
const pointerReqRelease = 1

// handle decodes a pointer event and invokes the matching callback.
func (p *Pointer) handle(opcode uint16, d *decoder) error {
	switch opcode {
	case pointerEvtEnter:
		d.getU32() // serial
		d.getU32() // surface
		x := d.getFixed()
		y := d.getFixed()
		if !d.ok {
			return fmt.Errorf("wayland: truncated wl_pointer.enter")
		}
		if p.OnEnter != nil {
			p.OnEnter(x, y)
		}
	case pointerEvtLeave:
		d.getU32() // serial
		d.getU32() // surface
		if !d.ok {
			return fmt.Errorf("wayland: truncated wl_pointer.leave")
		}
		if p.OnLeave != nil {
			p.OnLeave()
		}
	case pointerEvtMotion:
		d.getU32() // time
		x := d.getFixed()
		y := d.getFixed()
		if !d.ok {
			return fmt.Errorf("wayland: truncated wl_pointer.motion")
		}
		if p.OnMotion != nil {
			p.OnMotion(x, y)
		}
	case pointerEvtButton:
		d.getU32() // serial
		d.getU32() // time
		button := d.getU32()
		state := d.getU32()
		if !d.ok {
			return fmt.Errorf("wayland: truncated wl_pointer.button")
		}
		if p.OnButton != nil {
			p.OnButton(button, state == StatePressed)
		}
	case pointerEvtAxis:
		d.getU32() // time
		axis := d.getU32()
		value := d.getFixed()
		if !d.ok {
			return fmt.Errorf("wayland: truncated wl_pointer.axis")
		}
		if p.OnAxis != nil {
			p.OnAxis(axis, value)
		}
	}
	return nil
}

// Release releases the pointer object.
func (p *Pointer) Release() error {
	err := p.conn.send(p.id, pointerReqRelease, nil, nil)
	p.conn.unregister(p.id)
	return err
}

// --- wl_keyboard ----------------------------------------------------------

// wl_keyboard.keymap format values.
const (
	KeymapFormatNoKeymap = 0
	KeymapFormatXkbV1    = 1
)

// Core X11-compatible modifier mask bits used by standard keymaps; the
// wl_keyboard.modifiers masks follow this convention for the real modifiers.
const (
	modMaskShift   = 1 << 0
	modMaskControl = 1 << 2
	modMaskAlt     = 1 << 3
)

// Keyboard is a wl_keyboard device. It ingests the xkb keymap, tracks
// modifier state and delivers key press/release through OnKey.
type Keyboard struct {
	conn   *Conn
	id     uint32
	keymap *Keymap
	mods   uint32

	repeatRate  int
	repeatDelay int

	OnKey       func(evdevCode uint32, pressed bool)
	OnModifiers func()
	OnEnter     func()
	OnLeave     func()
}

// wl_keyboard event opcodes.
const (
	keyboardEvtKeymap     = 0
	keyboardEvtEnter      = 1
	keyboardEvtLeave      = 2
	keyboardEvtKey        = 3
	keyboardEvtModifiers  = 4
	keyboardEvtRepeatInfo = 5
)

// wl_keyboard request opcode.
const keyboardReqRelease = 0

// mapReadOnly and unmapReadOnly wrap the keymap-fd mmap syscalls behind
// package variables so a test can exercise the failure path.
var (
	mapReadOnly = func(fd, size int) ([]byte, error) {
		return syscall.Mmap(fd, 0, size, syscall.PROT_READ, syscall.MAP_PRIVATE)
	}
	unmapReadOnly = syscall.Munmap
)

// handle decodes a keyboard event.
func (k *Keyboard) handle(opcode uint16, d *decoder) error {
	switch opcode {
	case keyboardEvtKeymap:
		return k.handleKeymap(d)
	case keyboardEvtEnter:
		d.getU32()   // serial
		d.getU32()   // surface
		d.getArray() // pressed keys
		if !d.ok {
			return fmt.Errorf("wayland: truncated wl_keyboard.enter")
		}
		if k.OnEnter != nil {
			k.OnEnter()
		}
	case keyboardEvtLeave:
		d.getU32() // serial
		d.getU32() // surface
		if !d.ok {
			return fmt.Errorf("wayland: truncated wl_keyboard.leave")
		}
		if k.OnLeave != nil {
			k.OnLeave()
		}
	case keyboardEvtKey:
		d.getU32() // serial
		d.getU32() // time
		key := d.getU32()
		state := d.getU32()
		if !d.ok {
			return fmt.Errorf("wayland: truncated wl_keyboard.key")
		}
		if k.OnKey != nil {
			k.OnKey(key, state == StatePressed)
		}
	case keyboardEvtModifiers:
		d.getU32() // serial
		dep := d.getU32()
		lat := d.getU32()
		loc := d.getU32()
		d.getU32() // group
		if !d.ok {
			return fmt.Errorf("wayland: truncated wl_keyboard.modifiers")
		}
		k.mods = dep | lat | loc
		if k.OnModifiers != nil {
			k.OnModifiers()
		}
	case keyboardEvtRepeatInfo:
		rate := d.getI32()
		delay := d.getI32()
		if !d.ok {
			return fmt.Errorf("wayland: truncated wl_keyboard.repeat_info")
		}
		k.repeatRate = int(rate)
		k.repeatDelay = int(delay)
	}
	return nil
}

// handleKeymap mmaps the keymap fd, parses it and releases the mapping and
// descriptor. A non-xkb_v1 format leaves the (empty) keymap in place.
func (k *Keyboard) handleKeymap(d *decoder) error {
	format := d.getU32()
	size := d.getU32()
	if !d.ok {
		return fmt.Errorf("wayland: truncated wl_keyboard.keymap")
	}
	fd, ok := k.conn.recvFD()
	if !ok {
		return fmt.Errorf("wayland: wl_keyboard.keymap missing fd")
	}
	defer syscall.Close(fd)
	if format != KeymapFormatXkbV1 {
		return nil
	}
	data, err := mapReadOnly(fd, int(size))
	if err != nil {
		return fmt.Errorf("wayland: keymap mmap: %w", err)
	}
	defer unmapReadOnly(data)
	k.keymap = ParseKeymap(cstr(data))
	return nil
}

// cstr returns the NUL-terminated prefix of b as a string (the keymap text
// includes a trailing NUL within the reported size).
func cstr(b []byte) string {
	for i, c := range b {
		if c == 0 {
			return string(b[:i])
		}
	}
	return string(b)
}

// Keymap returns the parsed keymap.
func (k *Keyboard) Keymap() *Keymap { return k.keymap }

// Shift reports whether Shift is currently held.
func (k *Keyboard) Shift() bool { return k.mods&modMaskShift != 0 }

// Ctrl reports whether Control is currently held.
func (k *Keyboard) Ctrl() bool { return k.mods&modMaskControl != 0 }

// Alt reports whether Alt is currently held.
func (k *Keyboard) Alt() bool { return k.mods&modMaskAlt != 0 }

// RepeatRate returns the key-repeat rate in keys per second (0 disables).
func (k *Keyboard) RepeatRate() int { return k.repeatRate }

// RepeatDelay returns the key-repeat delay in milliseconds.
func (k *Keyboard) RepeatDelay() int { return k.repeatDelay }

// Release releases the keyboard object.
func (k *Keyboard) Release() error {
	err := k.conn.send(k.id, keyboardReqRelease, nil, nil)
	k.conn.unregister(k.id)
	return err
}
