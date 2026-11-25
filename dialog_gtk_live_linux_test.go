// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux && !android && (amd64 || arm64)

package window

import (
	"errors"
	"fmt"
	"os"
	"reflect"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/ebitengine/purego"
	"github.com/go-gtk/gtk4"
	"github.com/go-widgets/toolkit"
)

const dlgParentTitle = "dlgtest-parent"

// skipOrFail skips, unless GTK_REQUIRE_DIALOGS is set (the CI lane that has a
// display and GTK 4.10+), where a skip would read as a pass.
func skipOrFail(t *testing.T, format string, a ...any) {
	t.Helper()
	if os.Getenv("GTK_REQUIRE_DIALOGS") != "" {
		t.Fatalf(format, a...)
	}
	t.Skipf(format, a...)
}

// gtkDriver finds the dialogs GTK opens next to the parent window and acts on
// them in-process, through libgtk-4's own C API: it presses a button as a person
// would, or closes the dialog window (what Escape and the close control do).
type gtkDriver struct {
	toplevels  func() uintptr
	nItems     func(uintptr) uint32
	item       func(uintptr, uint32) uintptr
	unref      func(uintptr)
	mapped     func(uintptr) bool
	title      func(uintptr) string
	firstChild func(uintptr) uintptr
	nextSib    func(uintptr) uintptr
	isA        func(uintptr, uint64) bool
	buttonType func() uint64
	buttonText func(uintptr) string
	closeWin   func(uintptr)
	activate   func(uintptr)
	timeout    func(uint32, uintptr, uintptr) uint32
}

func newGTKDriver(t *testing.T) *gtkDriver {
	t.Helper()
	var d gtkDriver
	gtk, err := purego.Dlopen("libgtk-4.so.1", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	if err != nil {
		skipOrFail(t, "no libgtk-4: %v", err)
	}
	glib, _ := purego.Dlopen("libglib-2.0.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	gobj, _ := purego.Dlopen("libgobject-2.0.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	gio, _ := purego.Dlopen("libgio-2.0.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
	for p, n := range map[any]struct {
		lib  uintptr
		name string
	}{
		&d.toplevels:  {gtk, "gtk_window_get_toplevels"},
		&d.nItems:     {gio, "g_list_model_get_n_items"},
		&d.item:       {gio, "g_list_model_get_item"},
		&d.unref:      {gobj, "g_object_unref"},
		&d.mapped:     {gtk, "gtk_widget_get_mapped"},
		&d.title:      {gtk, "gtk_window_get_title"},
		&d.firstChild: {gtk, "gtk_widget_get_first_child"},
		&d.nextSib:    {gtk, "gtk_widget_get_next_sibling"},
		&d.isA:        {gobj, "g_type_check_instance_is_a"},
		&d.buttonType: {gtk, "gtk_button_get_type"},
		&d.buttonText: {gtk, "gtk_button_get_label"},
		&d.closeWin:   {gtk, "gtk_window_close"},
		&d.activate:   {gtk, "gtk_widget_activate"},
		&d.timeout:    {glib, "g_timeout_add"},
	} {
		purego.RegisterLibFunc(p, n.lib, n.name)
	}
	return &d
}

// dialogs lists the mapped windows that are not the parent.
func (d *gtkDriver) dialogs() []uintptr {
	m := d.toplevels()
	var out []uintptr
	for i, n := uint32(0), d.nItems(m); i < n; i++ {
		w := d.item(m, i)
		if d.mapped(w) && d.title(w) != dlgParentTitle {
			out = append(out, w)
		}
		d.unref(w)
	}
	return out
}

func (d *gtkDriver) buttons(w uintptr) (btns []uintptr, labels []string) {
	var walk func(uintptr)
	walk = func(n uintptr) {
		for c := d.firstChild(n); c != 0; c = d.nextSib(c) {
			if d.isA(c, d.buttonType()) {
				btns = append(btns, c)
				labels = append(labels, d.buttonText(c))
			}
			walk(c)
		}
	}
	walk(w)
	return
}

// TestGTKBackendDialogs drives the public window.Alert, OpenFile and SaveFile over
// the GTK backend on a real GTK 4.10+ under a display: an alert button pressed, a
// file dialog and a save dialog dismissed, and the default "OK" alert. Each
// callback opens the next dialog, so it also shows a callback may call back into
// the window. It skips without a display or on a GTK older than 4.10.
func TestGTKBackendDialogs(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	t.Setenv("GO_WIDGETS_GTK", "1")
	b, err := Open(Config{Title: dlgParentTitle, Width: 320, Height: 200})
	if err != nil {
		skipOrFail(t, "no GTK display: %v", err)
	}
	if !gtk4.DialogsSupported() {
		skipOrFail(t, "GTK older than 4.10%s", "")
	}
	if _, ok := b.(Dialogs); !ok {
		t.Fatalf("%T is not a Dialogs", b)
	}
	d := newGTKDriver(t)

	var (
		mu      sync.Mutex
		steps   int // dialogs answered so far
		alert1  AlertResult
		openRes OpenResult
		saveRes SaveResult
		alert2  AlertResult
		failure error
	)
	answered := func() int { mu.Lock(); defer mu.Unlock(); return steps }
	next := func(f func()) { mu.Lock(); f(); steps++; mu.Unlock() }
	fail := func(err error) {
		mu.Lock()
		if failure == nil {
			failure = err
		}
		mu.Unlock()
		b.Close()
	}

	start := func() error {
		return Alert(b, AlertOptions{Message: "Delete?", Detail: "d", Buttons: []string{"Yes", "No"}}, func(r AlertResult) {
			next(func() { alert1 = r })
			if err := OpenFile(b, OpenOptions{Title: "t", Multiple: true,
				Filters: []Filter{{Name: "Text", Extensions: []string{"txt"}}}}, func(r OpenResult) {
				next(func() { openRes = r })
				if err := SaveFile(b, SaveOptions{Title: "s", Name: "x.txt"}, func(r SaveResult) {
					next(func() { saveRes = r })
					if err := Alert(b, AlertOptions{Message: "done"}, func(r AlertResult) {
						next(func() { alert2 = r })
						b.Close()
					}); err != nil {
						fail(err)
					}
				}); err != nil {
					fail(err)
				}
			}); err != nil {
				fail(err)
			}
		})
	}
	if err := start(); err != nil {
		t.Fatalf("Alert: %v", err)
	}

	deadline := time.Now().Add(45 * time.Second)
	seen := map[int]int{}
	acted := map[int]bool{}
	cb := purego.NewCallback(func(_ uintptr) int32 {
		if time.Now().After(deadline) {
			var titles []string
			for _, c := range d.dialogs() {
				titles = append(titles, d.title(c))
			}
			fail(fmt.Errorf("timeout: %d of 4 dialogs answered, open windows titled %q", answered(), titles))
			return 0
		}
		// An answered dialog's window can outlive its callback a moment, and GTK
		// reuses addresses, so the dialog to act on is the one whose title is the
		// current step's, once per step.
		step := answered()
		wantTitle := map[int]string{1: "t", 2: "s"}[step] // the alerts have none
		w := uintptr(0)
		for _, c := range d.dialogs() {
			if d.title(c) == wantTitle {
				w = c
				break
			}
		}
		if w == 0 || acted[step] {
			return 1
		}
		if seen[step]++; seen[step] < 6 { // mapped for ~300 ms: let it populate
			return 1
		}
		acted[step] = true
		switch step {
		case 0: // the two-button alert: press "No"
			btns, labels := d.buttons(w)
			if !reflect.DeepEqual(labels, []string{"Yes", "No"}) {
				fail(errors.New("alert buttons " + join(labels)))
				return 0
			}
			d.activate(btns[1])
		case 3: // the default alert: one "OK" button
			btns, labels := d.buttons(w)
			if !reflect.DeepEqual(labels, []string{"OK"}) {
				fail(errors.New("default alert buttons " + join(labels)))
				return 0
			}
			d.activate(btns[0])
		default: // the file and save dialogs: dismissed
			d.closeWin(w)
		}
		return 1
	})
	d.timeout(50, cb, 0)

	if err := b.Run(toolkit.NewSurface(func() ([]byte, int, int) { return make([]byte, 320*200*4), 320, 200 })); err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if failure != nil {
		t.Fatal(failure)
	}
	if steps != 4 {
		t.Fatalf("%d of 4 dialogs answered", steps)
	}
	if alert1.Err != nil || alert1.Button != 1 {
		t.Errorf("alert: %+v, want button 1", alert1)
	}
	if !errors.Is(openRes.Err, ErrCancelled) || len(openRes.Paths) != 0 {
		t.Errorf("open: %+v, want ErrCancelled", openRes)
	}
	if !errors.Is(saveRes.Err, ErrCancelled) || saveRes.Path != "" {
		t.Errorf("save: %+v, want ErrCancelled", saveRes)
	}
	if alert2.Err != nil || alert2.Button != 0 {
		t.Errorf("default alert: %+v, want button 0", alert2)
	}
}

func join(ss []string) string {
	out := ""
	for _, s := range ss {
		out += "[" + s + "]"
	}
	return out
}
