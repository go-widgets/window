// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package cocoa

import (
	"fmt"

	"github.com/go-macos/appkit"
	"github.com/go-widgets/toolkit"
)

// This file is the Cocoa backing for toolkit.Native — the platform half of the
// native-control seam. The toolkit widget is OS-neutral and only marks WHERE a
// real control goes and holds its value; here we embed an actual AppKit control
// (from github.com/go-macos/appkit) as a subview OVER the framebuffer view, bind
// its value both ways to the Native's observables, and keep it across frames so
// the person's focus and text survive relayout.
//
// It differs from the Material seam in two ways that matter:
//
//   - A control sits ON TOP of the pixel view (it is interactive and opaque), so
//     there is no hole to punch behind it. The framebuffer's fallback draw for a
//     claimed Native is suppressed by the toolkit, so nothing shows through.
//   - Controls are RECONCILED, not rebuilt. A Material is a passive rectangle
//     rebuilt each layout; a control holds focus, a selection and an insertion
//     point, so syncNative finds the same control again by key and only moves it.

// liveControl is one embedded AppKit control and the observable subscriptions
// that keep it in step with its Native. applying guards the two-way binding: it
// is set while a change flowing FROM the control is written INTO the Native, so
// the Native's notification does not echo back and reset the control mid-edit.
type liveControl struct {
	ctl      *appkit.Control
	unsub    []func()
	applying bool
}

func (lc *liveControl) close() {
	for _, u := range lc.unsub {
		u()
	}
	lc.ctl.Close()
}

// syncNative reconciles the window's embedded AppKit controls with the Natives in
// the current tree. It runs after each frame's layout (bounds are known), so a
// control tracks its Native through scrolling and resizing. With no Natives ever
// present it does nothing, leaving the ordinary path untouched.
func (w *Window) syncNative(root toolkit.Widget) {
	places := toolkit.WalkNative(root)
	if len(places) == 0 && w.nativeControls == nil {
		return
	}
	if w.nativeControls == nil {
		w.nativeControls = make(map[string]*liveControl)
	}

	seen := make(map[string]bool, len(places))
	for _, pl := range places {
		key := nativeKey(pl.Control)
		seen[key] = true
		lc := w.nativeControls[key]
		if lc == nil {
			lc = w.makeControl(pl.Control)
			if lc == nil {
				continue
			}
			w.nativeControls[key] = lc
			pl.Control.Claimed().Set(true)
		}
		r := pl.Rect
		_ = lc.ctl.SetFrame(float64(r.X)/w.scale, float64(r.Y)/w.scale, float64(r.W)/w.scale, float64(r.H)/w.scale)
		_ = lc.ctl.SetHidden(!pl.Visible)
	}
	for key, lc := range w.nativeControls {
		if !seen[key] {
			lc.close()
			delete(w.nativeControls, key)
		}
	}
}

// nativeKey is the identity a control is kept under across frames: the Native's
// own Key when the caller set one, else the widget's address — which is stable in
// this retained-mode toolkit, where the same *Native is laid out frame after
// frame.
func nativeKey(n *toolkit.Native) string {
	if n.Key != "" {
		return n.Key
	}
	return fmt.Sprintf("%p", n)
}

// makeControl builds the AppKit control for a Native, binds it, and adds it over
// the framebuffer view. Returns nil if the control cannot be created (off a real
// AppKit, or an unknown kind).
func (w *Window) makeControl(n *toolkit.Native) *liveControl {
	var (
		ctl *appkit.Control
		err error
	)
	switch n.Kind {
	case toolkit.NativeButton:
		ctl, err = appkit.NewButton(n.Text().Get())
	case toolkit.NativeLabel:
		ctl, err = appkit.NewLabel(n.Text().Get())
	case toolkit.NativeEntry:
		ctl, err = appkit.NewTextField(n.Text().Get())
	case toolkit.NativeSecureEntry:
		ctl, err = appkit.NewSecureTextField(n.Text().Get())
	case toolkit.NativeCheckbox:
		ctl, err = appkit.NewCheckbox(n.Text().Get())
	case toolkit.NativeRadio:
		ctl, err = appkit.NewRadioButton(n.Text().Get())
	case toolkit.NativeSwitch:
		ctl, err = appkit.NewSwitch()
	case toolkit.NativeSlider:
		ctl, err = appkit.NewSlider(n.Min, n.Max, n.Number().Get())
	case toolkit.NativePopUp:
		ctl, err = appkit.NewPopUpButton(n.Items)
	default:
		return nil
	}
	if err != nil || ctl == nil {
		return nil
	}
	lc := &liveControl{ctl: ctl}
	bindControl(n, lc)
	_ = ctl.AddTo(w.view)
	return lc
}

// bindControl wires a control's value to its Native's observables, both ways.
// The toolkit→control direction is a subscription (the app sets the observable,
// the control follows); the control→toolkit direction is the control's own
// change/action callback (the person edits, the model follows). lc.applying
// breaks the echo between them.
func bindControl(n *toolkit.Native, lc *liveControl) {
	ctl := lc.ctl
	pushText := func() func() {
		return n.Text().Subscribe(func(s string) {
			if lc.applying {
				return
			}
			_ = ctl.SetStringValue(s)
		})
	}
	pushBool := func() func() {
		return n.On().Subscribe(func(b bool) {
			if lc.applying {
				return
			}
			_ = ctl.SetBool(b)
		})
	}

	switch n.Kind {
	case toolkit.NativeButton:
		ctl.OnAction(func() { n.Activate() })
	case toolkit.NativeLabel:
		lc.unsub = append(lc.unsub, pushText())
	case toolkit.NativeEntry, toolkit.NativeSecureEntry:
		lc.unsub = append(lc.unsub, pushText())
		ctl.OnChange(func() { withApplying(lc, func() { n.Text().Set(ctl.StringValue()) }) })
		ctl.OnAction(func() {
			withApplying(lc, func() { n.Text().Set(ctl.StringValue()) })
			n.Activate()
		})
	case toolkit.NativeCheckbox, toolkit.NativeRadio, toolkit.NativeSwitch:
		_ = ctl.SetBool(n.On().Get())
		lc.unsub = append(lc.unsub, pushBool())
		ctl.OnAction(func() {
			withApplying(lc, func() { n.On().Set(ctl.Bool()) })
			n.Activate()
		})
	case toolkit.NativeSlider:
		lc.unsub = append(lc.unsub, n.Number().Subscribe(func(v float64) {
			if lc.applying {
				return
			}
			_ = ctl.SetDouble(v)
		}))
		ctl.OnChange(func() { withApplying(lc, func() { n.Number().Set(ctl.Double()) }) })
	case toolkit.NativePopUp:
		_ = ctl.SetStringValue(n.Text().Get())
		lc.unsub = append(lc.unsub, pushText())
		ctl.OnAction(func() {
			withApplying(lc, func() { n.Text().Set(ctl.StringValue()) })
			n.Activate()
		})
	}
}

// withApplying runs fn with lc.applying set, so a value written into the Native
// from its control does not echo back through the subscription and disturb the
// control the person is using.
func withApplying(lc *liveControl, fn func()) {
	lc.applying = true
	fn()
	lc.applying = false
}
