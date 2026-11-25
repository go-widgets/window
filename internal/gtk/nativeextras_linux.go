// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux && !android

package gtk

import (
	"bytes"

	"github.com/go-gtk/gtk4"
	"github.com/go-widgets/painter"
	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/window/internal/nativeimg"
	"github.com/go-widgets/window/internal/nativemenu"
)

// nativeExtras is what a control carries beyond its value: the context menu a
// secondary click opens, and the picture on a button. Both are applied only when
// they changed, because the descriptors are rebuilt every frame and tearing a
// popover down under the pointer, or decoding the same PNG sixty times a second,
// is work that also makes the control flicker.
type nativeExtras struct {
	menu      *gtk4.ContextMenu
	menuItems []toolkit.NativeMenuItem // the NEWEST items: a pick runs the current closure
	menuShown []toolkit.NativeMenuItem // the items the popover was last built from

	lastImage []byte
	lastOnly  bool
	pic       gtk4.Picture
}

// applyMenu gives the control the descriptor's context menu.
func (lc *liveControl) applyMenu(items []toolkit.NativeMenuItem) {
	lc.menuItems = items
	if lc.menu == nil {
		if len(items) == 0 {
			return
		}
		lc.menu = gtk4.ContextMenuNew(lc.widget)
		if lc.menu == nil {
			return
		}
	}
	if nativemenu.Same(items, lc.menuShown) && lc.menuShown != nil {
		return
	}
	out := make([]gtk4.MenuItem, len(items))
	for i, it := range items {
		out[i].Label = it.Label
		if !nativemenu.Enabled(it) {
			continue // a separator, or a verb that does nothing: greyed
		}
		idx := i
		out[i].OnPick = func() {
			if idx < len(lc.menuItems) && lc.menuItems[idx].Pick != nil {
				lc.menuItems[idx].Pick()
			}
		}
	}
	lc.menu.SetItems(out)
	lc.menuShown = items
	if len(items) == 0 {
		lc.menuShown = []toolkit.NativeMenuItem{}
	}
}

// destroyExtras releases what a control holds that its box does not.
func (lc *liveControl) destroyExtras() {
	if lc.menu != nil {
		lc.menu.Destroy()
		lc.menu = nil
	}
}

// applyImage puts the descriptor's PNG on a button: the picture alone when
// ImageOnly, else beside the caption. Only a button takes one; the caption stays
// the accessible name and the tooltip when it is not drawn.
func (w *Window) applyImage(lc *liveControl, spec toolkit.NativeControl) {
	if lc.kind != toolkit.NativeButton {
		return
	}
	if bytes.Equal(spec.Image, lc.lastImage) && spec.ImageOnly == lc.lastOnly {
		return
	}
	lc.lastImage, lc.lastOnly = bytes.Clone(spec.Image), spec.ImageOnly
	if len(spec.Image) == 0 {
		lc.widget.SetButtonChild(gtk4.LabelNew(spec.Text))
		lc.widget.SetTooltip("")
		return
	}
	img, err := nativeimg.Decode(spec.Image)
	if err != nil {
		return // keep the caption: an icon that cannot be read is no reason to blank the button
	}
	pic := gtk4.PictureNew()
	if pic == 0 {
		return
	}
	pic.SetRGBA(img.RGBA, img.W, img.H)
	pic.SetCanShrink(true)
	pic.SetAlternativeText(spec.Text)
	fw, fh := painter.IconSize(img.W, img.H, int(w.pt(spec.Rect.W)), int(w.pt(spec.Rect.H)), nativeimg.Pad)
	pic.Widget().SetSizeRequest(fw, fh)
	lc.pic = pic
	if spec.ImageOnly {
		lc.widget.SetButtonChild(pic.Widget())
		lc.widget.SetTooltip(spec.Text)
		return
	}
	row := gtk4.BoxNew(true)
	row.Append(pic.Widget())
	row.Append(gtk4.LabelNew(spec.Text))
	lc.widget.SetButtonChild(row)
	lc.widget.SetTooltip("")
}
