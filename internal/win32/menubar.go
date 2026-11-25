// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package win32

import (
	"strings"

	"github.com/go-widgets/tray"
)

// The menu bar as data: what a tray.Menu becomes on Win32, worked out without a
// window so every rule is testable on any host. menubar_windows.go turns the plan
// into an HMENU (CreateMenu, AppendMenuW, SetMenu) and routes WM_COMMAND and the
// key accelerators back to the tray.MenuItem handlers.
//
// References: Microsoft, "AppendMenuW function" (the MF_* flags, and the "\t"
// that separates an item's text from its accelerator text), "WM_COMMAND" (a menu
// item sends HIWORD(wParam)==0 and lParam==0), "SetMenu function" (the window must
// be redrawn with DrawMenuBar), learn.microsoft.com/windows/win32/menurc.

// WMAppMenu is the private message SetMenuBar posts so the menu is built on the
// window's own thread. It sits after WMAppVisibility in the WM_APP range.
const WMAppMenu = WMAppVisibility + 1

// The AppendMenuW flags the plan uses.
const (
	MFString    = 0x0000
	MFGrayed    = 0x0001
	MFChecked   = 0x0008
	MFPopup     = 0x0010
	MFSeparator = 0x0800
)

// Virtual-key codes the accelerators map to (winuser.h).
const (
	accBack   = 0x08
	accTab    = 0x09
	accReturn = 0x0D
	accEscape = 0x1B
	accSpace  = 0x20
	accLeft   = 0x25
	accUp     = 0x26
	accRight  = 0x27
	accDown   = 0x28
	accDelete = 0x2E
	accF1     = 0x70
)

// AccelMods are the modifier keys an accelerator needs, as the Win32 key state
// reports them.
type AccelMods uint8

const (
	AccelCtrl AccelMods = 1 << iota
	AccelShift
	AccelAlt
)

// MenuEntry is one row of a menu: a separator, a submenu parent (Sub non-nil) or
// a command (ID > 0).
type MenuEntry struct {
	Label string // with the accelerator text after a tab
	Flags uint32 // MF_* for AppendMenuW
	ID    uint16 // WM_COMMAND id of a command, 0 otherwise
	Sub   []MenuEntry
}

// Accel binds a key chord to a command id.
type Accel struct {
	VK   uint32
	Mods AccelMods
	ID   uint16
}

// BarPlan is a tray.Menu ready for the Win32 menu-bar calls.
type BarPlan struct {
	Entries []MenuEntry
	Accels  []Accel
	// Leaves maps a command id (1-based) to its item: Leaves[id-1].
	Leaves []*tray.MenuItem
	// Signature is tray's: equal signatures draw the same menu.
	Signature string
}

// maxCommands is how many command ids a menu may hold: ids are 16 bits and 0 is
// "no command".
const maxCommands = 0xFFFF

// PlanBar works out the menu bar for m. A nil menu is an empty bar. Commands
// beyond maxCommands are dropped, which no real menu comes near.
func PlanBar(m *tray.Menu) BarPlan {
	p := BarPlan{Signature: m.Signature()}
	p.Leaves = m.Leaves()
	if len(p.Leaves) > maxCommands {
		p.Leaves = p.Leaves[:maxCommands]
	}
	next := 0
	var walk func(*tray.Menu) []MenuEntry
	walk = func(mm *tray.Menu) []MenuEntry {
		var out []MenuEntry
		if mm == nil {
			return out
		}
		for _, it := range mm.Items {
			switch {
			case it.Separator:
				out = append(out, MenuEntry{Flags: MFSeparator})
			case it.Submenu != nil:
				flags := uint32(MFString | MFPopup)
				if it.Disabled {
					flags |= MFGrayed
				}
				out = append(out, MenuEntry{Label: it.Label, Flags: flags, Sub: walk(it.Submenu)})
			default:
				if next >= len(p.Leaves) {
					continue
				}
				next++
				id := uint16(next)
				flags := uint32(MFString)
				if it.Disabled {
					flags |= MFGrayed
				}
				if it.Checked {
					flags |= MFChecked
				}
				label := it.Label
				if vk, mods, name, ok := Chord(it.Key, it.Mods); ok {
					label += "\t" + name
					// An editing command (Copy, Paste...) is handled by the focused
					// field, which gets the key; a second delivery from here would
					// act twice. It keeps its text but takes no accelerator.
					if !editRole(it.Role) {
						p.Accels = append(p.Accels, Accel{VK: vk, Mods: mods, ID: id})
					}
				}
				out = append(out, MenuEntry{Label: label, Flags: flags, ID: id})
			}
		}
		return out
	}
	p.Entries = walk(m)
	return p
}

func editRole(r tray.Role) bool {
	switch r {
	case tray.RoleUndo, tray.RoleRedo, tray.RoleCut, tray.RoleCopy, tray.RolePaste, tray.RoleSelectAll:
		return true
	}
	return false
}

// Chord translates a tray key equivalent into a virtual key, its modifiers and
// the text a Windows menu shows for it ("Ctrl+Shift+S"). ok is false for no key,
// or for a key with no virtual-key code here.
func Chord(key string, mods tray.Mods) (vk uint32, m AccelMods, name string, ok bool) {
	vk, base, ok := keyCode(key) // an empty key has no code
	if !ok {
		return 0, 0, "", false
	}
	var parts []string
	if mods&(tray.ModCommand|tray.ModControl) != 0 {
		m |= AccelCtrl
		parts = append(parts, "Ctrl")
	}
	if mods&tray.ModOption != 0 {
		m |= AccelAlt
		parts = append(parts, "Alt")
	}
	if mods&tray.ModShift != 0 {
		m |= AccelShift
		parts = append(parts, "Shift")
	}
	parts = append(parts, base)
	return vk, m, strings.Join(parts, "+"), true
}

func keyCode(key string) (vk uint32, name string, ok bool) {
	special := []struct {
		key  string
		vk   uint32
		name string
	}{
		{tray.KeyUp, accUp, "Up"}, {tray.KeyDown, accDown, "Down"},
		{tray.KeyLeft, accLeft, "Left"}, {tray.KeyRight, accRight, "Right"},
		{tray.KeyReturn, accReturn, "Enter"}, {tray.KeyEscape, accEscape, "Esc"},
		{tray.KeyDelete, accBack, "Backspace"}, {tray.KeyForwardDelete, accDelete, "Del"},
		{tray.KeyTab, accTab, "Tab"}, {tray.KeySpace, accSpace, "Space"},
	}
	for _, s := range special {
		if key == s.key {
			return s.vk, s.name, true
		}
	}
	fkeys := []string{
		tray.KeyF1, tray.KeyF2, tray.KeyF3, tray.KeyF4, tray.KeyF5, tray.KeyF6, tray.KeyF7, tray.KeyF8,
		tray.KeyF9, tray.KeyF10, tray.KeyF11, tray.KeyF12, tray.KeyF13, tray.KeyF14, tray.KeyF15,
	}
	for i, f := range fkeys {
		if key == f {
			return accF1 + uint32(i), "F" + itoa(i+1), true
		}
	}
	if len(key) == 1 {
		c := key[0]
		switch {
		case c >= 'a' && c <= 'z':
			return uint32(c - 'a' + 'A'), string(rune(c - 'a' + 'A')), true
		case c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			return uint32(c), string(rune(c)), true
		}
	}
	return 0, "", false
}

func itoa(n int) string {
	if n >= 10 {
		return string(rune('0'+n/10)) + string(rune('0'+n%10))
	}
	return string(rune('0' + n))
}

// MatchAccel returns the command id bound to the chord (vk, mods), or 0.
func (p BarPlan) MatchAccel(vk uint32, mods AccelMods) uint16 {
	for _, a := range p.Accels {
		if a.VK == vk && a.Mods == mods {
			return a.ID
		}
	}
	return 0
}

// Dispatch activates command id, ignoring one that is not in the plan.
func (p BarPlan) Dispatch(id uint16) bool {
	if id == 0 || int(id) > len(p.Leaves) {
		return false
	}
	// Activate does nothing for a disabled row; the key is consumed all the same,
	// so a grayed row does not fall through to a field.
	p.Leaves[id-1].Activate()
	return true
}
