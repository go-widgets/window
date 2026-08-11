// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package cocoa

import "github.com/go-macos/objc"

// nsPasteboardTypeString is the UTI for plain UTF-8 text on NSPasteboard
// (NSPasteboardTypeString == "public.utf8-plain-text").
const nsPasteboardTypeString = "public.utf8-plain-text"

var (
	selGeneralPasteboard = objc.RegisterName("generalPasteboard")
	selClearContents     = objc.RegisterName("clearContents")
	selSetStringForType  = objc.RegisterName("setString:forType:")
	selStringForType     = objc.RegisterName("stringForType:")
)

// generalPasteboard returns +[NSPasteboard generalPasteboard], loading AppKit
// first if nothing else has.
//
// A window opened through window.Open has loaded it long before; but the
// pasteboard is the one part of this back-end that is useful without a window
// on screen, and objc.GetClass on an unloaded framework quietly returns 0 --
// which reads as an empty clipboard rather than as an error, and is exactly how
// the first run of the live proof failed. 0 is still returned when AppKit is
// genuinely unavailable, and both callers treat that as "no clipboard".
func generalPasteboard() objc.ID {
	if err := loadFrameworks(); err != nil {
		return 0
	}
	return objc.ID(objc.GetClass("NSPasteboard")).Send(selGeneralPasteboard)
}

// ClipboardText reads the general pasteboard's plain-text contents, or "" when
// it holds none — stringForType: returns nil for an image or an empty board.
// Implements the window.Clipboard capability.
//
// The receiver is the window because that is what an app holds, but the
// pasteboard itself is per-application and global: NSPasteboard.generalPasteboard
// is the same object whichever window asks.
func (w *Window) ClipboardText() string {
	pb := generalPasteboard()
	if pb == 0 {
		return ""
	}
	s := pb.Send(selStringForType, objc.NSString(nsPasteboardTypeString))
	if s == 0 {
		return ""
	}
	return objc.GoString(s)
}

// SetClipboardText replaces the pasteboard's contents with text as plain UTF-8.
// Implements the window.Clipboard capability.
//
// clearContents comes first and is not optional: NSPasteboard is typed, and
// writing a string without clearing leaves whatever else was on the board — an
// image, a file URL — beside it, so the next reader may get the OLD payload in
// its preferred type.
func (w *Window) SetClipboardText(text string) {
	pb := generalPasteboard()
	if pb == 0 {
		return
	}
	pb.Send(selClearContents)
	pb.Send(selSetStringForType, objc.NSString(text), objc.NSString(nsPasteboardTypeString))
}
