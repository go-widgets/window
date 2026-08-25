// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package wayland

import (
	"encoding/binary"
	"errors"
	"testing"
)

// A compositor sends a burst of properties and then done. A client that acted
// on each one in turn would reallocate its framebuffer several times for one
// screen change — and, worse, would act on a scale that the burst had not
// finished describing.
func TestOutputScaleIsPublishedOnDone(t *testing.T) {
	order := binary.LittleEndian
	c := NewConn(&stubTransport{}, order)
	reg := &Registry{conn: c}
	reg.globals = []Global{{Name: 4, Interface: "wl_output", Version: 3}}

	outs, err := reg.Outputs()
	if err != nil {
		t.Fatalf("Outputs: %v", err)
	}
	if len(outs) != 1 {
		t.Fatalf("bound %d outputs, want 1", len(outs))
	}
	o := outs[0]
	if o.Name() != 4 {
		t.Errorf("registry name = %d, want 4", o.Name())
	}
	if o.ID() == 0 {
		t.Error("the output has no object id, so no surface.enter could ever match it")
	}
	if o.Scale() != 1 {
		t.Errorf("scale before anything was said = %d, want 1", o.Scale())
	}

	var announced []int
	o.OnScale = func(s int) { announced = append(announced, s) }

	// The burst: geometry and mode carry no scale and must not be mistaken for
	// the end of anything.
	_ = o.handle(outputEvtGeometry, decoderOf(order, 0, 0, 300, 200, 0))
	_ = o.handle(outputEvtMode, decoderOf(order, 1, 2560, 1440, 60000))
	_ = o.handle(outputEvtScale, decoderOf(order, 2))
	if o.Scale() != 1 {
		t.Errorf("scale = %d before done, want the previous 1 -- the burst is not finished", o.Scale())
	}
	if len(announced) != 0 {
		t.Errorf("announced %v before done", announced)
	}

	_ = o.handle(outputEvtDone, decoderOf(order))
	if o.Scale() != 2 {
		t.Errorf("scale after done = %d, want 2", o.Scale())
	}
	if len(announced) != 1 || announced[0] != 2 {
		t.Errorf("announced %v, want exactly one 2", announced)
	}

	// A second done with nothing changed announces nothing: a compositor
	// republishes its outputs freely, and a window that reallocated its
	// framebuffer each time would stutter for no reason.
	_ = o.handle(outputEvtDone, decoderOf(order))
	if len(announced) != 1 {
		t.Errorf("announced %v after an unchanged done, want just the one", announced)
	}

	// A nonsense scale is ignored rather than believed: zero would make the
	// framebuffer empty and negative would make it enormous.
	_ = o.handle(outputEvtScale, decoderOf(order, 0))
	_ = o.handle(outputEvtDone, decoderOf(order))
	if o.Scale() != 2 {
		t.Errorf("scale after a zero was offered = %d, want the previous 2", o.Scale())
	}
}

// Every output, not the first: a laptop with an external screen has two, with
// different scales, and which one a window is on is a question only
// wl_surface.enter answers.
func TestOutputsBindsAllOfThem(t *testing.T) {
	c := NewConn(&stubTransport{}, binary.LittleEndian)
	reg := &Registry{conn: c}
	reg.globals = []Global{
		{Name: 1, Interface: "wl_compositor", Version: 4},
		{Name: 4, Interface: "wl_output", Version: 3},
		{Name: 7, Interface: "wl_output", Version: 2},
	}
	outs, err := reg.Outputs()
	if err != nil {
		t.Fatalf("Outputs: %v", err)
	}
	if len(outs) != 2 {
		t.Fatalf("bound %d outputs, want 2", len(outs))
	}
	if outs[0].ID() == outs[1].ID() {
		t.Error("the two outputs share an object id")
	}
}

// A compositor with no screens at all is not an error: a headless session is a
// legitimate thing to run, and the window simply stays at 1:1.
func TestOutputsWithNoScreens(t *testing.T) {
	c := NewConn(&stubTransport{}, binary.LittleEndian)
	reg := &Registry{conn: c}
	reg.globals = []Global{{Name: 1, Interface: "wl_compositor", Version: 4}}

	outs, err := reg.Outputs()
	if err != nil {
		t.Fatalf("Outputs: %v", err)
	}
	if len(outs) != 0 {
		t.Errorf("bound %d outputs on a compositor advertising none", len(outs))
	}
}

// The bind itself can fail, and the caller must hear about it rather than
// quietly drawing at the wrong resolution.
func TestOutputsBindError(t *testing.T) {
	c := NewConn(&stubTransport{writeErr: errors.New("no")}, binary.LittleEndian)
	reg := &Registry{conn: c}
	reg.globals = []Global{{Name: 4, Interface: "wl_output", Version: 3}}

	if _, err := reg.Outputs(); err == nil {
		t.Fatal("a failed bind was swallowed")
	}
}

// set_buffer_scale is the request that stops the compositor upscaling a
// half-resolution buffer, so its wire form is worth asserting.
func TestSetBufferScaleWire(t *testing.T) {
	order := binary.LittleEndian
	st := &stubTransport{}
	c := NewConn(st, order)
	s := &Surface{conn: c, id: 0x30}

	if err := s.SetBufferScale(2); err != nil {
		t.Fatalf("SetBufferScale: %v", err)
	}
	obj, op, d := lastWrite(t, st, order)
	if obj != 0x30 || op != surfaceReqSetBufferScale {
		t.Fatalf("obj=%d op=%d, want %d and set_buffer_scale", obj, op, 0x30)
	}
	if got := d.getU32(); got != 2 {
		t.Errorf("scale = %d, want 2", got)
	}

	// A scale below one is not a scale. Sending it would be a protocol error
	// and the compositor would disconnect us.
	if err := s.SetBufferScale(0); err != nil {
		t.Fatalf("SetBufferScale(0): %v", err)
	}
	_, _, d = lastWrite(t, st, order)
	if got := d.getU32(); got != 1 {
		t.Errorf("scale = %d for a zero request, want 1", got)
	}
}

// A zero-value Output has not heard anything yet and is 1:1, which is what an
// unbound screen means. Reporting 0 would make a framebuffer of no pixels.
func TestZeroOutputIsOneToOne(t *testing.T) {
	var o Output
	if got := o.Scale(); got != 1 {
		t.Errorf("the zero Output reports scale %d, want 1", got)
	}
}

// --- the rest of the burst -------------------------------------------------
//
// The scale is only one of the things a wl_output says about itself. The rest
// is what a client enumerating the DISPLAYS needs: where the output is, how
// big, which way up, and what to call it.

// geometryOf encodes a wl_output.geometry body, whose two strings mean it
// cannot be built out of plain 32-bit words.
func geometryOf(order ByteOrder, x, y, pw, ph int32, mk, model string, transform int32) *decoder {
	e := newEncoder(order)
	e.putI32(x)
	e.putI32(y)
	e.putI32(pw)
	e.putI32(ph)
	e.putI32(1) // subpixel: horizontal RGB
	e.putString(mk)
	e.putString(model)
	e.putI32(transform)
	return newDecoder(order, e.buf)
}

// stringOf encodes a body that is one string, which is what the name and
// description events of wl_output 4 carry.
func stringOf(order ByteOrder, s string) *decoder {
	e := newEncoder(order)
	e.putString(s)
	return newDecoder(order, e.buf)
}

// oneOutput binds a single output for a test to drive by hand.
func oneOutput(t *testing.T, order ByteOrder) *Output {
	t.Helper()
	c := NewConn(&stubTransport{}, order)
	reg := &Registry{conn: c}
	reg.globals = []Global{{Name: 4, Interface: "wl_output", Version: 4}}
	outs, err := reg.Outputs()
	if err != nil {
		t.Fatalf("Outputs: %v", err)
	}
	return outs[0]
}

func TestOutputPublishesItsWholeDescriptionOnDone(t *testing.T) {
	order := binary.LittleEndian
	o := oneOutput(t, order)

	// Everything before done says nothing: a client acting on half a burst
	// places the output where the compositor never said it was.
	_ = o.handle(outputEvtGeometry, geometryOf(order, 1920, -120, 597, 336, "DELL", "U2720Q", 0))
	// Every mode the panel supports is announced; the one without the current
	// flag is not the answer to "how big is this screen".
	_ = o.handle(outputEvtMode, decoderOf(order, 0, 640, 480, 60000))
	_ = o.handle(outputEvtMode, decoderOf(order, 1, 3840, 2160, 59951))
	_ = o.handle(outputEvtScale, decoderOf(order, 2))
	_ = o.handle(outputEvtName, stringOf(order, "DP-2"))
	_ = o.handle(outputEvtDescription, stringOf(order, "Dell 27 inch"))
	if x, y := o.Position(); x != 0 || y != 0 {
		t.Errorf("position before done = %d,%d, want the unset 0,0", x, y)
	}

	_ = o.handle(outputEvtDone, decoderOf(order))
	if x, y := o.Position(); x != 1920 || y != -120 {
		t.Errorf("position = %d,%d, want 1920,-120", x, y)
	}
	if w, h := o.ModeSize(); w != 3840 || h != 2160 {
		t.Errorf("mode = %dx%d, want the CURRENT 3840x2160", w, h)
	}
	if w, h := o.LogicalSize(); w != 1920 || h != 1080 {
		t.Errorf("logical size = %dx%d, want the mode divided by the scale", w, h)
	}
	if w, h := o.PhysicalSize(); w != 597 || h != 336 {
		t.Errorf("physical size = %dx%d mm, want 597x336", w, h)
	}
	if o.Make() != "DELL" || o.Model() != "U2720Q" {
		t.Errorf("make/model = %q/%q, want DELL/U2720Q", o.Make(), o.Model())
	}
	if o.Connector() != "DP-2" || o.Description() != "Dell 27 inch" {
		t.Errorf("connector/description = %q/%q, want DP-2/Dell 27 inch",
			o.Connector(), o.Description())
	}
	if o.Refresh() != 59951 {
		t.Errorf("refresh = %d mHz, want 59951", o.Refresh())
	}
}

// A quarter turn swaps the axes. Reporting a portrait panel unswapped overlaps
// whatever sits beside it in the layout, and nothing anywhere says so.
func TestOutputLogicalSizeFollowsTheTransform(t *testing.T) {
	order := binary.LittleEndian
	for _, tc := range []struct {
		transform int32
		wantW     int
		wantH     int
		whatItIs  string
	}{
		{0, 1920, 1080, "normal"},
		{1, 1080, 1920, "90 degrees"},
		{2, 1920, 1080, "180 degrees"},
		{3, 1080, 1920, "270 degrees"},
		{4, 1920, 1080, "flipped"},
		{5, 1080, 1920, "flipped and turned 90"},
	} {
		t.Run(tc.whatItIs, func(t *testing.T) {
			o := oneOutput(t, order)
			_ = o.handle(outputEvtGeometry, geometryOf(order, 0, 0, 0, 0, "", "", tc.transform))
			_ = o.handle(outputEvtMode, decoderOf(order, 1, 1920, 1080, 60000))
			_ = o.handle(outputEvtDone, decoderOf(order))
			if w, h := o.LogicalSize(); w != tc.wantW || h != tc.wantH {
				t.Errorf("logical size = %dx%d, want %dx%d", w, h, tc.wantW, tc.wantH)
			}
		})
	}
}

// A truncated event describes nothing, and half of it is worse than none of
// it: the decoder has already gone not-OK, so every field read after the cut
// is a zero that would be taken for a real value.
func TestOutputIgnoresTruncatedEvents(t *testing.T) {
	order := binary.LittleEndian
	o := oneOutput(t, order)

	_ = o.handle(outputEvtGeometry, decoderOf(order, 100, 200)) // cut before the strings
	_ = o.handle(outputEvtMode, decoderOf(order, 1, 1920))      // cut before the height
	_ = o.handle(outputEvtName, decoderOf(order))
	_ = o.handle(outputEvtDescription, decoderOf(order))
	_ = o.handle(outputEvtDone, decoderOf(order))

	if x, y := o.Position(); x != 0 || y != 0 {
		t.Errorf("position = %d,%d from a truncated geometry, want 0,0", x, y)
	}
	if w, h := o.ModeSize(); w != 0 || h != 0 {
		t.Errorf("mode = %dx%d from a truncated mode event, want 0x0", w, h)
	}
	if o.Connector() != "" || o.Description() != "" {
		t.Errorf("connector/description = %q/%q from truncated events, want empty",
			o.Connector(), o.Description())
	}
}

// A compositor republishes its outputs freely. A done that changes nothing
// must announce nothing, or a window reallocates its framebuffer for no
// reason — and one that changes something OTHER than the scale must not
// announce a scale change either.
func TestOutputDoneWithoutAScaleChange(t *testing.T) {
	order := binary.LittleEndian
	o := oneOutput(t, order)
	announced := 0
	o.OnScale = func(int) { announced++ }

	_ = o.handle(outputEvtMode, decoderOf(order, 1, 800, 600, 60000))
	_ = o.handle(outputEvtDone, decoderOf(order))
	if w, _ := o.ModeSize(); w != 800 {
		t.Fatalf("mode width = %d, want 800", w)
	}
	if announced != 0 {
		t.Errorf("a mode change announced %d scale changes, want none", announced)
	}
}
