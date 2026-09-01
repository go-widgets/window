// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux && !android

package gtk

import (
	"github.com/go-gtk/gtk4"
	"github.com/go-widgets/toolkit"
)

// liveControl is one embedded GTK widget, the app callbacks for the frame, and
// the last value it reported — the baseline the next descriptor is compared
// against, so a value is pushed into the widget only when the app changed it and
// the person's own edit is never disturbed (the same immediate-mode-safe binding
// as the cocoa and win32 back-ends).
type liveControl struct {
	widget gtk4.Widget
	kind   toolkit.NativeKind

	onText     func(string)
	onBool     func(bool)
	onNumber   func(float64)
	onActivate func()

	lastText string
	lastBool bool
	lastNum  float64
	items    []string // a pop-up's item strings, to map its selected index to text
}

// nativeControlSource is the optional capability a root exposes to supply native
// controls directly — a self-rendering toolkit.Surface. A root that does not is
// walked as a widget tree.
type nativeControlSource interface {
	NativeControls() []toolkit.NativeControl
}

func gatherNative(root toolkit.Widget) []toolkit.NativeControl {
	if p, ok := root.(nativeControlSource); ok {
		return p.NativeControls()
	}
	return toolkit.WalkNative(root)
}

// syncNative reconciles the window's embedded GTK controls with the descriptors
// for the current frame: create new ones in the GtkFixed, move/update existing
// ones, unparent the ones that went away.
func (w *Window) syncNative(root toolkit.Widget) {
	specs := gatherNative(root)
	if len(specs) == 0 && len(w.native) == 0 {
		return
	}
	seen := make(map[string]bool, len(specs))
	for _, spec := range specs {
		if spec.Key == "" {
			continue
		}
		seen[spec.Key] = true
		lc := w.native[spec.Key]
		if lc == nil {
			lc = w.makeControl(spec)
			if lc == nil {
				continue
			}
			w.native[spec.Key] = lc
			if spec.OnClaim != nil {
				spec.OnClaim(true)
			}
		}
		w.applySpec(lc, spec)
	}
	for key, lc := range w.native {
		if !seen[key] {
			lc.widget.Unparent()
			delete(w.native, key)
		}
	}
}

// pt converts a framebuffer-pixel coordinate to a GTK logical point (GtkFixed and
// widget sizes are in points; the descriptors are in render pixels).
func (w *Window) pt(px int) float64 { return float64(px) / w.scale }

// applySpec updates a live control from this frame's descriptor: refresh the
// callbacks, push a value only when it changed, and position + size it.
func (w *Window) applySpec(lc *liveControl, spec toolkit.NativeControl) {
	lc.onText = spec.OnText
	lc.onBool = spec.OnBool
	lc.onNumber = spec.OnNumber
	lc.onActivate = spec.OnActivate

	switch spec.Kind {
	case toolkit.NativeLabel, toolkit.NativeEntry, toolkit.NativeSecureEntry:
		if spec.Text != lc.lastText {
			lc.widget.SetText(spec.Text)
			lc.lastText = spec.Text
		}
	case toolkit.NativeCheckbox, toolkit.NativeRadio, toolkit.NativeSwitch:
		if spec.On != lc.lastBool {
			lc.widget.SetActive(spec.On)
			lc.lastBool = spec.On
		}
	case toolkit.NativeSlider:
		if spec.Number != lc.lastNum {
			lc.widget.SetValue(spec.Number)
			lc.lastNum = spec.Number
		}
	case toolkit.NativePopUp:
		// A pop-up's value is the selected item's STRING (spec.Text), matching the
		// cocoa/win32 backends; push it by selecting that item's index.
		if spec.Text != lc.lastText {
			lc.widget.SetSelected(indexOf(lc.items, spec.Text))
			lc.lastText = spec.Text
		}
	}

	w.fixed.Move(lc.widget, w.pt(spec.Rect.X), w.pt(spec.Rect.Y))
	lc.widget.SetSizeRequest(int(w.pt(spec.Rect.W)), int(w.pt(spec.Rect.H)))
	lc.widget.SetVisible(spec.Visible)
}

// makeControl builds the GTK widget for a descriptor, puts it in the fixed over
// the framebuffer, and wires its signals to dispatch through the liveControl's
// current app callbacks. Returns nil for a kind this back-end does not host.
func (w *Window) makeControl(spec toolkit.NativeControl) *liveControl {
	lc := &liveControl{kind: spec.Kind, lastText: spec.Text, lastBool: spec.On, lastNum: spec.Number}
	switch spec.Kind {
	case toolkit.NativeButton:
		lc.widget = gtk4.ButtonNewWithLabel(spec.Text)
		lc.widget.Connect("clicked", func() {
			if lc.onActivate != nil {
				lc.onActivate()
			}
		})
	case toolkit.NativeLabel:
		lc.widget = gtk4.LabelNew(spec.Text)
	case toolkit.NativeEntry, toolkit.NativeSecureEntry:
		lc.widget = gtk4.EntryNew()
		if spec.Kind == toolkit.NativeSecureEntry {
			lc.widget.SetVisibility(false)
		}
		lc.widget.SetText(spec.Text)
		lc.widget.Connect("changed", func() {
			lc.lastText = lc.widget.Text()
			if lc.onText != nil {
				lc.onText(lc.lastText)
			}
		})
		lc.widget.Connect("activate", func() {
			if lc.onActivate != nil {
				lc.onActivate()
			}
		})
	case toolkit.NativeCheckbox, toolkit.NativeRadio, toolkit.NativeSwitch:
		lc.widget = gtk4.CheckButtonNewWithLabel(spec.Text)
		lc.widget.SetActive(spec.On)
		lc.widget.Connect("toggled", func() {
			lc.lastBool = lc.widget.Active()
			if lc.onBool != nil {
				lc.onBool(lc.lastBool)
			}
			if lc.onActivate != nil {
				lc.onActivate()
			}
		})
	case toolkit.NativeSlider:
		step := (spec.Max - spec.Min) / 100
		if step <= 0 {
			step = 1
		}
		lc.widget = gtk4.SliderNew(spec.Min, spec.Max, step)
		lc.widget.SetValue(spec.Number)
		lc.widget.Connect("value-changed", func() {
			lc.lastNum = lc.widget.Value()
			if lc.onNumber != nil {
				lc.onNumber(lc.lastNum)
			}
		})
	case toolkit.NativePopUp:
		lc.items = spec.Items
		lc.widget = gtk4.PopUpNew(spec.Items)
		lc.widget.SetSelected(indexOf(spec.Items, spec.Text))
		lc.widget.Connect("notify::selected", func() {
			if i := lc.widget.Selected(); i >= 0 && i < len(lc.items) {
				lc.lastText = lc.items[i]
				if lc.onText != nil {
					lc.onText(lc.lastText)
				}
				if lc.onActivate != nil {
					lc.onActivate()
				}
			}
		})
	default:
		return nil
	}
	if lc.widget == 0 {
		return nil
	}
	w.fixed.Put(lc.widget, w.pt(spec.Rect.X), w.pt(spec.Rect.Y))
	return lc
}

// indexOf returns the position of s in items, or 0 when it is absent (GtkDropDown
// selects the first item for an out-of-range index, the same "fall back to the
// head" the cocoa pop-up's string match does).
func indexOf(items []string, s string) int {
	for i, it := range items {
		if it == s {
			return i
		}
	}
	return 0
}
