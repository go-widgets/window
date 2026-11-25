// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package gtk

import (
	"testing"

	"github.com/go-widgets/tray"
)

func TestAccel(t *testing.T) {
	for _, c := range []struct {
		key  string
		mods tray.Mods
		want string
	}{
		{"s", tray.ModCommand, "<Control>s"},
		{"S", tray.ModCommand | tray.ModShift, "<Control><Shift>s"},
		{"q", tray.ModControl | tray.ModOption, "<Control><Alt>q"},
		{tray.KeyF5, 0, "F5"},
		{tray.KeyLeft, tray.ModOption, "<Alt>Left"},
		{tray.KeyDelete, tray.ModCommand, "<Control>BackSpace"},
		{tray.KeySpace, 0, "space"},
		{"", tray.ModCommand, ""},
		{"=", tray.ModCommand, ""},
	} {
		if got := Accel(c.key, c.mods); got != c.want {
			t.Errorf("Accel(%q, %v) = %q, want %q", c.key, c.mods, got, c.want)
		}
	}
}

func TestPlanBar(t *testing.T) {
	if PlanBar(nil, nil) != nil {
		t.Fatal("nil menu must plan an empty bar")
	}
	var log []string
	copyItem := tray.Item("Copy", func() { log = append(log, "copy") })
	copyItem.Key, copyItem.Mods, copyItem.Role = "c", tray.ModCommand, tray.RoleCopy
	save := tray.Item("Save", func() { log = append(log, "save") })
	save.Key, save.Mods = "s", tray.ModCommand
	ruler := tray.Checkbox("Ruler", true, func(on bool) {
		if on {
			log = append(log, "ruler on")
		} else {
			log = append(log, "ruler off")
		}
	})
	off := tray.Item("Off", nil)
	off.Disabled = true
	m := tray.NewMenu().
		Add(tray.SubMenu("File", tray.NewMenu().Add(save, tray.Separator(), off))).
		Add(tray.SubMenu("Edit", tray.NewMenu().Add(copyItem, ruler))).
		Add(tray.SubMenu("Empty", tray.NewMenu()))
	bar := PlanBar(m, nil)
	if len(bar) != 3 {
		t.Fatalf("top-level = %d, want 3", len(bar))
	}
	file := bar[0].Sub
	if len(file) != 3 || !file[1].Separator || !file[2].Disabled {
		t.Fatalf("File = %+v", file)
	}
	if file[0].Accel != "<Control>s" {
		t.Errorf("Save accel = %q", file[0].Accel)
	}
	edit := bar[1].Sub
	if edit[0].Accel != "" {
		t.Errorf("an editing role must take no accelerator, got %q", edit[0].Accel)
	}
	if !edit[1].Check || !edit[1].Checked {
		t.Errorf("Ruler = %+v, want a checked check row", edit[1])
	}
	if bar[2].Sub == nil {
		t.Error("an empty submenu must stay a menu (Sub non-nil)")
	}
	file[0].OnActivate()
	edit[0].OnActivate()
	edit[1].OnActivate()
	want := []string{"save", "copy", "ruler off"}
	if len(log) != 3 || log[0] != want[0] || log[1] != want[1] || log[2] != want[2] {
		t.Errorf("activations = %v, want %v", log, want)
	}
}
