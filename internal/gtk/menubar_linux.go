// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux && !android

package gtk

import (
	"errors"
	"sync"

	"github.com/go-gtk/gtk4"
	"github.com/go-widgets/tray"
)

// ErrNoMenuBar is returned where the GTK binding has no menu bar (an
// architecture go-gtk/gtk4 does not support). The parent package maps it to
// window.ErrNotSupported.
var ErrNoMenuBar = errors.New("gtk: no GtkPopoverMenuBar on this platform")

// menuBarState is what SetMenuBar leaves on a Window. mu guards menu and sig,
// which are written from any goroutine; bar, applied and leaves belong to the
// GTK main thread.
type menuBarState struct {
	mu      sync.Mutex
	menu    *tray.Menu
	sig     string // the signature the next apply will install
	set     bool
	queued  bool
	bar     *gtk4.MenuBar
	applied string
	built   bool
	leaves  []*tray.MenuItem // the current menu's commands, for late-bound picks
}

// SetMenuBar installs m as the window's menu bar: a GtkPopoverMenuBar above the
// content, its actions on the window, its key equivalents as window shortcuts.
// It is safe from any goroutine; the bar is built on the GTK main thread (before
// Run the work waits for the loop) and again only when m's signature changed. The
// handlers are always the latest ones. A nil or empty menu hides the bar.
func (w *Window) SetMenuBar(m *tray.Menu) error {
	if w.closing.Load() {
		return ErrClosed
	}
	if !gtk4.MenuBarSupported() {
		return ErrNoMenuBar
	}
	s := &w.mb
	s.mu.Lock()
	s.menu = m
	s.leaves = m.Leaves()
	s.sig, s.set = m.Signature(), true
	queue := !s.queued
	s.queued = true
	s.mu.Unlock()
	if queue {
		gtk4.IdleAdd(w.applyMenuBar)
	}
	return nil
}

// pick runs command n of the menu the window holds now. It is called on the main
// thread by GTK, and re-applies afterwards: a checkbox flipped its own state, and
// the handler may have changed the menu.
func (w *Window) pickMenu(n int) {
	s := &w.mb
	s.mu.Lock()
	var it *tray.MenuItem
	if n >= 0 && n < len(s.leaves) {
		it = s.leaves[n]
	}
	s.mu.Unlock()
	if it != nil {
		it.Activate()
	}
	w.refreshMenuBar()
}

// refreshMenuBar re-reads the current menu's signature and installs it if the
// bar shows something else (a checkbox flipped by a pick).
func (w *Window) refreshMenuBar() {
	s := &w.mb
	s.mu.Lock()
	if s.menu != nil {
		s.sig = s.menu.Signature()
	}
	queue := !s.queued && s.built && s.sig != s.applied
	if queue {
		s.queued = true
	}
	s.mu.Unlock()
	if queue {
		gtk4.IdleAdd(w.applyMenuBar)
	}
}

// applyMenuBar runs on the GTK main thread.
func (w *Window) applyMenuBar() {
	s := &w.mb
	s.mu.Lock()
	s.queued = false
	m, sig := s.menu, s.sig
	s.mu.Unlock()
	if w.closing.Load() || (s.built && sig == s.applied) {
		return
	}
	if s.bar == nil {
		bar := gtk4.MenuBarNew(w.win)
		if bar == nil {
			return
		}
		s.bar = bar
		w.box.Prepend(bar.Widget())
	}
	items := PlanBar(m, w.pickMenu)
	s.bar.SetItems(items)
	s.bar.Widget().SetVisible(len(items) > 0)
	s.applied, s.built = sig, true
}
