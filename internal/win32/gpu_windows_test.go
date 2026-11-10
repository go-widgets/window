// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package win32

import (
	"bytes"
	"errors"
	"os"
	"runtime"
	"testing"
	"time"

	"github.com/go-gfx/gpu2d/d3d11"
	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// The live GPU proof (Config.GPU on Win32): a real window, created and run on
// this thread, drawn by gpu2d's Direct3D 11 renderer on WARP (the software
// device: windows-latest has no GPU) and presented through a flip-model swap
// chain on the window's own HWND. The proof is the back buffer, read through
// staging just before the swap chain presents it, against the frame the CPU path
// draws from the same widgets. What a screen shows is not observed: a hosted
// runner has no one to look at it.
//
//	WINDOW_WIN32_GPU=1  run these tests (the Windows CI lane sets it)

func skipUnlessGPU(t *testing.T) {
	t.Helper()
	if os.Getenv("WINDOW_WIN32_GPU") == "" {
		t.Skip("set WINDOW_WIN32_GPU=1 to run the live Win32 GPU proof")
	}
}

// warp asks gpuPath for the software device and restores the seams afterwards.
func warp(t *testing.T) {
	t.Helper()
	oo, oc, of := gpuOpener, gpuCapture, gpuFault
	gpuOpener = func(w, h int) (*d3d11.Renderer, error) {
		return d3d11.NewWith(w, h, d3d11.Options{Software: true})
	}
	t.Cleanup(func() { gpuOpener, gpuCapture, gpuFault = oo, oc, of })
}

// gpuProofRoot paints translucent fills, rounded rectangles, strokes and text, so
// the comparison covers blending, coverage and glyph masks, not a flat colour.
type gpuProofRoot struct {
	toolkit.Base
	onCPU int // frames painted by a CPU painter
	drawn int
}

func (r *gpuProofRoot) Draw(p painter.Painter, _ *toolkit.Theme) {
	r.drawn++
	if _, ok := p.(*painter.PixelPainter); ok {
		r.onCPU++
	}
	b := r.Bounds()
	p.FillRect(painter.Rect{X: 4, Y: 4, W: b.W - 8, H: b.H - 8}, painter.RGBA{R: 30, G: 70, B: 120, A: 255})
	p.FillRoundRect(painter.Rect{X: 12, Y: 12, W: b.W - 24, H: b.H / 2}, 14, painter.RGBA{R: 220, G: 60, B: 50, A: 140})
	p.FillRoundRect(painter.Rect{X: 30, Y: b.H / 3, W: b.W / 2, H: b.H / 3}, 9, painter.RGBA{R: 20, G: 200, B: 90, A: 110})
	p.StrokeRoundRect(painter.Rect{X: 8, Y: 8, W: b.W - 16, H: b.H - 16}, 12, painter.RGBA{R: 255, G: 255, B: 255, A: 200}, 1)
	p.StrokeRect(painter.Rect{X: 20, Y: 20, W: 60, H: 30}, painter.RGBA{R: 255, G: 220, B: 0, A: 255}, 1)
	p.Text(24, 28, "go-widgets gpu", painter.RGBA{R: 255, G: 255, B: 255, A: 255})
	p.PutPixel(3, 3, painter.RGBA{R: 255, A: 255})
}

func (r *gpuProofRoot) OnEvent(toolkit.Event) {}

// cpuFrame is the frame the CPU path draws from root at w × h.
func cpuFrame(root toolkit.Widget, theme *toolkit.Theme, w, h int) []byte {
	buf := make([]byte, 4*w*h)
	p := painter.NewPixelPainter(buf, w, h)
	full := toolkit.Rect{X: 0, Y: 0, W: w, H: h}
	p.FillRect(full, theme.Background)
	root.SetBounds(full)
	root.Draw(p, theme)
	return buf
}

// watchdog closes the window after d, so a message that never arrives fails the
// test instead of hanging it.
func watchdog(w *Window, d time.Duration) (stop func()) {
	t := time.AfterFunc(d, func() { w.Close() })
	return func() { t.Stop() }
}

func TestLiveGPUWindowPresentsTheCPUFrame(t *testing.T) {
	skipUnlessGPU(t)
	warp(t)
	runtime.LockOSThread() // the window belongs to this thread, as Open arranges
	defer runtime.UnlockOSThread()

	w, err := NewScaledGPU("go-widgets gpu proof", 320, 200, nil, false, true)
	if err != nil {
		t.Fatalf("NewScaledGPU: %v", err)
	}
	root := &gpuProofRoot{}
	type seen struct{ w, h int }
	var sizes []seen
	var gpuWhenCaptured []bool
	gpuCapture = func(frame []byte, fw, fh int) {
		sizes = append(sizes, seen{fw, fh})
		gpuWhenCaptured = append(gpuWhenCaptured, w.GPU())
		want := cpuFrame(root, w.theme, fw, fh)
		if !bytes.Equal(frame, want) {
			bad := 0
			for i := range want {
				if want[i] != frame[i] {
					bad++
				}
			}
			t.Errorf("frame %d (%d x %d): the back buffer is not the CPU path's frame: %d of %d bytes differ", len(sizes), fw, fh, bad, len(want))
		}
		switch len(sizes) {
		case 1:
			// Resize the window: WM_SIZE reaches onSize on this thread, which resizes
			// the renderer and the swap chain and presents a frame at the new size.
			procSetWindowPos.Call(w.hwnd, 0, 0, 0, 520, 420, uintptr(swpNoZOrder|swpNoActivate|0x0002 /* SWP_NOMOVE */))
		default:
			w.Close()
		}
	}
	defer watchdog(w, 40*time.Second)()

	if err := w.Run(root); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(sizes) < 2 {
		t.Fatalf("%d presented frames, want the first and one after the resize (GPUError %v)", len(sizes), w.GPUError())
	}
	if sizes[0] == sizes[1] {
		t.Errorf("the window was not resized: both frames are %d x %d", sizes[0].w, sizes[0].h)
	}
	for i, g := range gpuWhenCaptured {
		if !g {
			t.Errorf("frame %d was not drawn by the GPU path", i+1)
		}
	}
	if root.onCPU != len(sizes) { // only the reference frames are painted on the CPU
		t.Errorf("%d CPU-painted frames, want exactly the %d reference frames: the window drew on the CPU too", root.onCPU, len(sizes))
	}
	t.Logf("%d frames presented through the swap chain on %s, sizes %v, each equal to the CPU path's", len(sizes), "WARP", sizes)
}

// A device that cannot start leaves the CPU path in place, with the reason.
func TestLiveGPUWindowFallsBackWhenTheDeviceWillNotStart(t *testing.T) {
	skipUnlessGPU(t)
	warp(t)
	boom := errors.New("no device in this test")
	gpuOpener = func(int, int) (*d3d11.Renderer, error) { return nil, boom }
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	w, err := NewScaledGPU("go-widgets gpu fallback", 200, 120, nil, false, true)
	if err != nil {
		t.Fatal(err)
	}
	root := &closeAfterFirstDraw{w: w}
	defer watchdog(w, 30*time.Second)()
	if err := w.Run(root); err != nil {
		t.Fatal(err)
	}
	if w.GPU() || !errors.Is(w.GPUError(), boom) {
		t.Fatalf("GPU %v, GPUError %v: want the CPU path and the reason", w.GPU(), w.GPUError())
	}
	if root.onCPU == 0 {
		t.Fatal("the window drew no frame on the CPU")
	}
}

// closeAfterFirstDraw closes its window once it has been drawn, and counts the
// frames a CPU painter drew.
type closeAfterFirstDraw struct {
	toolkit.Base
	w     *Window
	onCPU int
}

func (r *closeAfterFirstDraw) Draw(p painter.Painter, _ *toolkit.Theme) {
	if _, ok := p.(*painter.PixelPainter); ok {
		r.onCPU++
	}
	p.FillRect(r.Bounds(), painter.RGBA{R: 10, G: 20, B: 30, A: 255})
	r.w.Close()
}

func (r *closeAfterFirstDraw) OnEvent(toolkit.Event) {}

// A frame the device refuses stops the GPU path for good: that frame and every
// later one is drawn on the CPU, and the swap chain is released so GDI shows.
func TestLiveGPUWindowDropsToTheCPUWhenAFrameIsRefused(t *testing.T) {
	skipUnlessGPU(t)
	warp(t)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	w, err := NewScaledGPU("go-widgets gpu fault", 200, 120, nil, false, true)
	if err != nil {
		t.Fatal(err)
	}
	root := &gpuProofRoot{}
	lost := errors.New("device removed (injected)")
	presented := 0
	gpuCapture = func([]byte, int, int) {
		presented++
		gpuFault = func() error { return lost } // the next frame is refused
		w.Repaint()
	}
	frames := 0
	defer watchdog(w, 30*time.Second)()
	root2 := &faultRoot{gpuProofRoot: root, onFrame: func(onCPU bool) {
		frames++
		if onCPU {
			w.Close() // the first frame on the CPU after the fault: done
		}
	}}
	if err := w.Run(root2); err != nil {
		t.Fatal(err)
	}
	if presented != 1 {
		t.Fatalf("%d frames presented on the GPU before the fault, want 1", presented)
	}
	if w.GPU() || !errors.Is(w.GPUError(), lost) {
		t.Fatalf("GPU %v, GPUError %v: want the CPU path and the injected error", w.GPU(), w.GPUError())
	}
	if frames < 3 { // the GPU frame, the refused one (recorded, then redrawn on the CPU), and the CPU frame
		t.Logf("frames drawn: %d", frames)
	}
	if root.onCPU == 0 {
		t.Fatal("the refused frame was not drawn on the CPU")
	}
}

// faultRoot reports each frame and which painter drew it.
type faultRoot struct {
	*gpuProofRoot
	onFrame func(onCPU bool)
}

func (r *faultRoot) Draw(p painter.Painter, th *toolkit.Theme) {
	r.gpuProofRoot.Draw(p, th)
	_, cpu := p.(*painter.PixelPainter)
	r.onFrame(cpu)
}
