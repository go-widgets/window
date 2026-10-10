// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package tab

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/webcanvas"
)

// recorder is a widget that records what it is given.
type recorder struct {
	bounds toolkit.Rect
	events []toolkit.Event
	drawn  int
}

func (r *recorder) SetBounds(b toolkit.Rect)                  { r.bounds = b }
func (r *recorder) Bounds() toolkit.Rect                      { return r.bounds }
func (r *recorder) Draw(p painter.Painter, th *toolkit.Theme) { r.drawn++ }
func (r *recorder) OnEvent(e toolkit.Event)                   { r.events = append(r.events, e) }
func (r *recorder) HitTest(x, y int) bool                     { return true }

// damaged is a recorder that repaints incrementally.
type damaged struct {
	recorder
	rendered int
}

func (d *damaged) RenderDamaged(p painter.Painter, th *toolkit.Theme) []toolkit.Rect {
	d.rendered++
	return nil
}

func TestATreeDrawsTheRootOverTheSurface(t *testing.T) {
	r := &recorder{}
	tr := NewTree(r, 40, 30, nil)
	if w, h := tr.Size(); w != 40 || h != 30 {
		t.Fatalf("Size %dx%d", w, h)
	}
	buf := make([]byte, 4*40*30)
	tr.Draw(buf)
	if r.drawn != 1 || r.bounds != (toolkit.Rect{W: 40, H: 30}) {
		t.Fatalf("drawn %d, bounds %+v", r.drawn, r.bounds)
	}
	bg := toolkit.DefaultDark().Background
	if buf[0] != bg.R || buf[1] != bg.G || buf[2] != bg.B {
		t.Fatalf("background % x, want the theme's", buf[:4])
	}
}

func TestAnIncrementalRootRepaintsItsDamage(t *testing.T) {
	d := &damaged{}
	tr := NewTree(d, 10, 10, toolkit.DefaultLight())
	tr.Draw(make([]byte, 400))
	if d.rendered != 1 || d.drawn != 0 {
		t.Fatalf("rendered %d, drawn %d", d.rendered, d.drawn)
	}
}

// Every input reaches the tree as the toolkit event it is, with the modifier
// keys the page reported just before.
func TestInputReachesTheTreeWithItsModifiers(t *testing.T) {
	r := &recorder{}
	tr := NewTree(r, 100, 100, nil)
	tr.Modifiers(webcanvas.Modifiers{Shift: true})
	tr.Click(1, 2)
	tr.Move(3, 4) // button held: a drag
	tr.Release(5, 6)
	tr.Move(7, 8) // released: a move
	tr.Context(9, 10)
	tr.Modifiers(webcanvas.Modifiers{Ctrl: true, Meta: true, Alt: true})
	tr.KeyDown("a")
	tr.Modifiers(webcanvas.Modifiers{})
	tr.Char("é")
	tr.Scroll(11, 12, 1, -3)
	want := []struct {
		kind toolkit.EventKind
		x, y int
	}{
		{toolkit.EventClick, 1, 2}, {toolkit.EventMouseDrag, 3, 4}, {toolkit.EventMouseUp, 5, 6},
		{toolkit.EventMouseMove, 7, 8}, {toolkit.EventSecondaryClick, 9, 10},
		{toolkit.EventKeyDown, 0, 0}, {toolkit.EventChar, 0, 0}, {toolkit.EventScroll, 11, 12},
	}
	if len(r.events) != len(want) {
		t.Fatalf("%d events, want %d: %+v", len(r.events), len(want), r.events)
	}
	for i, w := range want {
		if e := r.events[i]; e.Kind != w.kind || e.X != w.x || e.Y != w.y {
			t.Fatalf("event %d: %+v, want kind %v at %d,%d", i, e, w.kind, w.x, w.y)
		}
	}
	if !r.events[0].Shift || r.events[0].Ctrl {
		t.Fatalf("click modifiers %+v", r.events[0])
	}
	if k := r.events[5]; k.Code != "a" || !k.Ctrl || !k.Meta || !k.Alt || k.Shift {
		t.Fatalf("Ctrl+Cmd+Alt+A arrived as %+v", k)
	}
	if c := r.events[6]; c.Code != "é" || c.Ctrl || c.Shift {
		t.Fatalf("char %+v", c)
	}
	if s := r.events[7]; s.Delta != -3 || s.DeltaX != 1 {
		t.Fatalf("scroll %+v", s)
	}
}

func TestTheTreeFollowsTheCanvas(t *testing.T) {
	tr := NewTree(&recorder{}, 10, 10, nil)
	if w, h := tr.Resize(800, 600); w != 800 || h != 600 {
		t.Fatalf("Resize gave %dx%d", w, h)
	}
	if w, h := tr.Resize(0, 0); w != 800 || h != 600 {
		t.Fatalf("a zero size changed the surface to %dx%d", w, h)
	}
}

// Repaint before Run does nothing; after RepaintWith it asks once per call,
// from any goroutine (run under -race).
func TestRepaintAsksWebcanvas(t *testing.T) {
	tr := NewTree(toolkit.NewLabel("x"), 10, 10, nil)
	tr.Repaint() // nothing handed over yet: no panic, nothing asked
	var asked atomic.Int64
	tr.RepaintWith(func() { asked.Add(1) })
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(tr.Repaint)
	}
	wg.Wait()
	if asked.Load() != 8 {
		t.Fatalf("%d requests for 8 repaints", asked.Load())
	}
}
