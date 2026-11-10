// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"bytes"
	"errors"
	"testing"

	"github.com/go-gfx/gpu2d"
	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/window/internal/gpupath"
)

// refRenderer is a gpupath.Renderer that draws with gpu2d's CPU reference, so
// the windows' use of the GPU path (what is drawn, when the CPU takes over, what
// is presented) is proven on every machine. It fails the draw numbered failAt.
type refRenderer struct {
	pix    []byte
	draws  int
	failAt int // the draw (1-based) that fails; 0 never
	failEr error
	// resizeEr makes Resize refuse.
	resizeEr error
}

func (r *refRenderer) Draw(l *gpu2d.List) error {
	r.draws++
	if r.failAt != 0 && r.draws == r.failAt {
		return r.failEr
	}
	return l.Rasterize(r.pix)
}
func (r *refRenderer) Pixels() []byte { return r.pix }
func (r *refRenderer) Resize(w, h int) error {
	if r.resizeEr != nil {
		return r.resizeEr
	}
	r.pix = make([]byte, 4*w*h)
	return nil
}
func (r *refRenderer) Close() {}

// useRef makes the GPU path start on a refRenderer, and returns it.
func useRef(t *testing.T, failAt int, failEr error) *refRenderer {
	t.Helper()
	r := &refRenderer{failAt: failAt, failEr: failEr}
	old := gpupath.Open
	gpupath.Open = func(w, h int) (gpupath.Renderer, error) {
		r.pix = make([]byte, 4*w*h)
		return r, nil
	}
	t.Cleanup(func() { gpupath.Open = old })
	return r
}

// useRefusing makes the GPU path fail to start with err.
func useRefusing(t *testing.T, err error) {
	t.Helper()
	old := gpupath.Open
	gpupath.Open = func(int, int) (gpupath.Renderer, error) { return nil, err }
	t.Cleanup(func() { gpupath.Open = old })
}

// sceneRoot is a root that draws something the CPU path can be compared on, and
// counts how it was asked to.
type sceneRoot struct {
	toolkit.Base
	drawn, damaged int // Draw calls, RenderDamaged calls
}

func (s *sceneRoot) OnEvent(toolkit.Event) {}
func (s *sceneRoot) Draw(p painter.Painter, _ *toolkit.Theme) {
	s.drawn++
	b := s.Bounds()
	p.FillRoundRect(painter.Rect{X: b.X + 3, Y: b.Y + 3, W: b.W / 2, H: b.H / 2}, 6, painter.RGBA{R: 200, G: 40, B: 90, A: 255})
	p.FillRect(painter.Rect{X: b.X + b.W/2, Y: b.Y + b.H/3, W: b.W / 3, H: b.H / 3}, painter.RGBA{G: 180, B: 60, A: 128})
}

// RenderDamaged makes sceneRoot an incremental root: it repaints everything and
// reports the whole surface.
func (s *sceneRoot) RenderDamaged(p painter.Painter, th *toolkit.Theme) []toolkit.Rect {
	s.damaged++
	s.Draw(p, th)
	b := s.Bounds()
	return []toolkit.Rect{{X: 0, Y: 0, W: b.W, H: b.H}}
}

// sceneFrame is the CPU path's frame of a sceneRoot at w × h.
func sceneFrame(theme *toolkit.Theme, w, h int) []byte {
	buf := make([]byte, 4*w*h)
	p := painter.NewPixelPainter(buf, w, h)
	full := toolkit.Rect{W: w, H: h}
	p.FillRect(full, theme.Background)
	root := &sceneRoot{}
	root.SetBounds(full)
	root.Draw(p, theme)
	return buf
}

func TestWindowOnTheGPUDrawsTheCPUFrameAndFollowsResizes(t *testing.T) {
	useRef(t, 0, nil)
	w, _ := dialFake(t, Config{Width: 100, Height: 80, GPU: true},
		configureNotify(160, 120), exposeEvent(), deleteMessage())
	if w.GPU() || w.GPUError() != nil {
		t.Fatalf("GPU %v, GPUError %v before Run", w.GPU(), w.GPUError())
	}
	root := &sceneRoot{}
	if err := w.Run(root); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !w.GPU() || w.GPUError() != nil {
		t.Fatalf("GPU %v, GPUError %v after Run", w.GPU(), w.GPUError())
	}
	if gw, gh := w.Size(); gw != 160 || gh != 120 {
		t.Fatalf("size %d × %d after the resize", gw, gh)
	}
	if !bytes.Equal(w.buf, sceneFrame(w.theme, 160, 120)) {
		t.Fatal("the framebuffer the GPU path left is not the CPU frame")
	}
	if root.damaged != 0 {
		t.Fatalf("an incremental root was repainted by damage %d times while the GPU drew", root.damaged)
	}
	_ = w.Close()
	if w.GPU() {
		t.Fatal("GPU after Close")
	}
}

// A window that did not ask never opens a renderer.
func TestWindowThatDidNotAskForTheGPUNeverOpensOne(t *testing.T) {
	old := gpupath.Open
	gpupath.Open = func(int, int) (gpupath.Renderer, error) { t.Error("Open called"); return nil, errors.New("no") }
	t.Cleanup(func() { gpupath.Open = old })
	w, _ := dialFake(t, Config{Width: 40, Height: 30}, deleteMessage())
	if err := w.Run(&sceneRoot{}); err != nil {
		t.Fatal(err)
	}
	if w.GPU() || w.GPUError() != nil {
		t.Fatalf("GPU %v, GPUError %v", w.GPU(), w.GPUError())
	}
}

// Where the GPU will not start the window draws on the CPU and says why.
func TestWindowWhoseGPUWillNotStartDrawsOnTheCPU(t *testing.T) {
	refused := errors.New("no device")
	old := gpupath.Open
	gpupath.Open = func(int, int) (gpupath.Renderer, error) { return nil, refused }
	t.Cleanup(func() { gpupath.Open = old })
	w, _ := dialFake(t, Config{Width: 90, Height: 70, GPU: true}, deleteMessage())
	root := &sceneRoot{}
	if err := w.Run(root); err != nil {
		t.Fatal(err)
	}
	if w.GPU() || !errors.Is(w.GPUError(), refused) {
		t.Fatalf("GPU %v, GPUError %v", w.GPU(), w.GPUError())
	}
	// An incremental root paints its own ground (RenderDamaged is not given a
	// background fill), so the oracle is the root alone on a fresh framebuffer.
	want := make([]byte, 4*90*70)
	ref := &sceneRoot{}
	ref.SetBounds(toolkit.Rect{W: 90, H: 70})
	ref.Draw(painter.NewPixelPainter(want, 90, 70), w.theme)
	if !bytes.Equal(w.buf, want) {
		t.Fatal("the CPU frame differs from PixelPainter's")
	}
	if root.damaged != 1 {
		t.Fatalf("the first frame of an incremental root must go through RenderDamaged once, got %d", root.damaged)
	}
}

// A frame the device refuses is drawn on the CPU, the window keeps running, and
// the error is exposed: ErrTooLarge, a lost device, whatever the renderer says.
// The first frame after it is a whole one, since the framebuffer was not kept
// current while the GPU drew; after that the incremental root is incremental.
func TestWindowFallsBackToTheCPUWhenAFrameIsRefused(t *testing.T) {
	for _, refusal := range []error{gpu2d.ErrTooLarge, gpupath.ErrSize, errors.New("device lost")} {
		useRef(t, 2, refusal) // the second frame is refused
		w, _ := dialFake(t, Config{Width: 100, Height: 80, GPU: true})
		root := &sceneRoot{}
		w.root, w.dmg = root, root
		w.gpu.Start(w.w, w.h)
		if err := w.paintFrame(false, false); err != nil { // the GPU draws
			t.Fatal(err)
		}
		if !w.GPU() {
			t.Fatalf("not on the GPU: %v", w.GPUError())
		}
		root.drawn = 0
		if err := w.paintFrame(false, false); err != nil { // refused: the CPU draws it
			t.Fatal(err)
		}
		if w.GPU() || !errors.Is(w.GPUError(), refusal) {
			t.Fatalf("GPU %v, GPUError %v, want the renderer's %v", w.GPU(), w.GPUError(), refusal)
		}
		// Draw ran twice: once recorded for the GPU that refused, once on the CPU.
		if root.drawn != 2 || root.damaged != 0 {
			t.Fatalf("the refused frame: Draw %d, RenderDamaged %d, want 2 and 0 (a whole CPU frame)", root.drawn, root.damaged)
		}
		if !bytes.Equal(w.buf, sceneFrame(w.theme, 100, 80)) {
			t.Fatal("the refused frame is not the CPU frame")
		}
		// The next frame: still a whole one (the flag the drop left), then
		// incremental again.
		root.drawn, root.damaged = 0, 0
		_ = w.paintFrame(false, false)
		if root.drawn != 1 || root.damaged != 0 {
			t.Fatalf("the frame after the drop: Draw %d, RenderDamaged %d, want one whole frame", root.drawn, root.damaged)
		}
		root.drawn, root.damaged = 0, 0
		_ = w.paintFrame(false, false)
		if root.damaged != 1 {
			t.Fatalf("incremental root not incremental again: RenderDamaged %d", root.damaged)
		}
		_ = w.Close()
	}
}

// A resize the device cannot hold drops the window to the CPU in the same
// breath: the frame that follows is a whole CPU frame at the new size.
func TestWindowFallsBackToTheCPUWhenAResizeIsRefused(t *testing.T) {
	r := useRef(t, 0, nil)
	w, _ := dialFake(t, Config{Width: 100, Height: 80, GPU: true})
	root := &sceneRoot{}
	w.root, w.dmg = root, root
	w.gpu.Start(w.w, w.h)
	if err := w.paintFrame(false, false); err != nil {
		t.Fatal(err)
	}
	r.resizeEr = gpu2d.ErrTooLarge
	w.resize(120, 90)
	if w.GPU() || !errors.Is(w.GPUError(), gpu2d.ErrTooLarge) {
		t.Fatalf("GPU %v, GPUError %v after a resize the device refused", w.GPU(), w.GPUError())
	}
	root.drawn, root.damaged = 0, 0
	if err := w.paintFrame(true, false); err != nil {
		t.Fatal(err)
	}
	if root.drawn != 1 || root.damaged != 0 {
		t.Fatalf("the frame after the drop: Draw %d, RenderDamaged %d, want one whole frame", root.drawn, root.damaged)
	}
	if !bytes.Equal(w.buf, sceneFrame(w.theme, 120, 90)) {
		t.Fatal("the frame after the resize is not the CPU frame")
	}
}
