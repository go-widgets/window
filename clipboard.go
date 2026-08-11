// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

// Clipboard is an optional [Backend] capability: the host OS text clipboard.
//
// Copy and paste inside a go-widgets app already work through the toolkit's own
// in-process clipboard. What that cannot do is carry text ACROSS applications —
// paste a URL from a browser into a text field, or copy an article's title out
// to somewhere else — because the OS pasteboard is a platform facility and the
// toolkit is deliberately platform-free.
//
// A back-end that can reach the pasteboard implements this. Its method set is
// deliberately identical to toolkit.Clipboard, so an app installs it in one
// line and every text widget's copy/cut/paste starts going through the real OS
// pasteboard:
//
//	w, err := window.Open(cfg)
//	...
//	if c, ok := w.(window.Clipboard); ok {
//		toolkit.SetClipboard(c)
//	}
//
// That line is the app's to write, not Open's: reaching into a package-level
// toolkit setting is a decision an app makes, not a side effect a constructor
// should have. A back-end that cannot reach a pasteboard simply does not
// implement this, the assertion fails, and the toolkit's in-process clipboard
// stays in place — copy/paste still works within the app.
//
// Implemented today by the macOS (Cocoa) back-end, through NSPasteboard.
type Clipboard interface {
	// ClipboardText returns the pasteboard's plain-text contents, or "" when it
	// holds no text (an image, a file promise, or nothing at all).
	ClipboardText() string
	// SetClipboardText replaces the pasteboard's contents with text.
	SetClipboardText(text string)
}
