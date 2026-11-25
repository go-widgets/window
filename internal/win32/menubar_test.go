// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package win32

import (
	"testing"

	"github.com/go-widgets/tray"
)

func TestChord(t *testing.T) {
	for _, tc := range []struct {
		key  string
		mods tray.Mods
		vk   uint32
		am   AccelMods
		name string
		ok   bool
	}{
		{"s", tray.ModCommand, 'S', AccelCtrl, "Ctrl+S", true},
		{"S", tray.ModCommand | tray.ModShift, 'S', AccelCtrl | AccelShift, "Ctrl+Shift+S", true},
		{"1", tray.ModControl | tray.ModOption, '1', AccelCtrl | AccelAlt, "Ctrl+Alt+1", true},
		{tray.KeyUp, 0, accUp, 0, "Up", true},
		{tray.KeyF1, 0, 0x70, 0, "F1", true},
		{tray.KeyF12, tray.ModShift, 0x7B, AccelShift, "Shift+F12", true},
		{tray.KeyF15, 0, 0x7E, 0, "F15", true},
		{tray.KeyReturn, tray.ModCommand, accReturn, AccelCtrl, "Ctrl+Enter", true},
		{tray.KeyDelete, 0, accBack, 0, "Backspace", true},
		{tray.KeyForwardDelete, 0, accDelete, 0, "Del", true},
		{"", tray.ModCommand, 0, 0, "", false},
		{"?", tray.ModCommand, 0, 0, "", false},
	} {
		vk, am, name, ok := Chord(tc.key, tc.mods)
		if vk != tc.vk || am != tc.am || name != tc.name || ok != tc.ok {
			t.Errorf("Chord(%q, %v) = (%#x, %v, %q, %v), want (%#x, %v, %q, %v)",
				tc.key, tc.mods, vk, am, name, ok, tc.vk, tc.am, tc.name, tc.ok)
		}
	}
}

func TestPlanBar(t *testing.T) {
	var fired []string
	on := func(s string) func() { return func() { fired = append(fired, s) } }
	copyRow := tray.RoleItem(tray.RoleCopy)
	m := tray.NewMenu().Add(
		tray.SubMenu("File", tray.NewMenu().Add(
			tray.KeyItem("Save", nil, "s", tray.ModCommand, on("save")),
			tray.Separator(),
			tray.Checkbox("Flag", true, nil),
			tray.Item("Plain", on("plain")),
		)),
		tray.SubMenu("Edit", tray.NewMenu().Add(copyRow)),
	)
	m.Items[0].Submenu.Items[3].Disabled = true

	p := PlanBar(m)
	if len(p.Leaves) != 4 {
		t.Fatalf("Leaves = %d, want 4 (Save, Flag, Plain, Copy)", len(p.Leaves))
	}
	if len(p.Entries) != 2 || p.Entries[0].Label != "File" || p.Entries[0].Flags&MFPopup == 0 {
		t.Fatalf("top level = %+v", p.Entries)
	}
	file := p.Entries[0].Sub
	if file[0].Label != "Save\tCtrl+S" || file[0].ID != 1 {
		t.Errorf("Save = %+v", file[0])
	}
	if file[1].Flags != MFSeparator || file[1].ID != 0 {
		t.Errorf("separator = %+v", file[1])
	}
	if file[2].Flags&MFChecked == 0 || file[2].ID != 2 {
		t.Errorf("Flag = %+v, want checked, id 2", file[2])
	}
	if file[3].Flags&MFGrayed == 0 || file[3].ID != 3 {
		t.Errorf("Plain = %+v, want grayed, id 3", file[3])
	}
	// Copy shows its key but gets no accelerator: the focused field takes the key.
	edit := p.Entries[1].Sub
	if edit[0].Label != "Copy\tCtrl+C" || edit[0].ID != 4 {
		t.Errorf("Copy = %+v", edit[0])
	}
	if len(p.Accels) != 1 || p.Accels[0] != (Accel{VK: 'S', Mods: AccelCtrl, ID: 1}) {
		t.Errorf("Accels = %+v, want only Ctrl+S", p.Accels)
	}

	if id := p.MatchAccel('S', AccelCtrl); id != 1 {
		t.Errorf("MatchAccel(Ctrl+S) = %d", id)
	}
	for _, mods := range []AccelMods{0, AccelCtrl | AccelShift, AccelAlt} {
		if id := p.MatchAccel('S', mods); id != 0 {
			t.Errorf("MatchAccel(S, %v) = %d, want none", mods, id)
		}
	}
	if id := p.MatchAccel('X', AccelCtrl); id != 0 {
		t.Errorf("MatchAccel(Ctrl+X) = %d, want none", id)
	}

	if !p.Dispatch(1) || len(fired) != 1 || fired[0] != "save" {
		t.Errorf("Dispatch(1) fired %v", fired)
	}
	if !p.Dispatch(3) || len(fired) != 1 {
		t.Errorf("a grayed row must be consumed without firing: %v", fired)
	}
	if p.Dispatch(0) || p.Dispatch(5) {
		t.Error("an id outside the plan was accepted")
	}
}

func TestPlanMenuSignatureAndNil(t *testing.T) {
	a := tray.NewMenu().Add(tray.KeyItem("Go", nil, "g", tray.ModCommand, nil))
	b := tray.NewMenu().Add(tray.KeyItem("Go", nil, "g", tray.ModCommand, nil))
	if PlanBar(a).Signature != PlanBar(b).Signature {
		t.Error("identical menus planned with different signatures")
	}
	b.Items[0].Key = "h"
	if PlanBar(a).Signature == PlanBar(b).Signature {
		t.Error("a changed key equivalent kept the signature")
	}
	if p := PlanBar(nil); len(p.Entries) != 0 || len(p.Leaves) != 0 {
		t.Errorf("a nil menu planned %+v", p)
	}
}

func TestWMAppMenuIsItsOwnMessage(t *testing.T) {
	if WMAppMenu == WMAppVisibility || WMAppMenu == WMAppRepaint || WMAppMenu < 0x8000 || WMAppMenu > 0xBFFF {
		t.Errorf("WMAppMenu = %#x", WMAppMenu)
	}
}

func TestPlanMenuDropsCommandsPastTheIDSpace(t *testing.T) {
	m := tray.NewMenu()
	for i := 0; i < maxCommands+5; i++ {
		m.Add(tray.Item("x", func() {}))
	}
	p := PlanBar(m)
	if len(p.Leaves) != maxCommands || len(p.Entries) != maxCommands {
		t.Fatalf("leaves=%d entries=%d, want %d of each", len(p.Leaves), len(p.Entries), maxCommands)
	}
	if last := p.Entries[len(p.Entries)-1]; last.ID != maxCommands {
		t.Errorf("last id = %d, want %d", last.ID, maxCommands)
	}
}

func TestPlanMenuGraysADisabledSubmenu(t *testing.T) {
	m := tray.NewMenu().Add(tray.SubMenu("Recent", tray.NewMenu().Add(tray.Item("a", func() {}))))
	m.Items[0].Disabled = true
	p := PlanBar(m)
	if got := p.Entries[0].Flags; got&MFGrayed == 0 || got&MFPopup == 0 {
		t.Errorf("flags = %#x, want a grayed popup", got)
	}
}
