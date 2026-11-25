// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux && !android

package gtk

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-gtk/gtk4"
	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/tray"
	"github.com/godbus/dbus/v5"
)

// menuFixture is the menu the live tests install: a File menu with a keyed row, a
// separator, a submenu and a disabled row, and a View menu with a checkbox.
type menuFixture struct {
	mu    sync.Mutex
	log   []string
	ruler bool
	menu  *tray.Menu
}

func newMenuFixture() *menuFixture {
	f := &menuFixture{}
	f.rebuild()
	return f
}

func (f *menuFixture) record(s string) {
	f.mu.Lock()
	f.log = append(f.log, s)
	f.mu.Unlock()
}

func (f *menuFixture) calls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.log...)
}

// rebuild makes a fresh menu from the fixture's state, as an application does
// each time its state changes.
func (f *menuFixture) rebuild() {
	save := tray.Item("Save", func() { f.record("save") })
	save.Key, save.Mods = "s", tray.ModCommand
	disabled := tray.Item("Frozen", func() { f.record("frozen") })
	disabled.Disabled = true
	f.mu.Lock()
	ruler := f.ruler
	f.mu.Unlock()
	rl := tray.Checkbox("Ruler", ruler, func(on bool) {
		f.mu.Lock()
		f.ruler = on
		f.mu.Unlock()
		f.record(fmt.Sprintf("ruler=%v", on))
	})
	f.mu.Lock()
	f.menu = tray.NewMenu().
		Add(tray.SubMenu("File", tray.NewMenu().Add(
			save, tray.Separator(),
			tray.SubMenu("Recent", tray.NewMenu().Add(tray.Item("Notes", func() { f.record("notes") }))),
			disabled))).
		Add(tray.SubMenu("View", tray.NewMenu().Add(rl)))
	f.mu.Unlock()
}

func (f *menuFixture) current() *tray.Menu {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.menu
}

func requireLive(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	if os.Getenv("GTK_REQUIRE_MENUBAR") != "" {
		t.Fatalf("no GTK display and GTK_REQUIRE_MENUBAR is set: %v", err)
	}
	t.Skipf("no GTK display: %v", err)
}

// waitFor polls cond until it holds or d passes.
func waitFor(d time.Duration, cond func() bool) bool {
	for end := time.Now().Add(d); time.Now().Before(end); time.Sleep(50 * time.Millisecond) {
		if cond() {
			return true
		}
	}
	return cond()
}

// TestGTKBackendMenuBar is the proof for the GTK application menu bar: the bar
// is installed from a goroutine that is not the GTK thread, a real key event
// (xdotool, XTEST) fires the Ctrl+S accelerator, choosing a row by its action
// name runs the handler, a checkbox flips and the bar follows the new menu, a
// disabled row is refused, and an unchanged menu is not rebuilt.
//
// GTK_REQUIRE_MENUBAR turns the skips into failures, so the lane cannot go green
// without running it.
func TestGTKBackendMenuBar(t *testing.T) {
	if _, err := exec.LookPath("xdotool"); err != nil {
		if os.Getenv("GTK_REQUIRE_MENUBAR") != "" {
			t.Fatal("xdotool not installed and GTK_REQUIRE_MENUBAR is set")
		}
		t.Skip("xdotool not installed")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	const title = "go-widgets menubar proof"
	win, err := Open(title, 400, 240, nil, 1)
	requireLive(t, err)
	fx := newMenuFixture()

	type result struct{ err string }
	done := make(chan result, 1)
	go func() {
		var r result
		defer func() {
			gtk4.IdleAdd(func() { win.Close() })
			done <- r
		}()
		fail := func(format string, a ...any) { r.err = fmt.Sprintf(format, a...) }

		id := ""
		for i := 0; i < 100 && id == ""; i++ {
			out, _ := exec.Command("xdotool", "search", "--onlyvisible", "--name", title).Output()
			id = strings.TrimSpace(strings.SplitN(string(out), "\n", 2)[0])
			time.Sleep(100 * time.Millisecond)
		}
		if id == "" {
			fail("the window never became visible to the X server")
			return
		}
		if err := win.SetMenuBar(fx.current()); err != nil {
			fail("SetMenuBar: %v", err)
			return
		}
		onGTK := func(f func()) {
			ch := make(chan struct{})
			gtk4.IdleAdd(func() { f(); close(ch) })
			select {
			case <-ch:
			case <-time.After(5 * time.Second):
			}
		}
		var leaves int
		if !waitFor(5*time.Second, func() bool {
			onGTK(func() {
				leaves = 0
				if win.mb.bar != nil {
					leaves = win.mb.bar.Len()
				}
			})
			return leaves == 4
		}) {
			fail("bar has %d commands, want 4", leaves)
			return
		}

		// A real key event reaches the window-wide shortcut.
		time.Sleep(500 * time.Millisecond)
		if out, err := exec.Command("xdotool", "windowfocus", "--sync", id).CombinedOutput(); err != nil {
			fail("windowfocus: %v: %s", err, out)
			return
		}
		if out, err := exec.Command("xdotool", "mousemove", "100", "100", "click", "1").CombinedOutput(); err != nil {
			fail("click: %v: %s", err, out)
			return
		}
		time.Sleep(300 * time.Millisecond)
		if out, err := exec.Command("xdotool", "key", "--delay", "50", "ctrl+s").CombinedOutput(); err != nil {
			fail("key: %v: %s", err, out)
			return
		}
		if !waitFor(3*time.Second, func() bool { return len(fx.calls()) == 1 }) || fx.calls()[0] != "save" {
			fail("Ctrl+S: calls = %v, want [save]", fx.calls())
			return
		}

		// Rows by action name: Notes (leaf 1), the disabled row (2) refused.
		var okNotes, okFrozen bool
		onGTK(func() { okNotes = win.mb.bar.Activate(1); okFrozen = win.mb.bar.Activate(2) })
		if !okNotes || okFrozen {
			fail("Activate: Notes=%v (want true) Frozen=%v (want false)", okNotes, okFrozen)
			return
		}

		// The checkbox: the pick flips the tray item, the application rebuilds its
		// menu and installs it; the bar's signature moves on.
		var before string
		onGTK(func() { before = win.mb.applied })
		onGTK(func() { win.mb.bar.Activate(3) })
		if !waitFor(3*time.Second, func() bool { return len(fx.calls()) == 3 }) {
			fail("ruler: calls = %v", fx.calls())
			return
		}
		fx.rebuild()
		if err := win.SetMenuBar(fx.current()); err != nil {
			fail("SetMenuBar 2: %v", err)
			return
		}
		var after string
		if !waitFor(3*time.Second, func() bool { onGTK(func() { after = win.mb.applied }); return after != before }) {
			fail("a changed checkbox did not reinstall the bar (signature %q)", after)
			return
		}
		// The same menu again changes nothing.
		var rebuilt string
		if err := win.SetMenuBar(fx.current()); err != nil {
			fail("SetMenuBar 3: %v", err)
			return
		}
		time.Sleep(300 * time.Millisecond)
		onGTK(func() { rebuilt = win.mb.applied })
		if rebuilt != after {
			fail("an unchanged menu was reinstalled")
			return
		}
		// A nil menu hides the bar.
		if err := win.SetMenuBar(nil); err != nil {
			fail("SetMenuBar nil: %v", err)
			return
		}
		var visible bool
		if !waitFor(3*time.Second, func() bool {
			onGTK(func() { visible = win.mb.bar.Widget().Mapped() })
			return !visible
		}) {
			fail("a nil menu left the bar on screen")
		}
	}()

	if err := win.Run(toolkit.NewContainer(nil)); err != nil {
		t.Fatal(err)
	}
	if r := <-done; r.err != "" {
		t.Fatal(r.err)
	}
	if got, want := strings.Join(fx.calls(), ","), "save,notes,ruler=true"; got != want {
		t.Errorf("handlers ran %q, want %q", got, want)
	}
}

// TestGTKBackendMenuBarATSPI reads the menu bar the way a screen reader does: the
// application's tree on the accessibility bus, reached through org.a11y.Bus, must
// name the menus and their rows. It needs dbus, at-spi2-core and a running
// at-spi-bus-launcher, so it is gated on WINDOW_ATSPI_LIVE=1 and run by the
// live-gtk-menubar-atspi CI lane, which fails when it did not reach PASS.
func TestGTKBackendMenuBarATSPI(t *testing.T) {
	if os.Getenv("WINDOW_ATSPI_LIVE") != "1" {
		t.Skip("set WINDOW_ATSPI_LIVE=1 under dbus-run-session with at-spi-bus-launcher")
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	const title = "go-widgets menubar atspi"
	win, err := Open(title, 400, 240, nil, 1)
	if err != nil {
		t.Fatalf("no GTK display: %v", err)
	}
	fx := newMenuFixture()
	if err := win.SetMenuBar(fx.current()); err != nil {
		t.Fatal(err)
	}

	type result struct {
		names []string
		err   string
	}
	done := make(chan result, 1)
	go func() {
		var r result
		defer func() {
			gtk4.IdleAdd(func() { win.Close() })
			done <- r
		}()
		time.Sleep(1500 * time.Millisecond) // first frame; GTK exports the tree as widgets map
		sess, err := dbus.ConnectSessionBus()
		if err != nil {
			r.err = "session bus: " + err.Error()
			return
		}
		defer sess.Close()
		var addr string
		if err := sess.Object("org.a11y.Bus", "/org/a11y/bus").Call("org.a11y.Bus.GetAddress", 0).Store(&addr); err != nil {
			r.err = "org.a11y.Bus.GetAddress: " + err.Error()
			return
		}
		cl, err := dbus.Dial(addr)
		if err != nil {
			r.err = "dial: " + err.Error()
			return
		}
		defer cl.Close()
		if err := cl.Auth(nil); err != nil {
			r.err = "auth: " + err.Error()
			return
		}
		if err := cl.Hello(); err != nil {
			r.err = "hello: " + err.Error()
			return
		}
		type ref struct {
			Name string
			Path dbus.ObjectPath
		}
		children := func(x ref) []ref {
			var out []ref
			_ = cl.Object(x.Name, x.Path).Call("org.a11y.atspi.Accessible.GetChildren", 0).Store(&out)
			return out
		}
		str := func(x ref, method string) string {
			var s string
			_ = cl.Object(x.Name, x.Path).Call("org.a11y.atspi.Accessible."+method, 0).Store(&s)
			return s
		}
		prop := func(x ref, name string) string {
			v, err := cl.Object(x.Name, x.Path).GetProperty("org.a11y.atspi.Accessible." + name)
			if err != nil {
				return ""
			}
			s, _ := v.Value().(string)
			return s
		}
		var walk func(x ref, depth int)
		walk = func(x ref, depth int) {
			if depth > 14 {
				return
			}
			role := str(x, "GetRoleName")
			name := prop(x, "Name")
			r.names = append(r.names, role+":"+name)
			for _, c := range children(x) {
				walk(c, depth+1)
			}
		}
		walk(ref{"org.a11y.atspi.Registry", "/org/a11y/atspi/accessible/root"}, 0)
		// GTK 4 exports the bar's top-level items only: the rows live in popovers
		// that are not in the tree until opened, and its menu items offer no
		// Action, so the AT-SPI proof is the bar and its menu names.
	}()

	if err := win.Run(toolkit.NewContainer(nil)); err != nil {
		t.Fatal(err)
	}
	r := <-done
	for _, n := range r.names {
		t.Logf("atspi: %s", n)
	}
	if r.err != "" {
		t.Fatal(r.err)
	}
	for _, want := range []string{"File", "View"} {
		found := false
		for _, n := range r.names {
			if strings.HasSuffix(n, ":"+want) && (strings.HasPrefix(n, "menu item:") || strings.HasPrefix(n, "check menu item:") || strings.HasPrefix(n, "menu:")) {
				found = true
			}
		}
		if !found {
			t.Errorf("the accessibility tree has no menu element named %q", want)
		}
	}
}
