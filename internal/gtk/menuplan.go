// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package gtk

import (
	"strconv"

	"github.com/go-gtk/gtk4"
	"github.com/go-widgets/tray"
)

// PlanBar turns a tray.Menu into the rows of a gtk4.MenuBar. A nil menu is an
// empty bar. It is pure (no GTK call), so it runs and is tested on every OS.
//
// The mapping mirrors the Windows bar: top-level items are menus, a separator is
// a separator, a checked row is a stateful boolean action, Disabled is a greyed
// row, and a pick runs the item's own Activate (which flips a checkbox and calls
// its handler). Key equivalents become GTK accelerators, with ⌘ read as Control
// as it is on Windows; an editing role (Copy, Paste...) keeps its text but takes
// no accelerator, because the focused field already handles that chord and a
// second delivery from the window would act twice.
//
// pick runs the n-th command (depth-first, the order of [tray.Menu.Leaves]); the
// window resolves it against the menu it holds NOW, so a menu that changed
// nothing visible still has its latest handlers called. A nil pick runs the
// item the plan was made from.
func PlanBar(m *tray.Menu, pick func(n int)) []gtk4.BarItem {
	n := 0
	return planBar(m, pick, &n)
}

func planBar(m *tray.Menu, pick func(int), n *int) []gtk4.BarItem {
	if m == nil {
		return nil
	}
	out := make([]gtk4.BarItem, 0, len(m.Items))
	for _, it := range m.Items {
		it := it
		switch {
		case it.Separator:
			out = append(out, gtk4.BarItem{Separator: true})
		case it.Submenu != nil:
			out = append(out, gtk4.BarItem{Label: it.Label, Disabled: it.Disabled,
				Sub: append([]gtk4.BarItem{}, planBar(it.Submenu, pick, n)...)})
		default:
			idx := *n
			*n++
			run := it.Activate
			if pick != nil {
				run = func() { pick(idx) }
			}
			b := gtk4.BarItem{
				Label:      it.Label,
				Disabled:   it.Disabled,
				Check:      it.Checked,
				Checked:    it.Checked,
				OnActivate: run,
			}
			if !editRole(it.Role) {
				b.Accel = Accel(it.Key, it.Mods)
			}
			out = append(out, b)
		}
	}
	return out
}

func editRole(r tray.Role) bool {
	switch r {
	case tray.RoleUndo, tray.RoleRedo, tray.RoleCut, tray.RoleCopy, tray.RolePaste, tray.RoleSelectAll:
		return true
	}
	return false
}

// Accel translates a tray key equivalent into a GTK accelerator string such as
// "<Control><Shift>s", or "" for no key or one this map does not know.
func Accel(key string, mods tray.Mods) string {
	name, ok := keyName(key)
	if !ok {
		return ""
	}
	var s string
	if mods&(tray.ModCommand|tray.ModControl) != 0 {
		s += "<Control>"
	}
	if mods&tray.ModOption != 0 {
		s += "<Alt>"
	}
	if mods&tray.ModShift != 0 {
		s += "<Shift>"
	}
	return s + name
}

func keyName(key string) (string, bool) {
	special := map[string]string{
		tray.KeyUp: "Up", tray.KeyDown: "Down", tray.KeyLeft: "Left", tray.KeyRight: "Right",
		tray.KeyReturn: "Return", tray.KeyEscape: "Escape", tray.KeyDelete: "BackSpace",
		tray.KeyForwardDelete: "Delete", tray.KeyTab: "Tab", tray.KeySpace: "space",
	}
	if n, ok := special[key]; ok {
		return n, true
	}
	fkeys := []string{
		tray.KeyF1, tray.KeyF2, tray.KeyF3, tray.KeyF4, tray.KeyF5, tray.KeyF6, tray.KeyF7, tray.KeyF8,
		tray.KeyF9, tray.KeyF10, tray.KeyF11, tray.KeyF12, tray.KeyF13, tray.KeyF14, tray.KeyF15,
	}
	for i, f := range fkeys {
		if key == f {
			return "F" + strconv.Itoa(i+1), true
		}
	}
	if len(key) == 1 {
		switch c := key[0]; {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			return key, true
		case c >= 'A' && c <= 'Z':
			return string(rune(c - 'A' + 'a')), true
		}
	}
	return "", false
}
