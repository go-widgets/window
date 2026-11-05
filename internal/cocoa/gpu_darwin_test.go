// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin && integration

package cocoa

import (
	"bytes"
	"errors"
	"syscall"
	"testing"
	"time"

	gpumetal "github.com/go-gfx/gpu2d/metal"
	mtl "github.com/go-macos/metal"
	objc "github.com/go-macos/objc"
	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

func gpuTree() toolkit.Widget {
	vbox := toolkit.NewVBox()
	vbox.Append(toolkit.NewLabel("GPU path: the pixels of the CPU path"))
	vbox.Append(toolkit.NewButton("A button", func() {}))
	vbox.Append(toolkit.NewEntry("an entry"))
	vbox.Append(toolkit.NewLabel("Ag — accents àéîõü"))
	return vbox
}

// cpuFrame is what the CPU path paints for root at w × h: the background,
// then the tree, on a fresh framebuffer.
func cpuFrame(root toolkit.Widget, theme *toolkit.Theme, w, h int) []byte {
	buf := make([]byte, 4*w*h)
	p := painter.NewPixelPainter(buf, w, h)
	full := toolkit.Rect{W: w, H: h}
	p.FillRect(full, theme.Background)
	root.SetBounds(full)
	root.Draw(p, theme)
	return buf
}

func openGPU(t *testing.T, w, h int) *Window {
	t.Helper()
	var (
		win *Window
		err error
	)
	callOnMain(func() {
		win, err = NewWithOptions(Options{Title: "gpu path", Width: w, Height: h, Theme: toolkit.DefaultDark(), GPU: true})
	})
	if err != nil {
		t.Fatalf("NewWithOptions(GPU): %v", err)
	}
	t.Cleanup(func() { callOnMain(func() { win.stopGPU(); _ = win.Close() }) })
	return win
}

// waitFrames lets the GPU finish what is in flight, so Pixels can be read.
func waitFrames(t *testing.T, win *Window) {
	t.Helper()
	for win.gpu.r.InFlight() > 0 {
		if err := win.gpu.r.Wait(); err != nil {
			t.Fatal(err)
		}
	}
}

func equalFrames(t *testing.T, what string, got, want []byte, w int) {
	t.Helper()
	if bytes.Equal(got, want) {
		return
	}
	if len(got) != len(want) {
		t.Fatalf("%s: %d bytes on the GPU, %d on the CPU", what, len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			p := i / 4
			t.Fatalf("%s: first difference at (%d,%d) channel %d: CPU %d, GPU %d", what, p%w, p/w, i%4, want[i], got[i])
		}
	}
}

// A window asked for the GPU draws there, its layer is the view layer's
// bottom sublayer at the framebuffer's size, and the frame it drew is the CPU
// path's frame byte for byte. (What reaches the drawable is the frame byte
// for byte too: go-gfx/gpu2d's TestAPresentedFrameIsTheFrame.)
// The names start with TestLiveCocoaWindow so the CI step that runs the live
// window proofs (-run 'TestLiveCocoaWindow|...') runs these too.
func TestLiveCocoaWindowOnTheGPUDrawsTheCPUFrame(t *testing.T) {
	win := openGPU(t, 320, 200)
	theme := win.theme
	var setupErr error
	callOnMain(func() {
		win.bindAndSeed(gpuTree())
		if !win.GPU() {
			setupErr = win.GPUError()
			if errors.Is(setupErr, mtl.ErrNoDevice) {
				return
			}
			if setupErr == nil {
				setupErr = errors.New("the window did not start the GPU path and says no reason")
			}
		}
	})
	if errors.Is(setupErr, mtl.ErrNoDevice) {
		t.Skip(setupErr)
	}
	if setupErr != nil {
		t.Fatal(setupErr)
	}
	callOnMain(func() {
		host := win.view.Send(objc.Sel("layer"))
		bottom := host.Send(objc.Sel("sublayers")).Send(objc.Sel("objectAtIndex:"), 0)
		if uintptr(bottom) != win.gpu.layer.ID() {
			t.Error("the Metal layer is not the view layer's bottom sublayer")
		}
		if lw, lh := win.gpu.layer.Size(); lw != win.w || lh != win.h {
			t.Errorf("drawables are %d × %d, the framebuffer %d × %d", lw, lh, win.w, win.h)
		}
		waitFrames(t, win)
	})
	equalFrames(t, "first frame", win.gpu.r.Pixels(), cpuFrame(gpuTree(), theme, win.w, win.h), win.w)

	// A resize follows: new drawables, new frame, still the CPU's pixels.
	callOnMain(func() {
		win.resize(400, 260, win.scale)
		waitFrames(t, win)
	})
	if !win.GPU() {
		t.Fatalf("the resize dropped the GPU path: %v", win.GPUError())
	}
	if lw, lh := win.gpu.layer.Size(); lw != 400 || lh != 260 {
		t.Fatalf("after the resize drawables are %d × %d, want 400 × 260", lw, lh)
	}
	// The renderer composites over its previous frame, as PixelPainter does
	// over its buffer; the theme background is opaque, so the result is the
	// fresh CPU frame.
	equalFrames(t, "after resize", win.gpu.r.Pixels(), cpuFrame(gpuTree(), theme, 400, 260), 400)

	// Stopping takes the layer out of the view; the CPU path draws again.
	callOnMain(func() {
		layer := objc.ID(win.gpu.layer.ID())
		layer.Send(objc.Sel("retain"))
		defer layer.Send(objc.Sel("release"))
		win.stopGPU()
		if layer.Send(objc.Sel("superlayer")) != 0 {
			t.Error("the stopped GPU path left its layer in the view")
		}
		win.draw()
	})
	equalFrames(t, "CPU path after stopping", win.buf, cpuFrame(gpuTree(), theme, 400, 260), 400)
}

// When Metal will not start the window says why and draws on the CPU.
func TestLiveCocoaWindowWhoseGPUFailsDrawsOnTheCPU(t *testing.T) {
	refused := errors.New("no GPU for this test")
	gpuOpener = func(int, int) (*gpumetal.Renderer, error) { return nil, refused }
	t.Cleanup(func() { gpuOpener = gpumetal.New })
	win := openGPU(t, 200, 120)
	callOnMain(func() { win.bindAndSeed(gpuTree()) })
	if win.GPU() {
		t.Fatal("the GPU path started with a refusing opener")
	}
	if !errors.Is(win.GPUError(), refused) {
		t.Fatalf("GPUError = %v, want the opener's refusal", win.GPUError())
	}
	equalFrames(t, "CPU fallback", win.buf, cpuFrame(gpuTree(), win.theme, win.w, win.h), win.w)
}

// A translucent window keeps the CPU path: its effect views show through
// holes punched in the CPU framebuffer.
func TestLiveCocoaWindowTranslucentStaysOnTheCPU(t *testing.T) {
	win := openGPU(t, 300, 160)
	side := toolkit.NewMaterial(toolkit.MaterialSidebar)
	side.Blend = toolkit.BlendBehindWindow
	col := toolkit.NewVBox()
	col.Append(toolkit.NewLabel("Content area"))
	callOnMain(func() { win.bindAndSeed(&vibrancyRoot{sidebar: side, content: col, sideW: 100}) })
	if !win.translucent {
		t.Skip("this machine did not make the window translucent")
	}
	if win.GPU() {
		t.Fatal("a translucent window took the GPU path")
	}
	if win.GPUError() != nil {
		t.Fatalf("staying on the CPU for a material is not an error: %v", win.GPUError())
	}
}

// cpuTime is the process's user + system time so far.
func cpuTime() time.Duration {
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

// BenchmarkLiveCocoaWindowFrame paints and presents one whole frame of a
// live window, on the CPU path (PixelPainter, then the NSBitmapImageRep blit
// in -drawRect:) and on the GPU path (gpupainter, then Present). Besides wall
// time it reports cpu-ns/op, the process's CPU time per frame: the cost the
// GPU path exists to take off the processor.
func BenchmarkLiveCocoaWindowFrame(b *testing.B) {
	for _, size := range []struct {
		name string
		w, h int
	}{{"960x640", 960, 640}, {"1920x1200", 1920, 1200}} {
		for _, gpu := range []bool{false, true} {
			name := size.name + "/cpu"
			if gpu {
				name = size.name + "/gpu"
			}
			b.Run(name, func(b *testing.B) {
				var win *Window
				var err error
				callOnMain(func() {
					win, err = NewWithOptions(Options{Title: "frame", Width: size.w, Height: size.h, Theme: toolkit.DefaultDark(), GPU: gpu})
					if err == nil {
						win.bindAndSeed(gpuTree())
					}
				})
				if err != nil {
					b.Fatal(err)
				}
				defer callOnMain(func() { win.stopGPU(); _ = win.Close() })
				if gpu && !win.GPU() {
					b.Skip(win.GPUError())
				}
				b.ReportAllocs()
				start := cpuTime()
				b.ResetTimer()
				for b.Loop() {
					callOnMain(func() { win.paintFrame(false) })
				}
				b.StopTimer()
				b.ReportMetric(float64(cpuTime()-start)/float64(b.N), "cpu-ns/op")
			})
		}
	}
}

// Without the option nothing changes.
func TestLiveCocoaWindowThatDidNotAskStaysOnTheCPU(t *testing.T) {
	var (
		win *Window
		err error
	)
	callOnMain(func() {
		win, err = NewWithOptions(Options{Title: "cpu", Width: 120, Height: 80, Theme: toolkit.DefaultDark()})
	})
	if err != nil {
		t.Fatal(err)
	}
	defer callOnMain(func() { _ = win.Close() })
	callOnMain(func() { win.bindAndSeed(gpuTree()) })
	if win.GPU() || win.GPUError() != nil {
		t.Fatalf("GPU %v, GPUError %v without the option", win.GPU(), win.GPUError())
	}
}
