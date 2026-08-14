// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/window/internal/x11"
)

var le = binary.LittleEndian

// fakeTransport is an in-memory io.ReadWriteCloser: reads drain a preloaded
// server→client script, writes are captured for inspection.
type fakeTransport struct {
	in     *bytes.Reader
	out    bytes.Buffer
	closed bool
}

func (f *fakeTransport) Read(p []byte) (int, error)  { return f.in.Read(p) }
func (f *fakeTransport) Write(p []byte) (int, error) { return f.out.Write(p) }
func (f *fakeTransport) Close() error                { f.closed = true; return nil }

var _ io.ReadWriteCloser = (*fakeTransport)(nil)

const (
	atomWMProtocols    = 0x100
	atomWMDeleteWindow = 0x101
	atomRepaint        = 0x102 // _GO_WIDGETS_REPAINT, the Repainter wakeup
)

// setupReply builds a success connection-setup reply (LE): one screen, one
// depth-24 TrueColor visual, one depth-24/32bpp format, keycodes 8..9.
func setupReply() []byte {
	var b bytes.Buffer
	w32 := func(v uint32) { _ = binary.Write(&b, le, v) }
	w16 := func(v uint16) { _ = binary.Write(&b, le, v) }
	w8 := func(v uint8) { b.WriteByte(v) }
	pad := func(n int) { b.Write(make([]byte, n)) }

	w32(1)          // release
	w32(0x04000000) // resource-id-base
	w32(0x001fffff) // resource-id-mask
	w32(0)          // motion-buffer
	w16(4)          // vendor length ("Test")
	w16(0xffff)     // maximum-request-length
	w8(1)           // screens
	w8(1)           // formats
	w8(0)           // image-byte-order LSB
	w8(0)           // bitmap-bit-order
	w8(32)          // scanline-unit
	w8(32)          // scanline-pad
	w8(8)           // min-keycode
	w8(9)           // max-keycode
	pad(4)
	b.WriteString("Test")
	// FORMAT
	w8(24)
	w8(32)
	w8(32)
	pad(5)
	// SCREEN
	w32(0x123)      // root
	w32(0x22)       // default-colormap
	w32(0x00ffffff) // white
	w32(0)          // black
	w32(0)          // input-masks
	w16(800)        // width
	w16(600)        // height
	w16(300)        // width-mm
	w16(200)        // height-mm
	w16(1)          // min-maps
	w16(1)          // max-maps
	w32(0x21)       // root-visual
	w8(0)           // backing
	w8(0)           // save-unders
	w8(24)          // root-depth
	w8(1)           // depths
	// DEPTH
	w8(24)
	pad(1)
	w16(1) // visuals
	pad(4)
	// VISUALTYPE
	w32(0x21)       // id
	w8(4)           // TrueColor
	w8(8)           // bits-per-rgb
	w16(256)        // colormap-entries
	w32(0x00ff0000) // red
	w32(0x0000ff00) // green
	w32(0x000000ff) // blue
	pad(4)

	body := b.Bytes()
	hdr := make([]byte, 8)
	hdr[0] = 1 // Success
	le.PutUint16(hdr[2:4], 11)
	le.PutUint16(hdr[6:8], uint16(len(body)/4))
	return append(hdr, body...)
}

func internReply(atom uint32) []byte {
	pkt := make([]byte, 32)
	pkt[0] = 1 // reply
	le.PutUint32(pkt[8:12], atom)
	return pkt
}

func keymapReply(perCode byte, syms []uint32) []byte {
	extra := make([]byte, 4*len(syms))
	for i, s := range syms {
		le.PutUint32(extra[i*4:], s)
	}
	pkt := make([]byte, 32+len(extra))
	pkt[0] = 1
	pkt[1] = perCode
	le.PutUint32(pkt[4:8], uint32(len(extra)/4))
	copy(pkt[32:], extra)
	return pkt
}

// clientMessage builds a WM_DELETE_WINDOW ClientMessage event.
func deleteMessage() []byte {
	pkt := make([]byte, 32)
	pkt[0] = 33 // ClientMessage
	pkt[1] = 32 // format
	le.PutUint32(pkt[4:8], 0x333)
	le.PutUint32(pkt[8:12], atomWMProtocols)
	le.PutUint32(pkt[12:16], atomWMDeleteWindow)
	return pkt
}

func buttonPress(button byte, x, y int16, state uint16) []byte {
	pkt := make([]byte, 32)
	pkt[0] = 4 // ButtonPress
	pkt[1] = button
	le.PutUint16(pkt[24:26], uint16(x))
	le.PutUint16(pkt[26:28], uint16(y))
	le.PutUint16(pkt[28:30], state)
	return pkt
}

func keyPress(keycode byte, state uint16) []byte {
	pkt := make([]byte, 32)
	pkt[0] = 2 // KeyPress
	pkt[1] = keycode
	le.PutUint16(pkt[28:30], state)
	return pkt
}

func configureNotify(w, h uint16) []byte {
	pkt := make([]byte, 32)
	pkt[0] = 22 // ConfigureNotify
	le.PutUint32(pkt[8:12], 0x123)
	le.PutUint16(pkt[20:22], w)
	le.PutUint16(pkt[22:24], h)
	return pkt
}

// serverScript concatenates the setup reply, the two InternAtom replies and
// the keymap reply newWindow consumes, then the given event stream.
func serverScript(events ...[]byte) []byte {
	out := setupReply()
	out = append(out, internReply(atomWMProtocols)...)
	out = append(out, internReply(atomWMDeleteWindow)...)
	out = append(out, internReply(atomRepaint)...)
	out = append(out, keymapReply(2, []uint32{0x61, 0x41, 0xff0d, 0})...) // kc8=a/A, kc9=Return
	for _, e := range events {
		out = append(out, e...)
	}
	return out
}

// dialFake constructs a Window over a scripted fake transport.
func dialFake(t *testing.T, cfg Config, events ...[]byte) (*Window, *fakeTransport) {
	t.Helper()
	ft := &fakeTransport{in: bytes.NewReader(serverScript(events...))}
	conn, err := x11.Handshake(ft, le, "", nil)
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	w, err := newWindow(conn, cfg)
	if err != nil {
		t.Fatalf("newWindow: %v", err)
	}
	return w, ft
}

// dialFakeWithKeymap is dialFake with a custom keyboard-mapping reply.
func dialFakeWithKeymap(t *testing.T, cfg Config, perCode byte, syms []uint32, events ...[]byte) (*Window, *fakeTransport) {
	t.Helper()
	script := setupReply()
	script = append(script, internReply(atomWMProtocols)...)
	script = append(script, internReply(atomWMDeleteWindow)...)
	script = append(script, internReply(atomRepaint)...)
	script = append(script, keymapReply(perCode, syms)...)
	for _, e := range events {
		script = append(script, e...)
	}
	ft := &fakeTransport{in: bytes.NewReader(script)}
	conn, err := x11.Handshake(ft, le, "", nil)
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	w, err := newWindow(conn, cfg)
	if err != nil {
		t.Fatalf("newWindow: %v", err)
	}
	return w, ft
}

// recWidget records every event delivered to it.
type recWidget struct {
	toolkit.Base
	events []toolkit.Event
	drawn  int
}

func (r *recWidget) OnEvent(ev toolkit.Event)                  { r.events = append(r.events, ev) }
func (r *recWidget) Draw(p painter.Painter, th *toolkit.Theme) { r.drawn++ }

func TestNewWindowConfigures(t *testing.T) {
	w, ft := dialFake(t, Config{Title: "T", Width: 320, Height: 240})
	if gw, gh := w.Size(); gw != 320 || gh != 240 {
		t.Fatalf("size = %dx%d", gw, gh)
	}
	if w.wmProtocols != atomWMProtocols || w.wmDeleteWindow != atomWMDeleteWindow {
		t.Fatalf("wm atoms wrong: %#x %#x", w.wmProtocols, w.wmDeleteWindow)
	}
	if w.keymap.Keysym(8, false) != 0x61 {
		t.Fatalf("keymap not fetched")
	}
	// A CreateWindow request must have been emitted (opcode 1).
	if ft.out.Len() == 0 || ft.out.Bytes()[0] == 0 {
		t.Fatalf("no requests emitted")
	}
	if s := w.String(); s == "" {
		t.Fatalf("String empty")
	}
}

func TestNewWindowDefaults(t *testing.T) {
	// Zero size falls back to 640x480; nil theme to DefaultDark.
	w, _ := dialFake(t, Config{})
	if gw, gh := w.Size(); gw != 640 || gh != 480 {
		t.Fatalf("default size = %dx%d", gw, gh)
	}
	if w.theme == nil {
		t.Fatalf("theme not defaulted")
	}
}

func TestRunClickThenClose(t *testing.T) {
	w, _ := dialFake(t, Config{Title: "T", Width: 100, Height: 80},
		buttonPress(1, 33, 44, 0),
		deleteMessage(),
	)
	root := &recWidget{}
	if err := w.Run(root); err != nil {
		t.Fatalf("Run: %v", err)
	}
	// The click must have been dispatched with the right coords.
	var click *toolkit.Event
	for i := range root.events {
		if root.events[i].Kind == toolkit.EventClick {
			click = &root.events[i]
		}
	}
	if click == nil {
		t.Fatalf("no EventClick dispatched; got %+v", root.events)
	}
	if click.X != 33 || click.Y != 44 {
		t.Fatalf("click coords = (%d,%d) want (33,44)", click.X, click.Y)
	}
	if root.drawn == 0 {
		t.Fatalf("root never drawn")
	}
}

func TestRunKeyAndResizeAndExpose(t *testing.T) {
	w, _ := dialFake(t, Config{Width: 100, Height: 80},
		keyPress(9, 0),            // keycode 9 -> Return -> "Enter"
		configureNotify(200, 160), // resize
		exposeEvent(),             // repaint
		deleteMessage(),
	)
	root := &recWidget{}
	if err := w.Run(root); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if gw, gh := w.Size(); gw != 200 || gh != 160 {
		t.Fatalf("size after resize = %dx%d", gw, gh)
	}
	var gotEnter bool
	for _, ev := range root.events {
		if ev.Kind == toolkit.EventKeyDown && ev.Code == "Enter" {
			gotEnter = true
		}
	}
	if !gotEnter {
		t.Fatalf("Enter key not dispatched; got %+v", root.events)
	}
}

func exposeEvent() []byte {
	pkt := make([]byte, 32)
	pkt[0] = 12 // Expose
	le.PutUint32(pkt[4:8], 0x123)
	le.PutUint16(pkt[8:10], 0)
	le.PutUint16(pkt[10:12], 0)
	le.PutUint16(pkt[12:14], 100)
	le.PutUint16(pkt[14:16], 80)
	return pkt
}

// failWriter is a transport whose writes start failing once fail is set.
type failWriter struct {
	in   *bytes.Reader
	fail bool
}

func (f *failWriter) Read(p []byte) (int, error) { return f.in.Read(p) }
func (f *failWriter) Write(p []byte) (int, error) {
	if f.fail {
		return 0, io.ErrClosedPipe
	}
	return len(p), nil
}
func (f *failWriter) Close() error { return nil }

func TestRunPresentError(t *testing.T) {
	fw := &failWriter{in: bytes.NewReader(serverScript())}
	conn, err := x11.Handshake(fw, le, "", nil)
	if err != nil {
		t.Fatalf("handshake: %v", err)
	}
	w, err := newWindow(conn, Config{Width: 10, Height: 10})
	if err != nil {
		t.Fatalf("newWindow: %v", err)
	}
	fw.fail = true // the initial present's PutImage write now fails
	if err := w.Run(&recWidget{}); err == nil {
		t.Fatalf("present error should propagate")
	}
}

func TestRunNextEventError(t *testing.T) {
	// Script ends without a delete message: after events drain, NextEvent
	// hits EOF and Run returns that error.
	w, _ := dialFake(t, Config{Width: 10, Height: 10}, buttonPress(1, 1, 1, 0))
	if err := w.Run(&recWidget{}); err == nil {
		t.Fatalf("EOF from event stream should surface")
	}
}

func TestPresentRectClamping(t *testing.T) {
	w, _ := dialFake(t, Config{Width: 40, Height: 30})
	// Out-of-range rect clamps to the surface; a fully-off rect is a no-op.
	if err := w.presentRect(-5, -5, 100, 100); err != nil {
		t.Fatalf("clamped present: %v", err)
	}
	if err := w.presentRect(1000, 1000, 10, 10); err != nil {
		t.Fatalf("off-surface present should no-op: %v", err)
	}
}

func TestResizeNoop(t *testing.T) {
	w, _ := dialFake(t, Config{Width: 40, Height: 30})
	before := len(w.buf)
	w.resize(0, 0)   // invalid
	w.resize(40, 30) // unchanged
	if len(w.buf) != before {
		t.Fatalf("buffer changed on no-op resize")
	}
	w.resize(20, 20)
	if len(w.buf) != 4*20*20 {
		t.Fatalf("buffer not resized")
	}
}

func TestClose(t *testing.T) {
	w, ft := dialFake(t, Config{Width: 10, Height: 10})
	if err := w.Close(); err != nil || !ft.closed {
		t.Fatalf("close failed: %v closed=%v", err, ft.closed)
	}
	if err := w.Close(); err != nil { // idempotent
		t.Fatalf("second close: %v", err)
	}
}

func TestMapEventBranches(t *testing.T) {
	w, _ := dialFake(t, Config{Width: 100, Height: 80})

	// Unhandled event (MapNotify).
	if out := w.mapEvent(x11.Event{Code: 19}); len(out.events) != 0 || out.quit || out.resize {
		t.Fatalf("unhandled event produced output: %+v", out)
	}
	// Non-delete ClientMessage does nothing.
	if out := w.mapEvent(x11.Event{Code: 33, Format: 32, Atom: atomWMProtocols, Data32: 0x999}); out.quit {
		t.Fatalf("non-delete client message should not quit")
	}
	// Delete ClientMessage quits.
	if out := w.mapEvent(x11.Event{Code: 33, Format: 32, Atom: atomWMProtocols, Data32: atomWMDeleteWindow}); !out.quit {
		t.Fatalf("delete message should quit")
	}
	// Same-size ConfigureNotify is a no-op; a new size resizes.
	if out := w.mapEvent(x11.Event{Code: 22, Width: 100, Height: 80}); out.resize {
		t.Fatalf("same-size configure should not resize")
	}
	if out := w.mapEvent(x11.Event{Code: 22, Width: 200, Height: 160}); !out.resize || out.rw != 200 || out.rh != 160 {
		t.Fatalf("resize outcome wrong: %+v", out)
	}

	// Wheel up/down -> scroll deltas.
	if out := w.mapEvent(x11.Event{Code: 4, Detail: x11.ButtonWheelUp, EventX: 5, EventY: 6}); len(out.events) != 1 || out.events[0].Kind != toolkit.EventScroll || out.events[0].Delta != -1 {
		t.Fatalf("wheel up wrong: %+v", out)
	}
	if out := w.mapEvent(x11.Event{Code: 4, Detail: x11.ButtonWheelDown}); out.events[0].Delta != 1 {
		t.Fatalf("wheel down wrong: %+v", out)
	}
	// Button release (real button) -> mouse up; wheel release -> nothing.
	if out := w.mapEvent(x11.Event{Code: 5, Detail: 1, EventX: 7, EventY: 8}); out.events[0].Kind != toolkit.EventMouseUp || out.events[0].X != 7 {
		t.Fatalf("button release wrong: %+v", out)
	}
	if out := w.mapEvent(x11.Event{Code: 5, Detail: x11.ButtonWheelUp}); len(out.events) != 0 {
		t.Fatalf("wheel release should be empty: %+v", out)
	}
	// Motion: no button -> move; button held -> drag; Ctrl/Shift from state.
	if out := w.mapEvent(x11.Event{Code: 6, EventX: 1, EventY: 2, State: x11.ModControl}); out.events[0].Kind != toolkit.EventMouseMove || !out.events[0].Ctrl {
		t.Fatalf("motion move wrong: %+v", out)
	}
	if out := w.mapEvent(x11.Event{Code: 6, EventX: 1, EventY: 2, State: x11.ModButton1 | x11.ModShift}); out.events[0].Kind != toolkit.EventMouseDrag || !out.events[0].Shift {
		t.Fatalf("motion drag wrong: %+v", out)
	}
	// Printable key press -> KeyDown + Char; release -> KeyUp.
	press := w.mapEvent(x11.Event{Code: 2, Detail: 8}) // keycode 8 -> 'a'
	if len(press.events) != 2 || press.events[0].Kind != toolkit.EventKeyDown || press.events[1].Kind != toolkit.EventChar || press.events[1].Code != "a" {
		t.Fatalf("printable press wrong: %+v", press)
	}
	rel := w.mapEvent(x11.Event{Code: 3, Detail: 8})
	if len(rel.events) != 1 || rel.events[0].Kind != toolkit.EventKeyUp || rel.events[0].Code != "a" {
		t.Fatalf("printable release wrong: %+v", rel)
	}
	// Shifted printable uses level 1 ('A').
	if out := w.mapEvent(x11.Event{Code: 2, Detail: 8, State: x11.ModShift}); out.events[1].Code != "A" {
		t.Fatalf("shifted char wrong: %+v", out)
	}
	// Named key release -> KeyUp with the name.
	if out := w.mapEvent(x11.Event{Code: 3, Detail: 9}); out.events[0].Kind != toolkit.EventKeyUp || out.events[0].Code != "Enter" {
		t.Fatalf("named release wrong: %+v", out)
	}
}

func TestMapKeyModifierAndUnmapped(t *testing.T) {
	// A keymap whose keycode 8 is a Shift modifier and keycode 9 is unmapped.
	w, _ := dialFakeWithKeymap(t, Config{Width: 10, Height: 10}, 1,
		[]uint32{0xffe1 /*Shift_L*/, 0 /*unmapped*/})
	if out := w.mapEvent(x11.Event{Code: 2, Detail: 8}); len(out.events) != 0 {
		t.Fatalf("modifier key should deliver nothing: %+v", out)
	}
	if out := w.mapEvent(x11.Event{Code: 2, Detail: 9}); len(out.events) != 0 || out.repaint {
		t.Fatalf("unmapped key should deliver nothing and not repaint: %+v", out)
	}
}
