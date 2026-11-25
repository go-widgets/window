// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// Package nativemenu holds the part of a native control's context menu that is
// the same on every host and has no reason to need a window to be tested.
package nativemenu

import "github.com/go-widgets/toolkit"

// Same reports whether two menus read the same: the words, and whether each
// verb applies. The handlers are closures the application rebuilds every frame,
// so comparing them would say "changed" forever, and rebuilding an open menu
// under the pointer is a way to make somebody click the wrong verb.
//
// The caller keeps the NEWEST items for dispatch even when Same is true, so a
// pick always runs the current closure.
func Same(a, b []toolkit.NativeMenuItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].Label != b[i].Label || (a[i].Pick == nil) != (b[i].Pick == nil) {
			return false
		}
	}
	return true
}

// IsSeparator reports whether the item is a divider: an empty label.
func IsSeparator(it toolkit.NativeMenuItem) bool { return it.Label == "" }

// Enabled reports whether picking the item does anything.
func Enabled(it toolkit.NativeMenuItem) bool { return !IsSeparator(it) && it.Pick != nil }
