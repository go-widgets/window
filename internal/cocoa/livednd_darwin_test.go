// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin && integration

// This is the on-device drag-and-drop proof for the shared DnD state machine.
// It opens a REAL NSWindow holding a DragSource widget above a toolkit.DropZone,
// then SYNTHESISES a real pointer drag as genuine NSEvents — a left mouse-down
// on the source, a sequence of left mouse-DRAGGED events travelling down onto
// the DropZone, and a left mouse-up over it — posted through NSApp so they flow
// through AppKit's real dispatch into the view's -mouseDown:/-mouseDragged:/
// -mouseUp: callbacks, the sovereign mapping.go decode, and the SAME
// internal/dnd.Controller every backend now shares.
//
// It asserts the full lifecycle the raw drag SHOULD synthesise: the DropZone
// lights up (Hover) mid-drag, an EventDrop reaches the target on release, the
// DropZone's OnDrop fires with the source's exact payload, and the recording
// root observed EventDragStart + EventDrop carrying that payload. The hover
// frame is captured on-device as a dated PNG artifact.
package cocoa

import (
	"os"
	"testing"

	objc "github.com/go-macos/objc"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// NSEventTypeLeftMouseDragged — the drag NSEvent the source drag travels as
// (Down=1/Up=2 are already declared in cocoa_darwin.go).
const evtLeftMouseDragged = 6

// dragSourceWidget is a minimal DragSource: a filled rectangle that hands out a
// fixed payload when a drag begins on it.
type dragSourceWidget struct {
	toolkit.Base
	payload string
}

func (d *dragSourceWidget) DragData() string { return d.payload }
func (d *dragSourceWidget) Draw(p painter.Painter, th *toolkit.Theme) {
	p.FillRect(d.Bounds(), th.Accent)
}

// dndRoot is a container root that records every toolkit.Event delivered while
// forwarding it to its inner tree, AND exposes Children so the DnD controller
// can descend it to hit-test the source and target.
type dndRoot struct {
	toolkit.Base
	inner  toolkit.Widget
	events []toolkit.Event
}

func (r *dndRoot) SetBounds(b toolkit.Rect) { r.Base.SetBounds(b); r.inner.SetBounds(b) }
func (r *dndRoot) Draw(p painter.Painter, th *toolkit.Theme) {
	p.FillRect(r.Bounds(), th.Background)
	r.inner.Draw(p, th)
}
func (r *dndRoot) OnEvent(ev toolkit.Event)   { r.events = append(r.events, ev); r.inner.OnEvent(ev) }
func (r *dndRoot) Children() []toolkit.Widget { return []toolkit.Widget{r.inner} }
func (r *dndRoot) dropped(code string) bool {
	for _, e := range r.events {
		if e.Kind == toolkit.EventDrop && e.Code == code {
			return true
		}
	}
	return false
}
func (r *dndRoot) sawKind(k toolkit.EventKind) bool {
	for _, e := range r.events {
		if e.Kind == k {
			return true
		}
	}
	return false
}

// centreBasePoint returns the window/base-space point (bottom-left origin, in
// points) at the centre of a widget whose Bounds are in render pixels.
func (w *Window) centreBasePoint(r toolkit.Rect) nsPoint {
	cx := float64(r.X+r.W/2) / w.scale
	cyTop := float64(r.Y+r.H/2) / w.scale
	viewHpts := float64(w.h) / w.scale
	return nsPoint{X: cx, Y: viewHpts - cyTop}
}

var (
	selMouseDownMsg    = objc.RegisterName("mouseDown:")
	selMouseDraggedMsg = objc.RegisterName("mouseDragged:")
	selMouseUpMsg      = objc.RegisterName("mouseUp:")
)

// sendMouseDirect builds a REAL left-mouse NSEvent at loc (window/base coords)
// and invokes the content view's own -mouseDown:/-mouseDragged:/-mouseUp:
// handler with it. That is the exact IMP AppKit calls when the window server
// routes a real click, so this drives the whole on-device path — the real
// NSEvent decoded by mapping.go's viewCoords/locationInWindow, w.dispatch, and
// the shared internal/dnd.Controller — deterministically. It bypasses only the
// window-server ROUTING layer (postEvent:+nextEventMatchingMask:/-sendEvent:),
// which a non-frontmost, windowNumber-unregistered test process never drains,
// so the proof does not hinge on the test being the active GUI app.
func (w *Window) sendMouseDirect(evtType int, loc nsPoint) {
	wn := int(w.win.Send(selWindowNumber))
	ev := objc.ID(objc.GetClass("NSEvent")).Send(selMouseEventFactory,
		uint(evtType), loc, uint(0), 0.0, wn, objc.ID(0), 0, 1, 1.0)
	var sel objc.SEL
	switch evtType {
	case evtLeftMouseDown:
		sel = selMouseDownMsg
	case evtLeftMouseDragged:
		sel = selMouseDraggedMsg
	case evtLeftMouseUp:
		sel = selMouseUpMsg
	}
	w.view.Send(sel, ev)
}

func TestLiveCocoaDragDrop(t *testing.T) {
	if os.Getenv("WINDOW_COCOA_INTEGRATION") == "" {
		t.Skip("set WINDOW_COCOA_INTEGRATION=1 to run the on-device DnD proof")
	}

	theme := toolkit.DefaultDark()
	const payload = "file:/private/tmp/go-widgets-dnd/photo.png"

	var (
		win      *Window
		root     *dndRoot
		src      *dragSourceWidget
		zone     *toolkit.DropZone
		gotPaths []string
		hoverMid bool
		hoverPNG []byte
		srcRect  toolkit.Rect
		zoneRect toolkit.Rect
		setupErr error
	)

	callOnMain(func() {
		win, setupErr = New("go-widgets/window DnD proof", 420, 360, theme)
		if setupErr != nil {
			return
		}
		src = &dragSourceWidget{payload: payload}
		zone = toolkit.NewDropZone("Drop the file here")
		zone.OnDrop = func(paths []string) { gotPaths = paths }
		vbox := toolkit.NewVBox()
		vbox.Append(src)  // top half
		vbox.Append(zone) // bottom half
		root = &dndRoot{inner: vbox}

		win.bindAndSeed(root)
		spin(0.4)
		win.presentFull()
		srcRect, zoneRect = src.Bounds(), zone.Bounds()

		from := win.centreBasePoint(srcRect)
		to := win.centreBasePoint(zoneRect)

		// Press on the source, then travel down onto the DropZone in several real
		// mouse-DRAGGED steps (crossing the drag threshold well before the zone).
		win.sendMouseDirect(evtLeftMouseDown, from)
		steps := 6
		for i := 1; i <= steps; i++ {
			f := float64(i) / float64(steps)
			p := nsPoint{X: from.X + (to.X-from.X)*f, Y: from.Y + (to.Y-from.Y)*f}
			win.sendMouseDirect(evtLeftMouseDragged, p)
		}
		// Mid-drag, the pointer is over the DropZone: it must be lit, and this is
		// the frame we capture on-device.
		hoverMid = zone.Hover().Get()
		win.presentFull()
		hoverPNG, _ = renderViewPNG(win.view)

		// Release over the DropZone: the drop fires.
		win.sendMouseDirect(evtLeftMouseUp, to)
	})

	if setupErr != nil {
		t.Fatalf("window setup failed: %v", setupErr)
	}

	// The DropZone lit up while the drag hovered it (feedback lifecycle live).
	if !hoverMid {
		t.Fatalf("DropZone.Hover was false mid-drag — no EventDragStart/Move reached the target")
	}
	// The drop delivered the source's exact payload to the target's OnDrop.
	if len(gotPaths) != 1 || gotPaths[0] != payload {
		t.Fatalf("DropZone.OnDrop paths = %v, want [%q]", gotPaths, payload)
	}
	// The root observed the synthesised lifecycle, not just the raw mouse stream.
	if !root.sawKind(toolkit.EventDragStart) {
		t.Fatalf("root never saw EventDragStart; events=%+v", root.events)
	}
	if !root.dropped(payload) {
		t.Fatalf("root never saw EventDrop carrying %q; events=%+v", payload, root.events)
	}
	// Hover must clear after the drop (lifecycle completed cleanly).
	if zone.Hover().Get() {
		t.Fatalf("DropZone.Hover still set after drop")
	}

	if len(hoverPNG) > 0 {
		writeArtifact(t, "cocoa-dnd-drop-2026-08-11.png", hoverPNG)
	}
	t.Logf("on-device drag→drop OK: payload %q delivered to DropZone under release; src=%v zone=%v",
		payload, srcRect, zoneRect)

	callOnMain(func() { _ = win.Close() })
}
