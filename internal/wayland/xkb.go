// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package wayland

import (
	"regexp"
	"strconv"
	"strings"
)

// A Wayland compositor hands the keyboard layout to the client as an
// xkb_v1 keymap: a text document, delivered over a shared-memory fd, that
// declares (among other sections) which keysym names each hardware key
// produces at each shift level. This file parses enough of that text —
// the xkb_keycodes and xkb_symbols sections — to turn a hardware key event
// into a keysym, and then a keysym name into either a printable rune or a
// toolkit key name.
//
// The mapping is deliberately minimal but functional: it covers the ASCII
// letters and digits, the common punctuation keysym names, the navigation
// and editing keys, and the modifier keys. Symbols outside that set yield
// no rune (the key is silently ignored), which is the documented, honest
// scope of this sovereign parser.

// Key is the resolved meaning of a hardware key at a given shift level.
type Key struct {
	// Name is a toolkit key name for a non-character key ("Enter",
	// "ArrowLeft", ...), or "" for a character / modifier key.
	Name string
	// Rune is the committed character for a printable key; valid only when
	// HasRune is true.
	Rune rune
	// HasRune reports whether Rune is meaningful.
	HasRune bool
	// IsModifier reports a modifier key (Shift/Control/Alt/...); such keys
	// deliver no toolkit event.
	IsModifier bool
}

// Keymap is a parsed xkb keymap: the per-keycode list of level keysym names
// (group 1 only), keyed by xkb keycode.
type Keymap struct {
	codeSyms map[uint32][]string
}

// evdevOffset is the constant added to a Linux evdev keycode to get the xkb
// keycode the keymap's xkb_keycodes section is written in.
const evdevOffset = 8

var (
	reLineComment  = regexp.MustCompile(`//[^\n]*`)
	reBlockComment = regexp.MustCompile(`(?s)/\*.*?\*/`)
	reKeycode      = regexp.MustCompile(`<([^>]+)>\s*=\s*(\d+)`)
	reKeyStmt      = regexp.MustCompile(`(?s)key\s*<([^>]+)>\s*\{([^}]*)\}`)
	reBrackets     = regexp.MustCompile(`\[([^\]]*)\]`)
	reGroupIdx     = regexp.MustCompile(`^Group\d+$`)
)

// ParseKeymap parses an xkb_v1 keymap document. An empty or unparsable
// document yields a Keymap that resolves every key to nothing (safe: keys
// simply produce no events), so a compositor sending a keymap this minimal
// parser does not understand degrades gracefully rather than crashing.
func ParseKeymap(text string) *Keymap {
	km := &Keymap{codeSyms: map[uint32][]string{}}
	clean := reBlockComment.ReplaceAllString(reLineComment.ReplaceAllString(text, ""), "")

	names := map[string]uint32{} // xkb key name -> xkb keycode
	for _, m := range reKeycode.FindAllStringSubmatch(sectionBody(clean, "xkb_keycodes"), -1) {
		if code, err := strconv.ParseUint(m[2], 10, 32); err == nil {
			names[m[1]] = uint32(code)
		}
	}

	symbols := sectionBody(clean, "xkb_symbols")
	for _, m := range reKeyStmt.FindAllStringSubmatch(symbols, -1) {
		keyName, body := m[1], m[2]
		levels := firstSymbolList(body)
		if levels == nil {
			continue
		}
		if code, ok := names[keyName]; ok {
			km.codeSyms[code] = levels
		}
	}
	return km
}

// sectionBody returns the brace-delimited body that follows the first
// occurrence of keyword, matching nested braces. It returns "" if the
// keyword or its opening brace is absent.
func sectionBody(text, keyword string) string {
	i := strings.Index(text, keyword)
	if i < 0 {
		return ""
	}
	rel := strings.IndexByte(text[i:], '{')
	if rel < 0 {
		return ""
	}
	start := i + rel + 1
	depth := 1
	for k := start; k < len(text); k++ {
		switch text[k] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return text[start:k]
			}
		}
	}
	return text[start:]
}

// firstSymbolList extracts the first bracketed list in a key body that is a
// real symbol list (skipping index brackets like "[Group1]"), returning the
// trimmed, comma-split level names. It returns nil if none is present.
func firstSymbolList(body string) []string {
	for _, b := range reBrackets.FindAllStringSubmatch(body, -1) {
		content := strings.TrimSpace(b[1])
		if content == "" || reGroupIdx.MatchString(content) {
			continue
		}
		parts := strings.Split(content, ",")
		out := make([]string, 0, len(parts))
		for _, p := range parts {
			out = append(out, strings.TrimSpace(p))
		}
		return out
	}
	return nil
}

// Lookup resolves an evdev keycode at the given shift level into a Key. An
// unknown keycode or empty level yields the zero Key (nothing to deliver).
func (km *Keymap) Lookup(evdevCode uint32, shift bool) Key {
	levels := km.codeSyms[evdevCode+evdevOffset]
	if len(levels) == 0 {
		return Key{}
	}
	level := 0
	if shift && len(levels) > 1 {
		level = 1
	}
	return resolveKeysym(levels[level])
}

// resolveKeysym maps an xkb keysym name to its Key meaning.
func resolveKeysym(name string) Key {
	if modifierKeysyms[name] {
		return Key{IsModifier: true}
	}
	if tk, ok := namedKeysyms[name]; ok {
		return Key{Name: tk}
	}
	if r, ok := punctKeysyms[name]; ok {
		return Key{Rune: r, HasRune: true}
	}
	if len(name) == 1 {
		c := name[0]
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') {
			return Key{Rune: rune(c), HasRune: true}
		}
	}
	return Key{}
}

// namedKeysyms maps non-character xkb keysym names to toolkit key names.
var namedKeysyms = map[string]string{
	"Return":    "Enter",
	"KP_Enter":  "Enter",
	"BackSpace": "Backspace",
	"Tab":       "Tab",
	"ISO_Left_Tab": "Tab",
	"Escape":    "Escape",
	"Delete":    "Delete",
	"Insert":    "Insert",
	"Left":      "ArrowLeft",
	"Right":     "ArrowRight",
	"Up":        "ArrowUp",
	"Down":      "ArrowDown",
	"Home":      "Home",
	"End":       "End",
	"Prior":     "PageUp",
	"Next":      "PageDown",
}

// modifierKeysyms is the set of modifier key names, which deliver no event.
var modifierKeysyms = map[string]bool{
	"Shift_L": true, "Shift_R": true,
	"Control_L": true, "Control_R": true,
	"Alt_L": true, "Alt_R": true,
	"Super_L": true, "Super_R": true,
	"Meta_L": true, "Meta_R": true,
	"Hyper_L": true, "Hyper_R": true,
	"Caps_Lock": true, "Num_Lock": true, "Shift_Lock": true,
	"ISO_Level3_Shift": true, "ISO_Level5_Shift": true,
	"Mode_switch": true,
}

// punctKeysyms maps symbolic punctuation/space keysym names to their runes.
var punctKeysyms = map[string]rune{
	"space":        ' ',
	"exclam":       '!',
	"quotedbl":     '"',
	"numbersign":   '#',
	"dollar":       '$',
	"percent":      '%',
	"ampersand":    '&',
	"apostrophe":   '\'',
	"parenleft":    '(',
	"parenright":   ')',
	"asterisk":     '*',
	"plus":         '+',
	"comma":        ',',
	"minus":        '-',
	"period":       '.',
	"slash":        '/',
	"colon":        ':',
	"semicolon":    ';',
	"less":         '<',
	"equal":        '=',
	"greater":      '>',
	"question":     '?',
	"at":           '@',
	"bracketleft":  '[',
	"backslash":    '\\',
	"bracketright": ']',
	"asciicircum":  '^',
	"underscore":   '_',
	"grave":        '`',
	"braceleft":    '{',
	"bar":          '|',
	"braceright":   '}',
	"asciitilde":   '~',
}
