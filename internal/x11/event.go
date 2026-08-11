// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package x11

// Modifier / button state-mask bits carried in pointer and key events.
const (
	ModShift   = 0x0001
	ModLock    = 0x0002
	ModControl = 0x0004
	ModMod1    = 0x0008 // typically Alt
	ModMod4    = 0x0040 // typically Super / the Meta (⌘/Windows/logo) key
	ModButton1 = 0x0100
	ModButton2 = 0x0200
	ModButton3 = 0x0400
)

// Pointer button numbers as reported in a Button event's detail byte.
const (
	Button1         = 1 // left
	Button2         = 2 // middle
	Button3         = 3 // right
	ButtonWheelUp   = 4
	ButtonWheelDown = 5
)

// Event is a decoded X11 event in a flat, protocol-level form. The host
// layer maps it to a toolkit.Event; keeping this struct free of toolkit
// types lets the whole decoder be unit-tested with no UI dependency.
type Event struct {
	Code   byte   // event type with the SendEvent bit stripped
	Synth  bool   // set if the SendEvent bit was present
	Detail byte   // keycode (key events) or button number (button events)
	Seq    uint16 // low 16 bits of the sequence number
	Time   uint32
	Window uint32 // event window
	RootX  int16
	RootY  int16
	EventX int16
	EventY int16
	State  uint16 // modifier + button mask
	X      int16  // Expose/ConfigureNotify origin
	Y      int16
	Width  uint16 // Expose/ConfigureNotify extent
	Height uint16
	Count  uint16 // Expose: remaining rectangles
	Atom   uint32 // ClientMessage: message type
	Format byte   // ClientMessage: data format
	Data32 uint32 // ClientMessage: first 32-bit data word (WM_DELETE_WINDOW)

	// Selection events. Requestor is the window asking (SelectionRequest) or
	// asked (SelectionNotify); Property is the one to write the answer into, or
	// 0 in a SelectionNotify that refuses.
	Requestor uint32
	Selection uint32
	Target    uint32
	Property  uint32
}

// decodeEvent parses a 32-byte X11 event packet. The layout of the shared
// pointer/key events, Expose, ConfigureNotify and ClientMessage are all
// handled; other events decode their common header and are returned with
// the remaining fields zero.
func decodeEvent(order ByteOrder, raw []byte) Event {
	d := newDecoder(order, raw)
	var ev Event
	first := d.get8()
	ev.Synth = first&sendEventBit != 0
	ev.Code = first &^ sendEventBit
	ev.Detail = d.get8()
	ev.Seq = d.get16()

	switch ev.Code {
	case evKeyPress, evKeyRelease, evButtonPress, evButtonRelease, evMotionNotify:
		ev.Time = d.get32()
		_ = d.get32() // root
		ev.Window = d.get32()
		_ = d.get32() // child
		ev.RootX = int16(d.get16())
		ev.RootY = int16(d.get16())
		ev.EventX = int16(d.get16())
		ev.EventY = int16(d.get16())
		ev.State = d.get16()
	case evExpose:
		ev.Window = d.get32()
		ev.X = int16(d.get16())
		ev.Y = int16(d.get16())
		ev.Width = d.get16()
		ev.Height = d.get16()
		ev.Count = d.get16()
	case evConfigureNotify:
		_ = d.get32() // event
		ev.Window = d.get32()
		_ = d.get32() // above-sibling
		ev.X = int16(d.get16())
		ev.Y = int16(d.get16())
		ev.Width = d.get16()
		ev.Height = d.get16()
	case evSelectionRequest:
		ev.Time = d.get32()
		ev.Window = d.get32() // owner
		ev.Requestor = d.get32()
		ev.Selection = d.get32()
		ev.Target = d.get32()
		ev.Property = d.get32()
	case evSelectionNotify:
		ev.Time = d.get32()
		ev.Requestor = d.get32()
		ev.Window = ev.Requestor // the event window, for callers that switch on it
		ev.Selection = d.get32()
		ev.Target = d.get32()
		ev.Property = d.get32()
	case evSelectionClear:
		ev.Time = d.get32()
		ev.Window = d.get32() // owner losing it
		ev.Selection = d.get32()
	case evClientMessage:
		ev.Format = ev.Detail
		ev.Window = d.get32()
		ev.Atom = d.get32()
		ev.Data32 = d.get32()
	}
	return ev
}
