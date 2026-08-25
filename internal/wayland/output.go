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
//
// A wl_output says more than its scale, though, and the rest is what a client
// enumerating the displays needs: geometry carries the output's position in the
// compositor's global space and the panel's own make and model, and mode
// carries the resolution it is running at. Both used to be read and dropped
// here. They are kept now, under the same done discipline as the scale.

// outputIfaceVersion is the highest wl_output this client understands. 2 is
// where the scale event appears, and 4 is where the output finally states its
// CONNECTOR ("DP-2") and a human description — the only identifiers a
// compositor can give for a panel that publishes no EDID, and every one of
// those otherwise comes back as the placeholder its driver invented. A
// compositor advertising less is bound at what it has.
const outputIfaceVersion = 4

// wl_output event opcodes.
const (
	outputEvtGeometry    = 0
	outputEvtMode        = 1
	outputEvtDone        = 2
	outputEvtScale       = 3
	outputEvtName        = 4
	outputEvtDescription = 5
)

// outputProps is everything a compositor says about one output between two
// done events. It is a value, and compared as one, so "did anything change"
// is a single comparison rather than a field-by-field audit that a new
// property would silently fall out of.
type outputProps struct {
	X, Y                      int32  // position in the compositor's global space, logical units
	PhysWidthMM, PhysHeightMM int32  // the panel's own size; 0 when it does not say
	Make, Model               string // what the panel calls itself, out of its EDID
	Connector                 string // wl_output v4: "DP-2", "HEADLESS-1"; stable across restarts
	Description               string // wl_output v4: a human sentence about the output
	Transform                 int32  // wl_output.transform: a rotated panel swaps its axes
	ModeWidth, ModeHeight     int32  // the current mode, in the output's own pixels
	Refresh                   int32  // mHz
	Scale                     int32  // device pixels per logical point
}

// Output is a wl_output: one screen, its scale, and where it sits.
type Output struct {
	conn *Conn
	id   uint32
	name uint32 // the registry name, which is what enter/leave events do NOT carry

	pending outputProps
	// applied is the last COMPLETE description: the compositor sends a burst of
	// properties and then done, and a client that reacted to each one in turn
	// would reallocate its framebuffer several times per screen change — and,
	// worse, would act on a burst that had not finished describing itself.
	applied outputProps

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
	if o.applied.Scale < 1 {
		return 1
	}
	return int(o.applied.Scale)
}

// Position is the output's top-left corner in the compositor's global space,
// in LOGICAL units — which is the space the other outputs' positions are in,
// and therefore the only one in which a desktop layout means anything.
func (o *Output) Position() (x, y int) {
	return int(o.applied.X), int(o.applied.Y)
}

// Model is the panel's own product name, as the compositor read it out of the
// display's EDID: "DELL U2720Q", "VITURE Beast". It is what a user recognises,
// and "" on an output that publishes none.
func (o *Output) Model() string { return o.applied.Model }

// Make is the panel's manufacturer, the other half of what the EDID says.
func (o *Output) Make() string { return o.applied.Make }

// Connector is the output's stable name — "DP-2", "eDP-1", "HEADLESS-1" —
// which is the socket rather than the panel, and is "" on a compositor older
// than wl_output 4. It is what to fall back to when a display publishes no
// model of its own, exactly as an X11 client falls back to the RANDR output
// name.
func (o *Output) Connector() string { return o.applied.Connector }

// Description is the compositor's own human sentence about the output, if it
// offers one. It is meant to be shown, not matched on: a compositor may change
// its wording, and it does not have to be unique.
func (o *Output) Description() string { return o.applied.Description }

// PhysicalSize is the panel's size in millimetres, 0x0 when it does not say.
func (o *Output) PhysicalSize() (widthMM, heightMM int) {
	return int(o.applied.PhysWidthMM), int(o.applied.PhysHeightMM)
}

// ModeSize is the current mode's resolution, in the output's OWN pixels —
// what the panel is actually driving, before the compositor's scale divides it
// into points.
func (o *Output) ModeSize() (w, h int) {
	return int(o.applied.ModeWidth), int(o.applied.ModeHeight)
}

// Refresh is the current mode's refresh rate in mHz (60000 for 60 Hz), 0 when
// the compositor has not sent a mode.
func (o *Output) Refresh() int { return int(o.applied.Refresh) }

// LogicalSize is the output's size in LOGICAL points: the mode divided by the
// scale, with the axes swapped when the panel is turned on its side.
//
// It is what belongs in a desktop layout, because it is the unit the positions
// are already in. Composing it here rather than at each caller is what keeps
// the rotation from being forgotten by one of them — a portrait monitor whose
// size is reported unswapped overlaps its neighbour, and nothing says so.
func (o *Output) LogicalSize() (w, h int) {
	w, h = int(o.applied.ModeWidth), int(o.applied.ModeHeight)
	if o.applied.Transform%2 == 1 {
		// The odd transforms are the quarter turns (90 and 270, flipped or
		// not); the even ones leave the axes where they were.
		w, h = h, w
	}
	s := o.Scale()
	return w / s, h / s
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
		o := &Output{conn: r.conn, id: id, name: g.Name,
			pending: outputProps{Scale: 1}, applied: outputProps{Scale: 1}}
		r.conn.register(id, o.handle)
		out = append(out, o)
	}
	return out, nil
}

// modeCurrent is the wl_output.mode flag marking the mode the output is
// actually running. A compositor lists every mode the panel supports and only
// one of them is the answer to "how big is this screen".
const modeCurrent = 0x1

func (o *Output) handle(opcode uint16, d *decoder) error {
	switch opcode {
	case outputEvtScale:
		if s := int32(d.getU32()); d.ok && s >= 1 {
			o.pending.Scale = s // pending: nothing acts on it until done
		}
	case outputEvtGeometry:
		x, y := d.getI32(), d.getI32()
		pw, ph := d.getI32(), d.getI32()
		_ = d.getI32() // subpixel layout: a font-rendering question, not ours
		mk, model := d.getString(), d.getString()
		transform := d.getI32()
		if !d.ok {
			// A truncated burst describes nothing; keeping half of it would
			// place the output somewhere the compositor never said.
			return nil
		}
		o.pending.X, o.pending.Y = x, y
		o.pending.PhysWidthMM, o.pending.PhysHeightMM = pw, ph
		o.pending.Make, o.pending.Model = mk, model
		o.pending.Transform = transform
	case outputEvtMode:
		flags := d.getU32()
		w, h := d.getI32(), d.getI32()
		refresh := d.getI32()
		if !d.ok || flags&modeCurrent == 0 {
			// Every supported mode is announced; only the current one says how
			// big the screen is right now.
			return nil
		}
		o.pending.ModeWidth, o.pending.ModeHeight, o.pending.Refresh = w, h, refresh
	case outputEvtName:
		if v := d.getString(); d.ok {
			o.pending.Connector = v
		}
	case outputEvtDescription:
		if v := d.getString(); d.ok {
			o.pending.Description = v
		}
	case outputEvtDone:
		if o.pending != o.applied {
			was := o.applied.Scale
			o.applied = o.pending
			if o.applied.Scale != was && o.OnScale != nil {
				o.OnScale(int(o.applied.Scale))
			}
		}
	}
	return nil
}
