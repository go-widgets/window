// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package cocoa

import (
	"bytes"
	"github.com/go-macos/appkit"
	"github.com/go-widgets/toolkit"
)

// This file is the Cocoa backing for toolkit's native-control seam. Each frame
// the app describes the native controls it wants — as flat toolkit.NativeControl
// descriptors, from a Surface's Controls field or from WalkNative over a widget
// tree — and this backend holds the real AppKit controls (github.com/go-macos/
// appkit), one per Key, laid over the framebuffer view, reconciling them to the
// descriptors.
//
// Two things make it different from the Material seam. A control sits ON TOP of
// the pixel view — interactive and opaque — so there is no hole to punch. And
// controls are RECONCILED by Key, not rebuilt: a control holds focus, a
// selection and an insertion point, so the same descriptor Key finds the same
// live control across frames and only moves or updates it.
//
// The value binding is immediate-mode-safe by construction: a descriptor's value
// is pushed into a control ONLY when it differs from what the control last
// reported. When the person edits, the change flows out through the descriptor's
// callback and the app's next descriptor carries that same value — equal, so
// nothing is pushed back and the caret is never disturbed. Only a value the app
// changed on its own differs, and only that is pushed.

// liveControl is one embedded AppKit control, the app callbacks for the frame,
// and the last value it reported — the baseline the next descriptor is compared
// against.
type liveControl struct {
	ctl *appkit.Control

	onText     func(string)
	onBool     func(bool)
	onNumber   func(float64)
	onActivate func()

	lastText  string
	lastBool  bool
	lastNum   float64
	lastItems []string
	lastMenu  []toolkit.NativeMenuItem
	lastImage []byte
	lastOnly  bool
}

// sameStrings reports whether two row lists are the same, so a list is only
// reloaded when its contents actually changed. Reloading on every frame would
// throw the selection away sixty times a second.
func sameStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func (lc *liveControl) close() { lc.ctl.Close() }

// nativeControlSource is the optional capability a root exposes to supply native
// controls directly — a self-rendering toolkit.Surface implements it. A root
// that does not is walked as a widget tree instead.
type nativeControlSource interface {
	NativeControls() []toolkit.NativeControl
}

// gatherNative collects this frame's control descriptors: from the root's own
// provider when it has one (a Surface), else by walking it as a widget tree.
func gatherNative(root toolkit.Widget) []toolkit.NativeControl {
	if p, ok := root.(nativeControlSource); ok {
		return p.NativeControls()
	}
	return toolkit.WalkNative(root)
}

// syncNative reconciles the window's embedded AppKit controls with the
// descriptors for the current frame. It runs after layout, so a control tracks
// its descriptor through scrolling and interaction. With no controls ever
// present it does nothing, leaving the ordinary path untouched.
//
// It reports whether it CREATED any control this pass, which the caller needs
// because creating one changes what the framebuffer should contain.
//
// ⛔ A CLAIMED REGION STOPS DRAWING, AND THE OLD PIXELS STAY. A toolkit.Native
// paints its Fallback until a host claims it; the claim happens HERE, after
// the frame that painted the fallback. On an opaque window nothing repainted
// afterwards, so the drawn fallback stayed in the framebuffer with the real
// AppKit control composited on top of it -- two buttons, one over the other,
// their labels a pixel apart. Seen in a settings window converted to native
// controls, and invisible in every unit test, because it is a question about
// pixels that are still there rather than about pixels that are wrong.
func (w *Window) syncNative(root toolkit.Widget) (created bool) {
	specs := gatherNative(root)
	if len(specs) == 0 && w.nativeControls == nil {
		return
	}
	if w.nativeControls == nil {
		w.nativeControls = make(map[string]*liveControl)
	}

	seen := make(map[string]bool, len(specs))
	for _, spec := range specs {
		if spec.Key == "" {
			// A descriptor with no identity cannot be reconciled across frames;
			// a producer must key every control (WalkNative synthesises one).
			continue
		}
		seen[spec.Key] = true
		lc := w.nativeControls[spec.Key]
		if lc == nil {
			lc = w.makeControl(spec)
			if lc == nil {
				continue
			}
			w.nativeControls[spec.Key] = lc
			created = true
			if spec.OnClaim != nil {
				spec.OnClaim(true)
			}
		}
		w.applySpec(lc, spec)
	}
	for key, lc := range w.nativeControls {
		if !seen[key] {
			lc.close()
			delete(w.nativeControls, key)
		}
	}
	return created
}

// applySpec updates a live control from this frame's descriptor: refresh the
// callbacks (their closures may differ each frame), push a value only when it
// changed away from what the control last reported, and reposition it.
func (w *Window) applySpec(lc *liveControl, spec toolkit.NativeControl) {
	lc.onText = spec.OnText
	lc.onBool = spec.OnBool
	lc.onNumber = spec.OnNumber
	lc.onActivate = spec.OnActivate

	switch spec.Kind {
	case toolkit.NativeLabel, toolkit.NativeEntry, toolkit.NativeSecureEntry, toolkit.NativePopUp:
		if spec.Text != lc.lastText {
			_ = lc.ctl.SetStringValue(spec.Text)
			lc.lastText = spec.Text
		}
	case toolkit.NativeCheckbox, toolkit.NativeRadio, toolkit.NativeSwitch:
		if spec.On != lc.lastBool {
			_ = lc.ctl.SetBool(spec.On)
			lc.lastBool = spec.On
		}
	case toolkit.NativeSlider:
		if spec.Number != lc.lastNum {
			_ = lc.ctl.SetDouble(spec.Number)
			lc.lastNum = spec.Number
		}
	case toolkit.NativeList:
		// The ROWS first: a selection is an index into them, so pushing it
		// against the old list would choose the wrong entry -- or none, if the
		// list has grown shorter and the row no longer exists.
		if !sameStrings(spec.Items, lc.lastItems) {
			_ = lc.ctl.SetItems(spec.Items)
			lc.lastItems = append(lc.lastItems[:0], spec.Items...)
			// Replacing them drops the selection, so it is pushed again even
			// when the application has not moved it.
			lc.lastNum = -2
		}
		if spec.Number != lc.lastNum {
			_ = lc.ctl.SetDouble(spec.Number)
			lc.lastNum = spec.Number
		}
	}
	// A Button's title is fixed at creation (NSButton has no stringValue), so it
	// is not pushed here.

	lc.applyMenu(spec.Menu)
	lc.applyImage(spec.Image, spec.ImageOnly)

	r := spec.Rect
	_ = lc.ctl.SetFrame(float64(r.X)/w.scale, float64(r.Y)/w.scale, float64(r.W)/w.scale, float64(r.H)/w.scale)
	_ = lc.ctl.SetHidden(!spec.Visible)
}

// makeControl builds the AppKit control for a descriptor, wires its native
// callbacks to dispatch through the liveControl's current app callbacks, and
// adds it over the framebuffer view.
func (w *Window) makeControl(spec toolkit.NativeControl) *liveControl {
	var (
		ctl *appkit.Control
		err error
	)
	switch spec.Kind {
	case toolkit.NativeButton:
		ctl, err = appkit.NewButton(spec.Text)
	case toolkit.NativeLabel:
		ctl, err = appkit.NewLabel(spec.Text)
	case toolkit.NativeEntry:
		ctl, err = appkit.NewTextField(spec.Text)
	case toolkit.NativeSecureEntry:
		ctl, err = appkit.NewSecureTextField(spec.Text)
	case toolkit.NativeCheckbox:
		ctl, err = appkit.NewCheckbox(spec.Text)
	case toolkit.NativeRadio:
		ctl, err = appkit.NewRadioButton(spec.Text)
	case toolkit.NativeSwitch:
		ctl, err = appkit.NewSwitch()
	case toolkit.NativeSlider:
		ctl, err = appkit.NewSlider(spec.Min, spec.Max, spec.Number)
	case toolkit.NativePopUp:
		ctl, err = appkit.NewPopUpButton(spec.Items)
	case toolkit.NativeList:
		ctl, err = appkit.NewTableView(spec.Items)
	default:
		return nil
	}
	if err != nil || ctl == nil {
		return nil
	}
	lc := &liveControl{
		ctl: ctl, lastText: spec.Text, lastBool: spec.On, lastNum: spec.Number,
		lastItems: append([]string(nil), spec.Items...),
	}

	switch spec.Kind {
	case toolkit.NativeCheckbox, toolkit.NativeRadio, toolkit.NativeSwitch:
		_ = ctl.SetBool(spec.On)
	case toolkit.NativeList:
		_ = ctl.SetDouble(spec.Number)
	case toolkit.NativePopUp:
		if spec.Text != "" {
			_ = ctl.SetStringValue(spec.Text)
		}
	}

	switch spec.Kind {
	case toolkit.NativeEntry, toolkit.NativeSecureEntry:
		ctl.OnChange(func() { lc.reportText() })
		ctl.OnAction(func() { lc.reportText(); lc.activate() })
	case toolkit.NativeButton:
		ctl.OnAction(func() { lc.activate() })
	case toolkit.NativeCheckbox, toolkit.NativeRadio, toolkit.NativeSwitch:
		ctl.OnAction(func() { lc.reportBool(); lc.activate() })
	case toolkit.NativeSlider, toolkit.NativeList:
		ctl.OnChange(func() { lc.reportNumber() })
	case toolkit.NativePopUp:
		ctl.OnAction(func() { lc.reportText(); lc.activate() })
	}

	lc.applyMenu(spec.Menu)
	lc.applyImage(spec.Image, spec.ImageOnly)
	_ = ctl.AddTo(w.view)
	return lc
}

// applyMenu hands the control its context menu, and only when it changed.
//
// Rebuilding a menu on every frame would tear it down while it is open: a
// person holding a menu with the pointer over "Remove" would find it replaced
// under them, which is a way to make somebody click the wrong verb.
func (lc *liveControl) applyMenu(items []toolkit.NativeMenuItem) {
	if sameMenu(items, lc.lastMenu) {
		return
	}
	out := make([]appkit.MenuItem, 0, len(items))
	for _, it := range items {
		out = append(out, appkit.MenuItem{Title: it.Label, OnPick: it.Pick})
	}
	_ = lc.ctl.SetMenu(out)
	lc.lastMenu = items
}

// applyImage puts a picture on the control, and only when it changed.
//
// Decoding a PNG on every frame to hand AppKit the same image sixty times a
// second is work for nothing, and it makes a button flicker as its cell is
// replaced under the pointer.
func (lc *liveControl) applyImage(png []byte, only bool) {
	if bytes.Equal(png, lc.lastImage) && only == lc.lastOnly {
		return
	}
	_ = lc.ctl.SetImage(png)
	_ = lc.ctl.SetImageOnly(only)
	lc.lastImage, lc.lastOnly = png, only
}

// sameMenu reports whether two menus read the same. Only the words and whether
// each verb applies: the handlers are closures the application rebuilds every
// frame, so comparing them would say "changed" forever.
func sameMenu(a, b []toolkit.NativeMenuItem) bool {
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

// reportText/reportBool/reportNumber record the control's new value as the
// baseline (so the next descriptor comparing equal does not push it back) and
// forward it to the app's current callback.
func (lc *liveControl) reportText() {
	lc.lastText = lc.ctl.StringValue()
	if lc.onText != nil {
		lc.onText(lc.lastText)
	}
}
func (lc *liveControl) reportBool() {
	lc.lastBool = lc.ctl.Bool()
	if lc.onBool != nil {
		lc.onBool(lc.lastBool)
	}
}
func (lc *liveControl) reportNumber() {
	lc.lastNum = lc.ctl.Double()
	if lc.onNumber != nil {
		lc.onNumber(lc.lastNum)
	}
}
func (lc *liveControl) activate() {
	if lc.onActivate != nil {
		lc.onActivate()
	}
}
