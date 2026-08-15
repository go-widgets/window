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
