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
	items    []string      // a pop-up's/segmented's item strings
	segments []gtk4.Widget // a segmented control's toggle buttons, in item order
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
	case toolkit.NativeLabel, toolkit.NativeEntry, toolkit.NativeSecureEntry, toolkit.NativeSearch:
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
	case toolkit.NativeStepper:
		if spec.Number != lc.lastNum {
			lc.widget.SetSpinValue(spec.Number)
			lc.lastNum = spec.Number
		}
	case toolkit.NativeProgress:
		if spec.Number != lc.lastNum {
			lc.widget.SetFraction(fraction(spec.Number, spec.Min, spec.Max))
			lc.lastNum = spec.Number
		}
	case toolkit.NativeSpinner:
		if spec.On != lc.lastBool {
			if spec.On {
				lc.widget.Start()
			} else {
				lc.widget.Stop()
			}
			lc.lastBool = spec.On
		}
	case toolkit.NativePopUp:
		// A pop-up's value is the selected item's STRING (spec.Text), matching the
		// cocoa/win32 backends; push it by selecting that item's index.
		if spec.Text != lc.lastText {
			lc.widget.SetSelected(indexOf(lc.items, spec.Text))
			lc.lastText = spec.Text
		}
	case toolkit.NativeCombo:
		if spec.Text != lc.lastText {
			lc.widget.SetComboText(spec.Text)
			lc.lastText = spec.Text
		}
	case toolkit.NativeSegmented:
		if spec.Text != lc.lastText {
			if i := indexOf(lc.items, spec.Text); i < len(lc.segments) {
				lc.segments[i].SetActive(true)
			}
			lc.lastText = spec.Text
		}
	case toolkit.NativeTextView:
		if spec.Text != lc.lastText {
			lc.widget.SetTextViewText(spec.Text)
			lc.lastText = spec.Text
		}
	case toolkit.NativeDate:
		if spec.Text != lc.lastText {
			lc.widget.SetDateISO(spec.Text)
			lc.lastText = spec.Text
		}
	case toolkit.NativeColor:
		if spec.Text != lc.lastText {
			lc.widget.SetColorHex(spec.Text)
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
	case toolkit.NativeSearch:
		lc.widget = gtk4.SearchNew()
		lc.widget.SetText(spec.Text)
		lc.widget.Connect("search-changed", func() {
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
	case toolkit.NativeCombo:
		lc.widget = gtk4.ComboNew(spec.Items)
		lc.widget.SetComboText(spec.Text)
		lc.widget.Connect("changed", func() {
			lc.lastText = lc.widget.ComboText()
			if lc.onText != nil {
				lc.onText(lc.lastText)
			}
		})
	case toolkit.NativeTextView:
		lc.widget = gtk4.TextViewNew()
		lc.widget.SetTextViewText(spec.Text)
		lc.widget.ConnectBufferChanged(func() {
			lc.lastText = lc.widget.TextViewText()
			if lc.onText != nil {
				lc.onText(lc.lastText)
			}
		})
	case toolkit.NativeProgress:
		lc.widget = gtk4.ProgressNew()
		lc.widget.SetFraction(fraction(spec.Number, spec.Min, spec.Max))
	case toolkit.NativeSpinner:
		lc.widget = gtk4.SpinnerNew()
		if spec.On {
			lc.widget.Start()
		}
	case toolkit.NativeStepper:
		step := (spec.Max - spec.Min) / 100
		if step <= 0 {
			step = 1
		}
		lc.widget = gtk4.StepperNew(spec.Min, spec.Max, step)
		lc.widget.SetSpinValue(spec.Number)
		lc.widget.Connect("value-changed", func() {
			lc.lastNum = lc.widget.SpinValue()
			if lc.onNumber != nil {
				lc.onNumber(lc.lastNum)
			}
		})
	case toolkit.NativeLink:
		lc.widget = gtk4.LinkNew(spec.Text)
		lc.widget.OnActivateLink(func() {
			if lc.onActivate != nil {
				lc.onActivate()
			}
		})
	case toolkit.NativeDate:
		lc.widget = gtk4.DateNew()
		lc.widget.SetDateISO(spec.Text)
		lc.widget.Connect("day-selected", func() {
			lc.lastText = lc.widget.DateISO()
			if lc.onText != nil {
				lc.onText(lc.lastText)
			}
		})
	case toolkit.NativeColor:
		lc.widget = gtk4.ColorNew()
		lc.widget.SetColorHex(spec.Text)
		lc.widget.Connect("color-set", func() {
			lc.lastText = lc.widget.ColorHex()
			if lc.onText != nil {
				lc.onText(lc.lastText)
			}
		})
	case toolkit.NativeSegmented:
		// GTK has no segmented control; compose one from linked toggle buttons in a
		// horizontal box (the go-gtk primitives). The selected segment's title is the
		// value, reported like a pop-up's.
		lc.items = spec.Items
		box := gtk4.BoxNew(true)
		var group gtk4.Widget
		for i, item := range spec.Items {
			tb := gtk4.ToggleButtonNewWithLabel(item)
			if i == 0 {
				group = tb
			} else {
				tb.SetGroup(group) // join the first button's radio group
			}
			if item == spec.Text {
				tb.SetActive(true)
			}
			idx := i
			tb.Connect("toggled", func() {
				if tb.Active() { // report only the newly-selected segment
					lc.lastText = lc.items[idx]
					if lc.onText != nil {
						lc.onText(lc.lastText)
					}
				}
			})
			box.Append(tb)
			lc.segments = append(lc.segments, tb)
		}
		lc.widget = box
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
func fraction(v, min, max float64) float64 {
	if max <= min {
		return 0
	}
	f := (v - min) / (max - min)
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

func indexOf(items []string, s string) int {
	for i, it := range items {
		if it == s {
			return i
		}
	}
	return 0
}
