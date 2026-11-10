// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package gpupath

import (
	"bytes"
	"errors"
	"sync"
	"testing"

	"github.com/go-gfx/gpu2d"
	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// cpuRenderer is a Renderer that draws with gpu2d's CPU reference, so the
// path's own work (recording through gpupainter, the frame copy, the state
// machine) is exercised on every machine, with or without a Vulkan device.
// Draw, Resize and Close can be made to fail.
type cpuRenderer struct {
	w, h              int
	pix               []byte
	drawErr, resizeEr error
	closed            int
	noDraw            bool // Draw succeeds and writes nothing
}

func newCPU(w, h int) *cpuRenderer {
	// Filled with 0xAA, as a device's memory may hold anything: Start clears it.
	r := &cpuRenderer{w: w, h: h, pix: bytes.Repeat([]byte{0xAA}, 4*w*h)}
	return r
}

func (r *cpuRenderer) Draw(l *gpu2d.List) error {
	if r.drawErr != nil {
		return r.drawErr
	}
	if r.noDraw {
		return nil
	}
	return l.Rasterize(r.pix)
}
func (r *cpuRenderer) Pixels() []byte { return r.pix }
func (r *cpuRenderer) Resize(w, h int) error {
	if r.resizeEr != nil {
		return r.resizeEr
	}
	r.w, r.h, r.pix = w, h, bytes.Repeat([]byte{0xAA}, 4*w*h)
	return nil
}
func (r *cpuRenderer) Close() { r.closed++ }

// use installs open as the Open seam for one test and returns the renderers it
// handed out.
func use(t *testing.T, open func(w, h int) (Renderer, error)) {
	t.Helper()
	old := Open
	Open = open
	t.Cleanup(func() { Open = old })
}

func tree() toolkit.Widget {
	v := toolkit.NewVBox()
	v.Append(toolkit.NewLabel("GPU path"))
	v.Append(toolkit.NewButton("A button", func() {}))
	v.Append(toolkit.NewEntry("an entry"))
	return v
}

// cpuFrame is what the CPU path paints for root: the background, then the tree,
// on a fresh framebuffer.
func cpuFrame(root toolkit.Widget, theme *toolkit.Theme, w, h int) []byte {
	buf := make([]byte, 4*w*h)
	p := painter.NewPixelPainter(buf, w, h)
	full := toolkit.Rect{W: w, H: h}
	p.FillRect(full, theme.Background)
	if root != nil {
		root.SetBounds(full)
		root.Draw(p, theme)
	}
	return buf
}

func TestAPathThatDidNotAskNeverStarts(t *testing.T) {
	use(t, func(int, int) (Renderer, error) { t.Error("Open called"); return nil, nil })
	var p Path
	p.Start(10, 10)
	p.Ask(false)
	p.Start(10, 10)
	if p.Active() || p.Err() != nil {
		t.Fatalf("Active %v, Err %v for a window that did not ask", p.Active(), p.Err())
	}
	dst := make([]byte, 4*10*10)
	if p.Render(dst, 10, 10, nil, toolkit.DefaultDark()) {
		t.Fatal("Render drew on a path that never started")
	}
	p.Resize(20, 20) // nothing to resize
	if p.TakeDropped() {
		t.Fatal("a path that never started reports a drop")
	}
	p.Close()
	p.Close()
}

// The frame the path draws is the CPU path's, byte for byte, before and after a
// resize; the renderer's memory is cleared first, and Close releases it once.
func TestTheGPUFrameIsTheCPUFrame(t *testing.T) {
	var made []*cpuRenderer
	use(t, func(w, h int) (Renderer, error) {
		r := newCPU(w, h)
		made = append(made, r)
		return r, nil
	})
	theme := toolkit.DefaultDark()
	var p Path
	p.Ask(true)
	p.Start(120, 80)
	p.Start(120, 80) // already up: no second renderer
	if !p.Active() || p.Err() != nil || len(made) != 1 {
		t.Fatalf("Active %v, Err %v, %d renderers", p.Active(), p.Err(), len(made))
	}
	for _, b := range made[0].pix {
		if b != 0 {
			t.Fatal("Start did not clear the renderer's memory")
		}
	}
	dst := make([]byte, 4*120*80)
	if !p.Render(dst, 120, 80, tree(), theme) {
		t.Fatalf("Render refused: %v", p.Err())
	}
	if !bytes.Equal(dst, cpuFrame(tree(), theme, 120, 80)) {
		t.Fatal("the GPU frame differs from the CPU frame")
	}

	// A second frame composites over the first, as PixelPainter does over its
	// buffer, and is the same frame.
	if !p.Render(dst, 120, 80, tree(), theme) || !bytes.Equal(dst, cpuFrame(tree(), theme, 120, 80)) {
		t.Fatal("the second frame differs")
	}

	p.Resize(200, 150)
	if !p.Active() {
		t.Fatalf("the resize stopped the path: %v", p.Err())
	}
	for _, b := range made[0].pix {
		if b != 0 {
			t.Fatal("Resize did not clear the renderer's memory")
		}
	}
	dst = make([]byte, 4*200*150)
	if !p.Render(dst, 200, 150, tree(), theme) || !bytes.Equal(dst, cpuFrame(tree(), theme, 200, 150)) {
		t.Fatal("the frame after the resize differs")
	}
	if p.TakeDropped() {
		t.Fatal("a healthy path reports a drop")
	}

	p.Close()
	p.Close()
	if made[0].closed != 1 {
		t.Fatalf("the renderer was closed %d times, want 1", made[0].closed)
	}
	if p.Active() {
		t.Fatal("Active after Close")
	}
	p.Start(120, 80)
	if p.Active() || len(made) != 1 {
		t.Fatal("a closed path started again")
	}
}

// A root of nil still draws the background.
func TestARootOfNilDrawsTheBackground(t *testing.T) {
	use(t, func(w, h int) (Renderer, error) { return newCPU(w, h), nil })
	theme := toolkit.DefaultDark()
	var p Path
	p.Ask(true)
	p.Start(16, 16)
	dst := make([]byte, 4*16*16)
	if !p.Render(dst, 16, 16, nil, theme) || !bytes.Equal(dst, cpuFrame(nil, theme, 16, 16)) {
		t.Fatal("the background alone differs")
	}
}

func TestStartRecordsWhyItFailed(t *testing.T) {
	refused := errors.New("no device")
	use(t, func(int, int) (Renderer, error) { return nil, refused })
	var p Path
	p.Ask(true)
	p.Start(10, 10)
	if p.Active() || !errors.Is(p.Err(), refused) {
		t.Fatalf("Active %v, Err %v", p.Active(), p.Err())
	}
	if p.TakeDropped() {
		t.Fatal("a path that never started reports a drop")
	}
	if p.Render(make([]byte, 400), 10, 10, nil, toolkit.DefaultDark()) {
		t.Fatal("Render drew without a renderer")
	}
}

// A surface the list cannot hold releases the renderer it just opened.
func TestStartWithABadSizeClosesTheRenderer(t *testing.T) {
	var r *cpuRenderer
	use(t, func(w, h int) (Renderer, error) { r = newCPU(1, 1); return r, nil })
	var p Path
	p.Ask(true)
	p.Start(-1, 10)
	if p.Active() || !errors.Is(p.Err(), gpu2d.ErrSize) {
		t.Fatalf("Active %v, Err %v", p.Active(), p.Err())
	}
	if r.closed != 1 {
		t.Fatalf("the renderer was closed %d times, want 1", r.closed)
	}
}

// Every way a frame can be refused stops the path for good, records why, and
// tells the caller to draw on the CPU; the first frame after it is a whole one.
func TestARefusedFrameStopsThePath(t *testing.T) {
	huge := &hugeImage{}
	for _, c := range []struct {
		name string
		prep func(r *cpuRenderer)
		root toolkit.Widget
		want error
	}{
		{"device lost", func(r *cpuRenderer) { r.drawErr = errors.New("device lost") }, tree(), nil},
		{"too large for the device", func(r *cpuRenderer) { r.drawErr = gpu2d.ErrTooLarge }, tree(), gpu2d.ErrTooLarge},
		{"the list refused a primitive", func(*cpuRenderer) {}, huge, gpu2d.ErrTooLarge},
		{"the frame is not the window's size", func(r *cpuRenderer) { r.noDraw, r.pix = true, r.pix[:4] }, tree(), ErrSize},
	} {
		t.Run(c.name, func(t *testing.T) {
			var r *cpuRenderer
			use(t, func(w, h int) (Renderer, error) { r = newCPU(w, h); return r, nil })
			var p Path
			p.Ask(true)
			p.Start(40, 30)
			c.prep(r)
			dst := make([]byte, 4*40*30)
			if p.Render(dst, 40, 30, c.root, toolkit.DefaultDark()) {
				t.Fatal("a refused frame reported success")
			}
			if p.Active() || p.Err() == nil {
				t.Fatalf("Active %v, Err %v", p.Active(), p.Err())
			}
			if c.want != nil && !errors.Is(p.Err(), c.want) {
				t.Fatalf("Err = %v, want %v", p.Err(), c.want)
			}
			if r.closed != 1 {
				t.Fatalf("the renderer was closed %d times, want 1", r.closed)
			}
			if !p.TakeDropped() || p.TakeDropped() {
				t.Fatal("TakeDropped must say true once, then false")
			}
			if p.Render(dst, 40, 30, c.root, toolkit.DefaultDark()) {
				t.Fatal("a stopped path drew")
			}
		})
	}
}

// hugeImage draws an image no list can hold.
type hugeImage struct{ toolkit.Base }

func (hugeImage) Draw(p painter.Painter, _ *toolkit.Theme) {
	p.(interface {
		DrawImage(painter.Rect, []byte, int, int)
	}).DrawImage(painter.Rect{W: 4, H: 4}, nil, 1<<16, 1<<16)
}
func (hugeImage) OnEvent(toolkit.Event) {}

func TestAResizeTheDeviceRefusesStopsThePath(t *testing.T) {
	for _, c := range []struct {
		name string
		prep func(r *cpuRenderer)
		w, h int
		want error
	}{
		{"the device cannot hold it", func(r *cpuRenderer) { r.resizeEr = gpu2d.ErrTooLarge }, 80, 60, gpu2d.ErrTooLarge},
		{"the list cannot hold it", func(*cpuRenderer) {}, -1, 60, gpu2d.ErrSize},
	} {
		t.Run(c.name, func(t *testing.T) {
			var r *cpuRenderer
			use(t, func(w, h int) (Renderer, error) { r = newCPU(w, h); return r, nil })
			var p Path
			p.Ask(true)
			p.Start(40, 30)
			c.prep(r)
			p.Resize(c.w, c.h)
			if p.Active() || !errors.Is(p.Err(), c.want) {
				t.Fatalf("Active %v, Err %v", p.Active(), p.Err())
			}
			if r.closed != 1 || !p.TakeDropped() {
				t.Fatalf("closed %d, dropped false", r.closed)
			}
		})
	}
}

// Close may come from any goroutine while the run loop draws.
func TestCloseDuringFramesIsSafe(t *testing.T) {
	use(t, func(w, h int) (Renderer, error) { return newCPU(w, h), nil })
	var p Path
	p.Ask(true)
	p.Start(64, 48)
	theme := toolkit.DefaultDark()
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		dst := make([]byte, 4*64*48)
		for range 50 {
			p.Render(dst, 64, 48, tree(), theme)
			p.Resize(64, 48)
		}
	}()
	go func() { defer wg.Done(); p.Close() }()
	wg.Wait()
	if p.Active() {
		t.Fatal("Active after Close")
	}
}
