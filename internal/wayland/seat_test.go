// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package wayland

import (
	"encoding/binary"
	"errors"
	"os"
	"testing"
)

func TestSeatBind(t *testing.T) {
	order := binary.LittleEndian
	st := &stubTransport{}
	c := NewConn(st, order)
	reg := &Registry{conn: c}
	// No global -> error.
	if _, err := reg.Seat(); err == nil {
		t.Error("Seat with no global should error")
	}
	reg.globals = []Global{{Name: 1, Interface: "wl_seat", Version: 5}}
	seat, err := reg.Seat()
	if err != nil {
		t.Fatalf("Seat: %v", err)
	}
	if _, ok := c.handlers[seat.id]; !ok {
		t.Error("bound seat should register a handler")
	}
}

func TestSeatBindWriteError(t *testing.T) {
	c := NewConn(&stubTransport{writeErr: errors.New("nope")}, binary.LittleEndian)
	reg := &Registry{conn: c}
	reg.globals = []Global{{Name: 1, Interface: "wl_seat", Version: 5}}
	if _, err := reg.Seat(); err == nil {
		t.Error("seat bind write error should propagate")
	}
}

func TestSeatHandle(t *testing.T) {
	order := binary.LittleEndian
	s := &Seat{}
	caps := bodyOf(order, func(e *encoder) { e.putU32(SeatCapabilityPointer | SeatCapabilityKeyboard) })
	if err := s.handle(seatEvtCapabilities, newDecoder(order, caps)); err != nil {
		t.Fatal(err)
	}
	if !s.HasPointer() || !s.HasKeyboard() {
		t.Errorf("caps = %#x", s.Capabilities())
	}
	if s.Capabilities()&SeatCapabilityTouch != 0 {
		t.Error("touch should be absent")
	}
	name := bodyOf(order, func(e *encoder) { e.putString("seat0") })
	if err := s.handle(seatEvtName, newDecoder(order, name)); err != nil {
		t.Fatal(err)
	}
	if s.Name() != "seat0" {
		t.Errorf("name = %q", s.Name())
	}
	// unknown opcode ignored; truncated caps/name error.
	if err := s.handle(99, newDecoder(order, nil)); err != nil {
		t.Errorf("unknown seat opcode = %v", err)
	}
	if err := s.handle(seatEvtCapabilities, newDecoder(order, nil)); err == nil {
		t.Error("truncated capabilities should error")
	}
	if err := s.handle(seatEvtName, newDecoder(order, nil)); err == nil {
		t.Error("truncated name should error")
	}
}

func TestSeatGetDevicesAndRelease(t *testing.T) {
	order := binary.LittleEndian
	st := &stubTransport{}
	c := NewConn(st, order)
	s := &Seat{conn: c, id: 7}
	p, err := s.GetPointer()
	if err != nil {
		t.Fatalf("GetPointer: %v", err)
	}
	if _, ok := c.handlers[p.id]; !ok {
		t.Error("pointer should be registered")
	}
	k, err := s.GetKeyboard()
	if err != nil {
		t.Fatalf("GetKeyboard: %v", err)
	}
	if k.Keymap() == nil {
		t.Error("keyboard should start with an empty keymap")
	}
	if err := s.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if _, ok := c.handlers[s.id]; ok {
		t.Error("Release should unregister the seat")
	}
}

func TestSeatDeviceWriteErrors(t *testing.T) {
	c := NewConn(&stubTransport{writeErr: errors.New("nope")}, binary.LittleEndian)
	s := &Seat{conn: c, id: 7}
	if _, err := s.GetPointer(); err == nil {
		t.Error("GetPointer write error")
	}
	if _, err := s.GetKeyboard(); err == nil {
		t.Error("GetKeyboard write error")
	}
	if err := s.Release(); err == nil {
		t.Error("Release write error")
	}
}

func TestPointerHandle(t *testing.T) {
	order := binary.LittleEndian
	c := NewConn(&stubTransport{}, order)
	p := &Pointer{conn: c, id: 9}

	var enterX, enterY int
	var left bool
	var motX, motY int
	var btn uint32
	var pressed bool
	var axis uint32
	var axisVal int
	p.OnEnter = func(x, y Fixed) { enterX, enterY = x.Int(), y.Int() }
	p.OnLeave = func() { left = true }
	p.OnMotion = func(x, y Fixed) { motX, motY = x.Int(), y.Int() }
	p.OnButton = func(b uint32, pr bool) { btn, pressed = b, pr }
	p.OnAxis = func(a uint32, v Fixed) { axis, axisVal = a, v.Int() }

	must := func(op uint16, body []byte) {
		if err := p.handle(op, newDecoder(order, body)); err != nil {
			t.Fatalf("op %d: %v", op, err)
		}
	}
	must(pointerEvtEnter, bodyOf(order, func(e *encoder) { e.putU32(1); e.putU32(2); e.putFixed(FixedFromInt(30)); e.putFixed(FixedFromInt(40)) }))
	must(pointerEvtLeave, bodyOf(order, func(e *encoder) { e.putU32(1); e.putU32(2) }))
	must(pointerEvtMotion, bodyOf(order, func(e *encoder) { e.putU32(0); e.putFixed(FixedFromInt(11)); e.putFixed(FixedFromInt(12)) }))
	must(pointerEvtButton, bodyOf(order, func(e *encoder) { e.putU32(1); e.putU32(0); e.putU32(BtnLeft); e.putU32(StatePressed) }))
	must(pointerEvtAxis, bodyOf(order, func(e *encoder) { e.putU32(0); e.putU32(AxisVerticalScroll); e.putFixed(FixedFromInt(5)) }))
	if enterX != 30 || enterY != 40 || !left || motX != 11 || motY != 12 || btn != BtnLeft || !pressed || axis != AxisVerticalScroll || axisVal != 5 {
		t.Fatalf("pointer callbacks not all fired: enter(%d,%d) left=%v mot(%d,%d) btn=%d pressed=%v axis=%d val=%d",
			enterX, enterY, left, motX, motY, btn, pressed, axis, axisVal)
	}
	// button release path.
	must(pointerEvtButton, bodyOf(order, func(e *encoder) { e.putU32(1); e.putU32(0); e.putU32(BtnLeft); e.putU32(StateReleased) }))
	if pressed {
		t.Error("release should report pressed=false")
	}
	// unknown opcode ignored.
	if err := p.handle(99, newDecoder(order, nil)); err != nil {
		t.Errorf("unknown pointer opcode = %v", err)
	}
	if err := p.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
}

func TestPointerHandleNilCallbacksAndTruncation(t *testing.T) {
	order := binary.LittleEndian
	p := &Pointer{} // no callbacks set
	full := map[uint16][]byte{
		pointerEvtEnter:  bodyOf(order, func(e *encoder) { e.putU32(0); e.putU32(0); e.putFixed(0); e.putFixed(0) }),
		pointerEvtLeave:  bodyOf(order, func(e *encoder) { e.putU32(0); e.putU32(0) }),
		pointerEvtMotion: bodyOf(order, func(e *encoder) { e.putU32(0); e.putFixed(0); e.putFixed(0) }),
		pointerEvtButton: bodyOf(order, func(e *encoder) { e.putU32(0); e.putU32(0); e.putU32(0); e.putU32(0) }),
		pointerEvtAxis:   bodyOf(order, func(e *encoder) { e.putU32(0); e.putU32(0); e.putFixed(0) }),
	}
	for op, body := range full {
		if err := p.handle(op, newDecoder(order, body)); err != nil {
			t.Errorf("nil-callback op %d: %v", op, err)
		}
		if err := p.handle(op, newDecoder(order, nil)); err == nil {
			t.Errorf("truncated op %d should error", op)
		}
	}
}

// makeKeymapFD writes text (NUL-terminated) to a temp file and returns its
// descriptor and size for a wl_keyboard.keymap event.
func makeKeymapFD(t *testing.T, text string) (int, int) {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "km")
	if err != nil {
		t.Fatalf("temp: %v", err)
	}
	t.Cleanup(func() { _ = f.Close() })
	if _, err := f.WriteString(text + "\x00"); err != nil {
		t.Fatalf("write: %v", err)
	}
	return int(f.Fd()), len(text) + 1
}

func TestKeyboardKeymap(t *testing.T) {
	order := binary.LittleEndian
	fd, size := makeKeymapFD(t, kmText)
	st := &stubTransport{fds: []int{fd}}
	c := NewConn(st, order)
	k := &Keyboard{conn: c, keymap: &Keymap{codeSyms: map[uint32][]string{}}}
	body := bodyOf(order, func(e *encoder) { e.putU32(KeymapFormatXkbV1); e.putU32(uint32(size)) })
	if err := k.handle(keyboardEvtKeymap, newDecoder(order, body)); err != nil {
		t.Fatalf("keymap: %v", err)
	}
	if key := k.Keymap().Lookup(30, false); !key.HasRune || key.Rune != 'a' {
		t.Errorf("parsed keymap Lookup(30) = %+v", key)
	}
}

func TestKeyboardKeymapMissingFD(t *testing.T) {
	order := binary.LittleEndian
	c := NewConn(&stubTransport{}, order) // no fds queued
	k := &Keyboard{conn: c}
	body := bodyOf(order, func(e *encoder) { e.putU32(KeymapFormatXkbV1); e.putU32(10) })
	if err := k.handle(keyboardEvtKeymap, newDecoder(order, body)); err == nil {
		t.Error("keymap with no fd should error")
	}
}

func TestKeyboardKeymapNonXkb(t *testing.T) {
	order := binary.LittleEndian
	fd, _ := makeKeymapFD(t, "irrelevant")
	st := &stubTransport{fds: []int{fd}}
	c := NewConn(st, order)
	empty := &Keymap{codeSyms: map[uint32][]string{}}
	k := &Keyboard{conn: c, keymap: empty}
	body := bodyOf(order, func(e *encoder) { e.putU32(KeymapFormatNoKeymap); e.putU32(5) })
	if err := k.handle(keyboardEvtKeymap, newDecoder(order, body)); err != nil {
		t.Fatalf("non-xkb keymap: %v", err)
	}
	if k.Keymap() != empty {
		t.Error("non-xkb keymap should leave the keymap unchanged")
	}
}

func TestKeyboardKeymapMmapError(t *testing.T) {
	order := binary.LittleEndian
	fd, size := makeKeymapFD(t, kmText)
	st := &stubTransport{fds: []int{fd}}
	c := NewConn(st, order)
	k := &Keyboard{conn: c}
	orig := mapReadOnly
	mapReadOnly = func(int, int) ([]byte, error) { return nil, errors.New("mmap boom") }
	defer func() { mapReadOnly = orig }()
	body := bodyOf(order, func(e *encoder) { e.putU32(KeymapFormatXkbV1); e.putU32(uint32(size)) })
	if err := k.handle(keyboardEvtKeymap, newDecoder(order, body)); err == nil {
		t.Error("keymap mmap error should propagate")
	}
}

func TestKeyboardKeymapTruncated(t *testing.T) {
	order := binary.LittleEndian
	c := NewConn(&stubTransport{}, order)
	k := &Keyboard{conn: c}
	if err := k.handle(keyboardEvtKeymap, newDecoder(order, nil)); err == nil {
		t.Error("truncated keymap should error")
	}
}

func TestKeyboardEvents(t *testing.T) {
	order := binary.LittleEndian
	c := NewConn(&stubTransport{}, order)
	k := &Keyboard{conn: c, id: 10, keymap: &Keymap{codeSyms: map[uint32][]string{}}}

	var entered, leftFocus, modsSeen bool
	var keyCode uint32
	var keyPressed bool
	k.OnEnter = func() { entered = true }
	k.OnLeave = func() { leftFocus = true }
	k.OnKey = func(code uint32, pressed bool) { keyCode, keyPressed = code, pressed }
	k.OnModifiers = func() { modsSeen = true }

	must := func(op uint16, body []byte) {
		if err := k.handle(op, newDecoder(order, body)); err != nil {
			t.Fatalf("op %d: %v", op, err)
		}
	}
	must(keyboardEvtEnter, bodyOf(order, func(e *encoder) { e.putU32(1); e.putU32(2); e.putArray([]byte{30, 0, 0, 0}) }))
	must(keyboardEvtLeave, bodyOf(order, func(e *encoder) { e.putU32(1); e.putU32(2) }))
	must(keyboardEvtKey, bodyOf(order, func(e *encoder) { e.putU32(1); e.putU32(0); e.putU32(30); e.putU32(StatePressed) }))
	must(keyboardEvtModifiers, bodyOf(order, func(e *encoder) {
		e.putU32(1)
		e.putU32(modMaskShift | modMaskControl | modMaskLogo)
		e.putU32(0)
		e.putU32(0)
		e.putU32(0)
	}))
	must(keyboardEvtRepeatInfo, bodyOf(order, func(e *encoder) { e.putI32(25); e.putI32(600) }))

	if !entered || !leftFocus || keyCode != 30 || !keyPressed || !modsSeen {
		t.Fatalf("keyboard callbacks: entered=%v left=%v key=%d pressed=%v mods=%v", entered, leftFocus, keyCode, keyPressed, modsSeen)
	}
	// Shift+Ctrl+Super held (no Alt): Logo (the Meta/⌘ key) reports true.
	if !k.Shift() || !k.Ctrl() || k.Alt() || !k.Logo() {
		t.Errorf("modifiers shift=%v ctrl=%v alt=%v logo=%v", k.Shift(), k.Ctrl(), k.Alt(), k.Logo())
	}
	if k.RepeatRate() != 25 || k.RepeatDelay() != 600 {
		t.Errorf("repeat = %d/%d", k.RepeatRate(), k.RepeatDelay())
	}
	// unknown opcode ignored.
	if err := k.handle(99, newDecoder(order, nil)); err != nil {
		t.Errorf("unknown keyboard opcode = %v", err)
	}
	if err := k.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
}

func TestKeyboardEventsNilCallbacksAndTruncation(t *testing.T) {
	order := binary.LittleEndian
	c := NewConn(&stubTransport{}, order)
	k := &Keyboard{conn: c, keymap: &Keymap{codeSyms: map[uint32][]string{}}} // no callbacks
	full := map[uint16][]byte{
		keyboardEvtEnter:      bodyOf(order, func(e *encoder) { e.putU32(0); e.putU32(0); e.putArray(nil) }),
		keyboardEvtLeave:      bodyOf(order, func(e *encoder) { e.putU32(0); e.putU32(0) }),
		keyboardEvtKey:        bodyOf(order, func(e *encoder) { e.putU32(0); e.putU32(0); e.putU32(0); e.putU32(0) }),
		keyboardEvtModifiers:  bodyOf(order, func(e *encoder) { e.putU32(0); e.putU32(0); e.putU32(0); e.putU32(0); e.putU32(0) }),
		keyboardEvtRepeatInfo: bodyOf(order, func(e *encoder) { e.putI32(0); e.putI32(0) }),
	}
	for op, body := range full {
		if err := k.handle(op, newDecoder(order, body)); err != nil {
			t.Errorf("nil-callback op %d: %v", op, err)
		}
		if err := k.handle(op, newDecoder(order, nil)); err == nil {
			t.Errorf("truncated op %d should error", op)
		}
	}
}

func TestKeyboardReleaseWriteError(t *testing.T) {
	c := NewConn(&stubTransport{writeErr: errors.New("nope")}, binary.LittleEndian)
	k := &Keyboard{conn: c, id: 10}
	if err := k.Release(); err == nil {
		t.Error("keyboard Release write error should propagate")
	}
	p := &Pointer{conn: c, id: 9}
	if err := p.Release(); err == nil {
		t.Error("pointer Release write error should propagate")
	}
}

func TestCstr(t *testing.T) {
	if got := cstr([]byte("hello\x00world")); got != "hello" {
		t.Errorf("cstr = %q", got)
	}
	if got := cstr([]byte("nonul")); got != "nonul" {
		t.Errorf("cstr no NUL = %q", got)
	}
}

// Wayland refuses to let a client act on the user's behalf out of nowhere:
// taking the clipboard quotes the serial of the input event that justifies it.
// Every event carrying one used to be decoded and thrown away, so there was
// nothing to quote.
func TestSeatRecordsInputSerials(t *testing.T) {
	c := NewConn(&stubTransport{}, binary.LittleEndian)
	seat := &Seat{conn: c, id: 7}

	if got := seat.LastSerial(); got != 0 {
		t.Errorf("a seat nobody has touched reports serial %d, want 0", got)
	}

	p := &Pointer{conn: c, id: 8, seat: seat}
	k := &Keyboard{conn: c, id: 9, seat: seat, keymap: &Keymap{codeSyms: map[uint32][]string{}}}

	// A pointer enter carries a serial: serial, surface, x, y.
	e := newEncoder(c.order)
	e.putU32(4242)
	e.putU32(1)
	e.putFixed(0)
	e.putFixed(0)
	if err := p.handle(pointerEvtEnter, newDecoder(c.order, e.buf)); err != nil {
		t.Fatalf("pointer enter: %v", err)
	}
	if got := seat.LastSerial(); got != 4242 {
		t.Errorf("after a pointer enter the seat reports %d, want 4242", got)
	}

	// A keyboard event supersedes it: the latest interaction is the one that
	// justifies the next request.
	e = newEncoder(c.order)
	e.putU32(5555)
	e.putU32(1)
	if err := k.handle(keyboardEvtLeave, newDecoder(c.order, e.buf)); err != nil {
		t.Fatalf("keyboard leave: %v", err)
	}
	if got := seat.LastSerial(); got != 5555 {
		t.Errorf("after a keyboard event the seat reports %d, want 5555", got)
	}

	// A zero is not a serial the compositor issued, and must not erase a real
	// one — quoting 0 gets a request refused.
	seat.noteSerial(0)
	if got := seat.LastSerial(); got != 5555 {
		t.Errorf("a zero serial overwrote a real one: %d", got)
	}

	// A device with no seat is not a crash.
	(&Pointer{}).noteSerial(1)
	(&Keyboard{}).noteSerial(1)
}
