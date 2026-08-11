// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin && integration

package cocoa

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"os/exec"
	"testing"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
)

// vibrancyRoot is a minimal childContainer root: it lays out a left Material
// sidebar and a right content column, and exposes both to CollectMaterials.
type vibrancyRoot struct {
	toolkit.Base
	sidebar *toolkit.Material
	content toolkit.Widget
	sideW   int
}

func (v *vibrancyRoot) SetBounds(b toolkit.Rect) {
	v.Base.SetBounds(b)
	v.sidebar.SetBounds(toolkit.Rect{X: b.X, Y: b.Y, W: v.sideW, H: b.H})
	v.content.SetBounds(toolkit.Rect{X: b.X + v.sideW, Y: b.Y, W: b.W - v.sideW, H: b.H})
}
func (v *vibrancyRoot) Draw(p painter.Painter, th *toolkit.Theme) {
	v.sidebar.Draw(p, th)
	v.content.Draw(p, th)
}
func (v *vibrancyRoot) Children() []toolkit.Widget {
	return []toolkit.Widget{v.sidebar, v.content}
}

// TestLiveCocoaVibrancy is the on-device proof of the NSVisualEffectView backing
// for toolkit.Material: it opens a real window whose left third is a Sidebar
// material, verifies the backend installed a native effect view and marked the
// material native-backed, punches the framebuffer hole, and captures the live
// window (screencapture, which composites the real desktop blur) plus the
// offscreen container render. It is gated behind WINDOW_COCOA_INTEGRATION.
func TestLiveCocoaVibrancy(t *testing.T) {
	if os.Getenv("WINDOW_COCOA_INTEGRATION") == "" {
		t.Skip("set WINDOW_COCOA_INTEGRATION=1 to run the live macOS vibrancy proof")
	}

	theme := toolkit.DefaultDark()
	var (
		win        *Window
		root       *vibrancyRoot
		setupErr   error
		renderPNG  []byte
		effectN    int
		nativeFlag bool
	)

	callOnMain(func() {
		win, setupErr = New("go-widgets/window vibrancy proof", 480, 320, theme)
		if setupErr != nil {
			return
		}
		side := toolkit.NewMaterial(toolkit.MaterialSidebar)
		side.Blend = toolkit.BlendBehindWindow
		col := toolkit.NewVBox()
		col.Append(toolkit.NewLabel("Content area"))
		col.Append(toolkit.NewButton("Click me", func() {}))
		root = &vibrancyRoot{sidebar: side, content: col, sideW: 160}

		win.bindAndSeed(root)
		spin(0.6) // let the window map + composite the vibrancy
		win.presentFull()
		spin(0.3)

		effectN = len(win.effectViews)
		nativeFlag = side.NativeBacked()

		// Offscreen composite of the whole content view (effect views + the
		// framebuffer view drawn on top). Permission-free.
		renderPNG, setupErr = renderViewPNG(win.container)

		// Live on-screen capture of this window, which DOES include the real
		// desktop blur the effect view produces. Best-effort (Screen Recording).
		wn := int(win.win.Send(selWindowNumber))
		_ = exec.Command("screencapture", "-x", "-o", "-l", fmt.Sprintf("%d", wn),
			"cocoa-vibrancy-screencapture-2026-08-11.png").Run()
	})

	if setupErr != nil {
		t.Fatalf("vibrancy setup/capture failed: %v", setupErr)
	}
	if !win.translucent {
		t.Fatal("window did not switch to translucent mode for a Material")
	}
	if effectN != 1 {
		t.Fatalf("effect views installed = %d, want 1", effectN)
	}
	if !nativeFlag {
		t.Fatal("sidebar material was not marked native-backed")
	}

	img, err := png.Decode(bytes.NewReader(renderPNG))
	if err != nil {
		t.Fatalf("decode container render: %v", err)
	}
	writeArtifact(t, "cocoa-vibrancy-render-2026-08-11.png", renderPNG)
	b := img.Bounds()
	if b.Dx() < 100 || b.Dy() < 100 {
		t.Fatalf("render too small: %v", b)
	}
	if allUniform(img) {
		t.Fatal("container render is a single flat colour — nothing composited")
	}

	// The sidebar region (left, over the effect view) must read differently from
	// the content region (right, opaque theme background), proving the effect
	// view composited a distinct material where the framebuffer hole was punched.
	sampleSide := avgRegion(img, b.Min.X+b.Dx()/12, b.Min.Y+b.Dy()/2, 6)
	sampleMain := avgRegion(img, b.Min.X+b.Dx()*3/4, b.Min.Y+b.Dy()/2, 6)
	if colourNear(sampleSide, sampleMain, 12) {
		t.Fatalf("sidebar sample %v not distinct from content sample %v — vibrancy region did not composite",
			sampleSide, sampleMain)
	}
	t.Logf("vibrancy proof: translucent=%v effectViews=%d native=%v sidebar=%v content=%v",
		win.translucent, effectN, nativeFlag, sampleSide, sampleMain)

	callOnMain(func() { _ = win.Close() })
}

// _ keeps painter imported even if Draw is trimmed during edits.
var _ = painter.RGBA{}

// avgRegion averages a (2r+1)² pixel block centred on (cx,cy).
func avgRegion(img image.Image, cx, cy, r int) [3]int {
	var sum [3]int
	var n int
	b := img.Bounds()
	for y := cy - r; y <= cy+r; y++ {
		for x := cx - r; x <= cx+r; x++ {
			if x < b.Min.X || x >= b.Max.X || y < b.Min.Y || y >= b.Max.Y {
				continue
			}
			rr, gg, bb, _ := img.At(x, y).RGBA()
			sum[0] += int(rr >> 8)
			sum[1] += int(gg >> 8)
			sum[2] += int(bb >> 8)
			n++
		}
	}
	if n == 0 {
		return [3]int{}
	}
	return [3]int{sum[0] / n, sum[1] / n, sum[2] / n}
}

// colourNear reports whether two averaged colours are within tol on each channel.
func colourNear(a, b [3]int, tol int) bool {
	for i := 0; i < 3; i++ {
		d := a[i] - b[i]
		if d < 0 {
			d = -d
		}
		if d > tol {
			return false
		}
	}
	return true
}
