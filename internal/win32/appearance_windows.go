// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows

package win32

import (
	"os"
	"syscall"
	"unsafe"
)

// The Win32 half of the AppearanceReader capability.
//
// Windows keeps both answers in the registry rather than behind an API: the
// light/dark choice under Themes\Personalize, the accent under DWM. Reading
// them is a documented, stable contract that every application on the platform
// uses, and it needs no COM apartment and no message loop -- which matters,
// because an appearance poll must be callable from wherever the application
// happens to be.

const (
	hkeyCurrentUser = 0x80000001
	keyRead         = 0x20019
	regDword        = 4
)

var (
	advapi32           = syscall.NewLazyDLL("advapi32.dll")
	procRegOpenKeyExW  = advapi32.NewProc("RegOpenKeyExW")
	procRegQueryValueW = advapi32.NewProc("RegQueryValueExW")
	procRegCloseKey    = advapi32.NewProc("RegCloseKey")
)

// regDWORD reads one DWORD from HKEY_CURRENT_USER, reporting whether it was
// there at all. Absence is an ordinary answer here: a machine where the user
// never chose a theme simply has no value.
func regDWORD(subkey, name string) (uint32, bool) {
	kp, err := syscall.UTF16PtrFromString(subkey)
	if err != nil {
		return 0, false
	}
	np, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return 0, false
	}
	var h syscall.Handle
	if r, _, _ := procRegOpenKeyExW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(kp)),
		0, keyRead, uintptr(unsafe.Pointer(&h))); r != 0 {
		return 0, false
	}
	defer procRegCloseKey.Call(uintptr(h))

	var typ, val, size uint32 = 0, 0, 4
	if r, _, _ := procRegQueryValueW.Call(uintptr(h), uintptr(unsafe.Pointer(np)), 0,
		uintptr(unsafe.Pointer(&typ)), uintptr(unsafe.Pointer(&val)),
		uintptr(unsafe.Pointer(&size))); r != 0 || typ != regDword {
		return 0, false
	}
	return val, true
}

// AppearanceRaw reports the desktop's look in primitive values: dark mode from
// Themes\Personalize\AppsUseLightTheme, and the accent from DWM\AccentColor.
//
// It hands back plain values rather than the window package's Appearance struct
// because that struct is declared in the package that imports this one; naming
// it here would be a cycle. The parent wraps them (see open_windows.go).
//
// AppsUseLightTheme is INVERTED by name: 1 means light, 0 means dark, and a
// missing value means light -- Windows only writes it once the user has chosen.
// AccentColor is 0xAABBGGRR, which is not the byte order anything else here
// uses; reading it as RGBA silently swaps red and blue, giving a plausible
// wrong colour rather than an obvious one.
func (w *Window) AppearanceRaw() (dark bool, r, g, b uint8, hasAccent bool) {
	if light, ok := regDWORD(`Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`,
		"AppsUseLightTheme"); ok {
		dark = light == 0
	}
	accent, ok := regDWORD(`Software\Microsoft\Windows\DWM`, "AccentColor")
	if !ok {
		return dark, 0, 0, 0, false
	}
	return dark,
		uint8(accent & 0xFF),
		uint8((accent >> 8) & 0xFF),
		uint8((accent >> 16) & 0xFF),
		true
}

// systemFontPath is the Windows UI face. It is a variable so a test can point
// it somewhere else without a real system font.
var systemFontPath = `C:\Windows\Fonts\segoeui.ttf`

// SystemFontTTF returns the bytes of the Windows UI font.
//
// Unlike a Linux desktop, which names a family and leaves finding it to
// fontconfig, Windows ships one documented UI face at a fixed path. This does
// NOT consult SystemParametersInfo for the user's chosen message font, nor the
// font-substitution table: it answers "the system font", which for every
// supported Windows is Segoe UI, and says so here rather than pretending to
// more.
func (w *Window) SystemFontTTF() ([]byte, error) { return os.ReadFile(systemFontPath) }
