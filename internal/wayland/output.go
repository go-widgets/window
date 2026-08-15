// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package wayland

// wl_output, which is how a client learns that the screen it is on has more
// pixels than points.
//
// A compositor on a HiDPI panel advertises the output with a SCALE — 2 on the
// usual laptop — and by default treats a client's buffer as being in logical
// units, stretching it. That is why an application that never asks looks soft
// on exactly the machines whose screens are best: the compositor is upscaling
// a buffer drawn at half the resolution of the panel.
//
// The remedy is a client-side one: allocate the buffer at scale times the
// logical size and tell the compositor with wl_surface.set_buffer_scale, after
// which the pixels are the panel's own.

const outputIfaceVersion = 2 // 2 is where the scale event appears

// wl_output event opcodes.
const (
	outputEvtGeometry = 0
	outputEvtMode     = 1
	outputEvtDone     = 2
	outputEvtScale    = 3
)

// Output is a wl_output: one screen, and the thing that knows its scale.
type Output struct {
	conn *Conn
	id   uint32
	name uint32 // the registry name, which is what enter/leave events do NOT carry

	scale int32 // pending, until done
	// Scale is the last COMPLETE scale: the compositor sends a burst of
	// properties and then done, and a client that reacted to each one in turn
	// would reallocate its framebuffer several times per screen change.
	applied int32

	// OnScale fires when done publishes a scale different from the last one.
	OnScale func(scale int)
}

// ID returns the output's object id, which is what wl_surface.enter names.
func (o *Output) ID() uint32 { return o.id }

// Name returns the registry name the output was bound from.
func (o *Output) Name() uint32 { return o.name }

// Scale is the output's scale factor: how many device pixels the compositor
// puts in one logical point. 1 until the compositor says otherwise, because a
// compositor that never sends the event is describing a 1:1 screen.
func (o *Output) Scale() int {
	if o.applied < 1 {
		return 1
	}
	return int(o.applied)
}

// Outputs binds every wl_output the compositor advertises.
//
// Every one, not the first: a laptop plugged into an external screen has two,
// with different scales, and which one a window is on is a question only
// wl_surface.enter can answer.
func (r *Registry) Outputs() ([]*Output, error) {
	var out []*Output
	for _, g := range r.Globals() {
		if g.Interface != "wl_output" {
			continue
		}
		id, err := r.bind(g.Name, "wl_output", min32(g.Version, outputIfaceVersion))
		if err != nil {
			return out, err
		}
		o := &Output{conn: r.conn, id: id, name: g.Name, scale: 1, applied: 1}
		r.conn.register(id, o.handle)
		out = append(out, o)
	}
	return out, nil
}

func (o *Output) handle(opcode uint16, d *decoder) error {
	switch opcode {
	case outputEvtScale:
		if s := int32(d.getU32()); d.ok && s >= 1 {
			o.scale = s // pending: nothing acts on it until done
		}
	case outputEvtDone:
		if o.scale != o.applied {
			o.applied = o.scale
			if o.OnScale != nil {
				o.OnScale(int(o.applied))
			}
		}
	case outputEvtGeometry, outputEvtMode:
		// Physical size, position, model, refresh rate: real information, and
		// none of it changes how many pixels go in the buffer. Read and dropped
		// rather than left to desynchronise the decoder.
	}
	return nil
}
