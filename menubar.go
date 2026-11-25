// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"fmt"

	"github.com/go-widgets/tray"
)

// MenuBar is an optional [Backend] capability: the application's menu bar, built
// from the same [tray.Menu] model a tray icon's menu uses (labels, key
// equivalents, checkmarks, separators, submenus, handlers).
//
//   - macOS: the menu bar at the top of the screen (NSApp.mainMenu). The
//     application menu (About, Hide, Quit) is put first and appName names it.
//
//   - Windows: the window's menu bar (CreateMenu and SetMenu); key equivalents are
//     shown as accelerator text and handled by the window.
//
//   - GTK (the opt-in $GO_WIDGETS_GTK back-end, linux amd64 and arm64): a
//     GtkPopoverMenuBar above the content; key equivalents are window-wide GTK
//     shortcuts (⌘ reads as Control) drawn beside the labels, except an editing
//     role's (Copy, Paste...), which the focused field already handles.
//
// SetMenuBar is safe from any goroutine, and installs the menu again only when it
// changed ([tray.Menu.Signature]); the handlers are always the latest ones.
//
// X11, Wayland, wasmbox and Android have no menu bar this package draws (the
// toolkit has no painted one): [SetMenuBar] answers [ErrNotSupported] for them,
// and the application keeps the same commands reachable another way.
type MenuBar interface {
	SetMenuBar(appName string, m *tray.Menu) error
}

// SetMenuBar installs m as b's menu bar; see [MenuBar]. A back-end without the
// capability answers [ErrNotSupported].
func SetMenuBar(b Backend, appName string, m *tray.Menu) error {
	if mb, ok := b.(MenuBar); ok {
		return mb.SetMenuBar(appName, m)
	}
	return fmt.Errorf("%w: %v has no menu bar", ErrNotSupported, b)
}
