// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build integration && linux

// This is the live proof of Config.GPU on X11 and Wayland: a real window whose
// frames gpu2d's Vulkan back-end drew shows the bytes the CPU path shows for the
// same scene.
//
//   - X11: the window's pixels are read back from the server (ImageMagick's
//     `import` is XGetImage) and compared byte for byte with the same scene drawn
//     by a window that did not ask for the GPU, and with PixelPainter's own frame.
//   - Wayland: the wl_shm buffer the window last committed is compared with the
//     CPU window's, and with PixelPainter's frame packed as ARGB8888; where the
//     compositor is ours, grim's capture of its output is compared too.
//
// It needs a Vulkan device (lavapipe is one; test/gpu-linux/Dockerfile is the
// recipe) and, with WINDOW_GPU_SPAWN=1, starts the servers itself: Xvfb and a
// headless sway. Without a device it skips, unless WINDOW_GPU_REQUIRE=1, which
// makes a missing device a failure so a CI lane cannot pass by proving nothing.
//
//	WINDOW_GPU_INTEGRATION=1 [WINDOW_GPU_SPAWN=1] [WINDOW_GPU_REQUIRE=1] \
//	  go test -tags=integration -run TestLiveGPU -v .
package window

import (
	"bytes"
	"errors"
	"fmt"
	"image/png"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/window/internal/gpupath"
	"github.com/go-widgets/window/internal/wayland"
)

func gpuLiveEnabled(t testing.TB) {
	t.Helper()
	if os.Getenv("WINDOW_GPU_INTEGRATION") == "" {
		t.Skip("set WINDOW_GPU_INTEGRATION=1 (and WINDOW_GPU_SPAWN=1 to start Xvfb and sway) to enable")
	}
}

func gpuSpawn() bool { return os.Getenv("WINDOW_GPU_SPAWN") != "" }

// gpuScene is what every window shows: text, controls and rounded shapes, so
// that coverage, blending and glyph masks are all in the comparison.
func gpuScene() toolkit.Widget {
	v := toolkit.NewVBox()
	v.Append(toolkit.NewLabel("GPU path: the pixels of the CPU path"))
	v.Append(toolkit.NewButton("A button", func() {}))
	v.Append(toolkit.NewEntry("an entry"))
	v.Append(toolkit.NewLabel("Ag - accents aeiou, 0123456789"))
	return v
}

// gpuCPUFrame is what PixelPainter paints for the scene at w × h on a fresh
// framebuffer: the oracle both back-ends are compared with.
func gpuCPUFrame(theme *toolkit.Theme, w, h int) []byte {
	buf := make([]byte, 4*w*h)
	p := painter.NewPixelPainter(buf, w, h)
	full := toolkit.Rect{W: w, H: h}
	p.FillRect(full, theme.Background)
	root := gpuScene()
	root.SetBounds(full)
	root.Draw(p, theme)
	return buf
}

// gpuReport says where two frames differ first, and how many bytes differ.
func gpuReport(what string, got, want []byte, w int) string {
	if len(got) != len(want) {
		return fmt.Sprintf("%s: %d bytes, want %d", what, len(got), len(want))
	}
	n, first := 0, -1
	for i := range want {
		if got[i] != want[i] {
			if first < 0 {
				first = i
			}
			n++
		}
	}
	p := first / 4
	return fmt.Sprintf("%s: %d of %d bytes differ, first at (%d,%d) channel %d: got %d, want %d",
		what, n, len(want), p%w, p/w, first%4, got[first], want[first])
}

// gpuNeed is the Vulkan-or-skip decision: a window that asked for the GPU and
// did not get it says why, and the lane decides whether that is a skip.
func gpuNeed(t testing.TB, b Backend) {
	t.Helper()
	g, ok := b.(interface {
		GPU() bool
		GPUError() error
	})
	if !ok {
		t.Fatalf("%T has no GPU and GPUError", b)
	}
	deadline := time.Now().Add(10 * time.Second)
	for !g.GPU() && g.GPUError() == nil && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if g.GPU() {
		return
	}
	err := g.GPUError()
	if os.Getenv("WINDOW_GPU_REQUIRE") != "" {
		t.Fatalf("WINDOW_GPU_REQUIRE is set and the window is not on the GPU: %v", err)
	}
	t.Skipf("no Vulkan device here: %v", err)
}

// --- servers ----------------------------------------------------------------

// gpuProc starts a server the test owns and kills it, by PID, when done.
func gpuProc(t testing.TB, env []string, name string, args ...string) {
	t.Helper()
	cmd := exec.Command(name, args...)
	cmd.Env = append(os.Environ(), env...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	t.Cleanup(func() {
		_ = syscall.Kill(cmd.Process.Pid, syscall.SIGTERM)
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
		if t.Failed() {
			t.Logf("%s output:\n%s", name, out.String())
		}
	})
}

// gpuWaitSocket waits for a unix socket to accept connections.
func gpuWaitSocket(t testing.TB, path string) {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := net.Dial("unix", path); err == nil {
			c.Close()
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("%s never accepted a connection", path)
}

// gpuXvfb starts Xvfb (or uses $DISPLAY when the lane brought its own) and
// returns the DISPLAY to use. The screen is larger than any window measured.
func gpuXvfb(t testing.TB) string {
	t.Helper()
	if !gpuSpawn() {
		d := os.Getenv("DISPLAY")
		if d == "" {
			t.Fatal("DISPLAY is not set (and WINDOW_GPU_SPAWN is not)")
		}
		return d
	}
	for n := 90; n < 120; n++ {
		sock := fmt.Sprintf("/tmp/.X11-unix/X%d", n)
		if _, err := os.Stat(sock); err == nil {
			continue
		}
		disp := fmt.Sprintf(":%d", n)
		gpuProc(t, nil, "Xvfb", disp, "-screen", "0", "2048x1400x24", "-nolisten", "tcp")
		gpuWaitSocket(t, sock)
		return disp
	}
	t.Fatal("no free display number")
	return ""
}

// gpuSway starts a headless sway whose output is w × h (the toplevel fills it)
// and returns the socket name; XDG_RUNTIME_DIR is set for the test. With no
// WINDOW_GPU_SPAWN it uses the session it is in.
func gpuSway(t testing.TB, w, h int) string {
	t.Helper()
	if !gpuSpawn() {
		name := os.Getenv("WAYLAND_DISPLAY")
		if name == "" {
			t.Fatal("WAYLAND_DISPLAY is not set (and WINDOW_GPU_SPAWN is not)")
		}
		return name
	}
	dir, err := os.MkdirTemp("", "gpu-sway-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	conf := filepath.Join(dir, "sway.conf")
	body := fmt.Sprintf("output HEADLESS-1 resolution %dx%d\ndefault_border none\ndefault_floating_border none\ngaps inner 0\ngaps outer 0\n", w, h)
	if err := os.WriteFile(conf, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("XDG_RUNTIME_DIR", dir)
	gpuProc(t, []string{
		"XDG_RUNTIME_DIR=" + dir, "WLR_BACKENDS=headless", "WLR_RENDERER=pixman",
		"WLR_LIBINPUT_NO_DEVICES=1", "LIBSEAT_BACKEND=noop", "WLR_HEADLESS_OUTPUTS=1",
	}, "sway", "-c", conf)
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		socks, _ := filepath.Glob(filepath.Join(dir, "wayland-*"))
		for _, s := range socks {
			if filepath.Ext(s) == ".lock" {
				continue
			}
			gpuWaitSocket(t, s)
			t.Setenv("WAYLAND_DISPLAY", filepath.Base(s))
			return filepath.Base(s)
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("sway's Wayland socket never appeared")
	return ""
}

// --- X11 --------------------------------------------------------------------

// gpuX11Window opens a window on the display, running its loop, and closes it
// when the test ends.
func gpuX11Window(t testing.TB, disp string, gpu bool, w, h int) *Window {
	t.Helper()
	t.Setenv("DISPLAY", disp)
	b, err := openX11(Config{Title: fmt.Sprintf("gpulive-%d-%v", os.Getpid(), gpu), Width: w, Height: h, GPU: gpu})
	if err != nil {
		t.Fatalf("openX11: %v", err)
	}
	win := b.(*Window)
	done := make(chan error, 1)
	go func() { done <- win.Run(gpuScene()) }()
	t.Cleanup(func() {
		_ = win.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Log("the run loop did not exit after Close")
		}
	})
	if gpu && !gpuRefused {
		gpuNeed(t, win)
		t.Logf("Vulkan device: %s", win.gpu.Device())
	}
	return win
}

// gpuRefused is set by the test that makes the GPU path fail on purpose, so
// that gpuNeed does not turn the refusal it wants into a failure.
var gpuRefused bool

// gpuX11Read reads the window back from the server as RGB bytes.
func gpuX11Read(t testing.TB, win *Window) []byte {
	t.Helper()
	out := filepath.Join(t.TempDir(), "read.rgb")
	cmd := exec.Command("import", "-window", fmt.Sprintf("0x%x", win.win), "-depth", "8", "rgb:"+out)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("import: %v\n%s", err, b)
	}
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// gpuRGB drops the alpha channel of an RGBA frame.
func gpuRGB(rgba []byte) []byte {
	out := make([]byte, 0, len(rgba)/4*3)
	for i := 0; i < len(rgba); i += 4 {
		out = append(out, rgba[i], rgba[i+1], rgba[i+2])
	}
	return out
}

// gpuSettle waits for the window to have presented its frame at w × h.
func gpuSettle(t testing.TB, size func() (int, int), w, h int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if gw, gh := size(); gw == w && gh == h {
			time.Sleep(700 * time.Millisecond) // the frame after the resize
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	gw, gh := size()
	t.Fatalf("the window is %d × %d, want %d × %d", gw, gh, w, h)
}

// A window on the GPU shows, in the X server, the bytes of the window that did
// not ask for the GPU, before and after a resize, under MIT-SHM when the server
// has it.
func TestLiveGPUX11ShowsTheCPUFrame(t *testing.T) {
	gpuLiveEnabled(t)
	requireTool(t, "import")
	requireTool(t, "xdotool")
	disp := gpuXvfb(t)
	theme := toolkit.DefaultDark()
	const W, H, W2, H2 = 320, 200, 400, 260

	read := func(gpu bool) (a, b []byte, shm bool) {
		win := gpuX11Window(t, disp, gpu, W, H)
		time.Sleep(700 * time.Millisecond)
		a = gpuX11Read(t, win)
		shm = win.seg != nil
		mustRun(t, "xdotool", "windowsize", fmt.Sprint(win.win), fmt.Sprint(W2), fmt.Sprint(H2))
		gpuSettle(t, win.Size, W2, H2)
		b = gpuX11Read(t, win)
		if gpu && !win.GPU() {
			t.Fatalf("the resize dropped the GPU path: %v", win.GPUError())
		}
		return a, b, shm
	}
	cpuA, cpuB, shm := read(false)
	gpuA, gpuB, _ := read(true)
	t.Logf("MIT-SHM present: %v", shm)

	wantA, wantB := gpuRGB(gpuCPUFrame(theme, W, H)), gpuRGB(gpuCPUFrame(theme, W2, H2))
	for _, c := range []struct {
		name      string
		got, want []byte
		w         int
	}{
		{"CPU window read back vs PixelPainter (the method is sound)", cpuA, wantA, W},
		{"GPU window vs CPU window, first frame", gpuA, cpuA, W},
		{"GPU window vs PixelPainter, first frame", gpuA, wantA, W},
		{"CPU window read back vs PixelPainter after the resize", cpuB, wantB, W2},
		{"GPU window vs CPU window after the resize", gpuB, cpuB, W2},
		{"GPU window vs PixelPainter after the resize", gpuB, wantB, W2},
	} {
		if !bytes.Equal(c.got, c.want) {
			t.Error(gpuReport(c.name, c.got, c.want, c.w))
		} else {
			t.Logf("equal: %s (%d bytes)", c.name, len(c.got))
		}
	}
}

// A machine where the GPU will not start draws the same frame on the CPU, and
// the window says why.
func TestLiveGPUX11WithoutVulkanShowsTheCPUFrame(t *testing.T) {
	gpuLiveEnabled(t)
	requireTool(t, "import")
	disp := gpuXvfb(t)
	refused := errors.New("no Vulkan in this test")
	old := gpupath.Open
	gpupath.Open = func(int, int) (gpupath.Renderer, error) { return nil, refused }
	gpuRefused = true
	t.Cleanup(func() { gpupath.Open, gpuRefused = old, false })

	win := gpuX11Window(t, disp, true, 320, 200)
	time.Sleep(700 * time.Millisecond)
	if win.GPU() || !errors.Is(win.GPUError(), refused) {
		t.Fatalf("GPU %v, GPUError %v: want the refusal", win.GPU(), win.GPUError())
	}
	got, want := gpuX11Read(t, win), gpuRGB(gpuCPUFrame(toolkit.DefaultDark(), 320, 200))
	if !bytes.Equal(got, want) {
		t.Fatal(gpuReport("the fallback frame vs PixelPainter", got, want, 320))
	}
}

// --- Wayland ----------------------------------------------------------------

// gpuWaylandWindow opens a toplevel on the compositor, running its loop.
func gpuWaylandWindow(t testing.TB, name string, gpu bool, w, h int) *wlWindow {
	t.Helper()
	b, err := openWayland(Config{Title: fmt.Sprintf("gpulive-%d-%v", os.Getpid(), gpu), Width: w, Height: h, GPU: gpu}, name)
	if err != nil {
		t.Fatalf("openWayland: %v", err)
	}
	win := b.(*wlWindow)
	done := make(chan error, 1)
	go func() { done <- win.Run(gpuScene()) }()
	t.Cleanup(func() {
		_ = win.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Log("the run loop did not exit after Close")
		}
	})
	if gpu && !gpuRefused {
		gpuNeed(t, win)
		t.Logf("Vulkan device: %s", win.gpu.Device())
	}
	return win
}

// gpuWaylandIdle opens a toplevel and drives it from the calling goroutine, with
// no run loop: the measurement paints frames itself, and a second goroutine
// dispatching events into the same connection would race it. Each step does
// what Run does between events (dispatch, acknowledge, resize); frame is one
// paint followed by a round trip, which delivers the buffer releases and frame
// callbacks Run would have dispatched.
func gpuWaylandIdle(t testing.TB, name string, gpu bool, w, h int) (win *wlWindow, frame func()) {
	t.Helper()
	b, err := openWayland(Config{Title: fmt.Sprintf("gpubench-%d-%v", os.Getpid(), gpu), Width: w, Height: h, GPU: gpu}, name)
	if err != nil {
		t.Fatalf("openWayland: %v", err)
	}
	win = b.(*wlWindow)
	t.Cleanup(func() { _ = win.Close() })
	win.root = gpuScene()
	win.gpu.Start(win.w, win.h)
	if gpu {
		gpuNeed(t, win)
	}
	step := func() {
		if err := win.conn.Roundtrip(); err != nil {
			t.Fatalf("round trip: %v", err)
		}
		if err := win.flushAck(); err != nil {
			t.Fatal(err)
		}
		if win.needResize {
			win.applyResize()
		}
		if err := win.paintFrame(); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < 50; i++ {
		if step(); win.w == w && win.h == h {
			break
		}
	}
	if win.w != w || win.h != h {
		t.Fatalf("the window is %d × %d, want %d × %d", win.w, win.h, w, h)
	}
	return win, func() {
		if err := win.paintFrame(); err != nil {
			t.Fatal(err)
		}
		if err := win.conn.Roundtrip(); err != nil {
			t.Fatal(err)
		}
	}
}

// gpuShm is the wl_shm buffer the window committed last, as ARGB8888 rows.
func (w *wlWindow) gpuShm() []byte {
	w.fbmu.Lock()
	defer w.fbmu.Unlock()
	size := w.stride * w.h
	off := (1 - w.cur) * size
	return append([]byte(nil), w.poolData[off:off+size]...)
}

// A window on the GPU commits, to the compositor, the wl_shm bytes the CPU
// window commits, and the compositor's own capture of its output is that frame.
func TestLiveGPUWaylandShowsTheCPUFrame(t *testing.T) {
	gpuLiveEnabled(t)
	const W, H = 800, 600
	name := gpuSway(t, W, H)
	theme := toolkit.DefaultDark()

	var shm [2][]byte
	var size [2][2]int
	for i, gpu := range []bool{false, true} {
		win := gpuWaylandWindow(t, name, gpu, 320, 200)
		// sway configures the toplevel to the output's size: the GPU path is
		// resized once on the way, live.
		gw, gh := W, H
		if !gpuSpawn() {
			time.Sleep(time.Second)
			gw, gh = win.Size()
		}
		gpuSettle(t, win.Size, gw, gh)
		shm[i] = win.gpuShm()
		size[i] = [2]int{gw, gh}
		if gpu {
			if !win.GPU() {
				t.Fatalf("the window left the GPU path: %v", win.GPUError())
			}
			if gpuSpawn() && gpuToolOK("grim") {
				gpuGrim(t, win, theme)
			}
		}
		_ = win.Close()
	}
	if size[0] != size[1] {
		t.Fatalf("the windows are %v and %v", size[0], size[1])
	}
	w, h := size[0][0], size[0][1]
	want := make([]byte, 4*w*h)
	wayland.PackARGB8888(want, 4*w, gpuCPUFrame(theme, w, h), 4*w, w, h)
	for _, c := range []struct {
		name      string
		got, want []byte
	}{
		{"CPU window's wl_shm buffer vs PixelPainter packed as ARGB8888", shm[0], want},
		{"GPU window's wl_shm buffer vs CPU window's", shm[1], shm[0]},
		{"GPU window's wl_shm buffer vs PixelPainter packed as ARGB8888", shm[1], want},
	} {
		if !bytes.Equal(c.got, c.want) {
			t.Error(gpuReport(c.name, c.got, c.want, w))
		} else {
			t.Logf("equal: %s (%d bytes, %d × %d)", c.name, len(c.got), w, h)
		}
	}
}

func gpuToolOK(name string) bool { _, err := exec.LookPath(name); return err == nil }

// gpuGrim compares the compositor's capture of its (private) output with the
// CPU frame: the window fills the output, so every pixel is the window's.
func gpuGrim(t testing.TB, win *wlWindow, theme *toolkit.Theme) {
	t.Helper()
	out := filepath.Join(t.TempDir(), "grim.png")
	if b, err := exec.Command("grim", out).CombinedOutput(); err != nil {
		t.Fatalf("grim: %v\n%s", err, b)
	}
	f, err := os.Open(out)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decode grim's capture: %v", err)
	}
	w, h := win.Size()
	if b := img.Bounds(); b.Dx() != w || b.Dy() != h {
		t.Logf("grim captured %d × %d, the window is %d × %d: not compared", b.Dx(), b.Dy(), w, h)
		return
	}
	want := gpuCPUFrame(theme, w, h)
	bad := 0
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			r, g, b, _ := img.At(x, y).RGBA()
			i := 4 * (y*w + x)
			if uint8(r>>8) != want[i] || uint8(g>>8) != want[i+1] || uint8(b>>8) != want[i+2] {
				bad++
			}
		}
	}
	if bad != 0 {
		t.Errorf("the compositor's capture of the GPU window differs from PixelPainter at %d of %d pixels", bad, w*h)
		return
	}
	t.Logf("equal: the compositor's capture of the GPU window vs PixelPainter (%d pixels)", w*h)
}

// --- the measurement --------------------------------------------------------

// gpuCPUTime is the process's user + system time so far, every thread of it:
// lavapipe's worker threads are the device's work, and are counted.
func gpuCPUTime() time.Duration {
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
}

// BenchmarkLiveGPUFrame paints and presents one whole frame of a live window:
// PixelPainter then the back-end's present, or gpupainter, Vulkan, the copy and
// the same present. cpu-ns/op is the process's CPU time per frame. On Wayland a
// frame also includes one round trip with the compositor (see gpuWaylandIdle),
// the same for both paths. On a machine whose "GPU" is lavapipe this is a CPU
// rasterizer's number, not a GPU's.
func BenchmarkLiveGPUFrame(b *testing.B) {
	gpuLiveEnabled(b)
	disp := gpuXvfb(b)
	for _, size := range []struct {
		name string
		w, h int
	}{{"960x640", 960, 640}, {"1920x1200", 1920, 1200}} {
		for _, proto := range []string{"x11", "wayland"} {
			for _, gpu := range []bool{false, true} {
				name := proto + "/" + size.name + "/cpu"
				if gpu {
					name = proto + "/" + size.name + "/gpu"
				}
				b.Run(name, func(b *testing.B) {
					var frame func()
					if proto == "x11" {
						win := gpuX11Window(b, disp, gpu, size.w, size.h)
						time.Sleep(300 * time.Millisecond)
						frame = func() { _ = win.paintFrame(false, false) }
						if w, h := win.Size(); w != size.w || h != size.h {
							b.Fatalf("window is %d × %d", w, h)
						}
					} else {
						n := gpuSway(b, size.w, size.h)
						_, frame = gpuWaylandIdle(b, n, gpu, size.w, size.h)
					}
					b.ReportAllocs()
					start := gpuCPUTime()
					b.ResetTimer()
					for b.Loop() {
						frame()
					}
					b.StopTimer()
					b.ReportMetric(float64(gpuCPUTime()-start)/float64(b.N), "cpu-ns/op")
				})
			}
		}
	}
}
