// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// The AppKit half of the macOS accessibility bridge.
//
// The window presents ONE view holding a rasterised widget tree. To the
// accessibility system that is a single opaque rectangle: there are no child
// NSViews to inspect, so without this every go-widgets application is one
// unlabelled image and a VoiceOver user cannot find a single control. AppKit's
// answer for exactly this case is NSAccessibilityElement — a concrete class for
// elements that exist for the accessibility tree and nowhere else.
//
// Nothing is asked of the application. window.Run already receives the widget
// root, and the toolkit already knows how to describe a widget, so the tree is
// derived from what is on screen: any go-widgets program is readable and
// operable by a screen reader without writing a line for it.
//
// # Three non-obvious requirements
//
// Each was found by measuring a live process against an external accessibility
// client, and the obvious version of each is silently wrong:
//
//  1. The view must report isAccessibilityElement = YES. The instinct is NO —
//     it is a container, not a control — and that prunes the view AND its
//     synthetic children from the tree entirely.
//  2. Children must be PUSHED with -setAccessibilityChildren:, not returned
//     from an -accessibilityChildren override. AppKit does call the override,
//     repeatedly, and the array never reaches the tree.
//  3. Elements must be built with alloc/init and one-argument setters, never
//     +accessibilityElementWithRole:frame:label:parent:. That factory takes an
//     NSRect BETWEEN two object pointers, and a by-value struct mid-arglist
//     shifts every later argument through purego: the elements arrive with no
//     role and no label.
//
//go:build darwin

package cocoa

import (
	"sync"
	"time"

	objc "github.com/go-macos/objc"
)

var (
	selSetAccessibilityElement  = objc.RegisterName("setAccessibilityElement:")
	selSetAccessibilityRole     = objc.RegisterName("setAccessibilityRole:")
	selSetAccessibilityLabel    = objc.RegisterName("setAccessibilityLabel:")
	selSetAccessibilityValue    = objc.RegisterName("setAccessibilityValue:")
	selSetAccessibilityFrame    = objc.RegisterName("setAccessibilityFrame:")
	selSetAccessibilityParent   = objc.RegisterName("setAccessibilityParent:")
	selSetAccessibilityChildren = objc.RegisterName("setAccessibilityChildren:")
	selSetAccessibilityIdent    = objc.RegisterName("setAccessibilityIdentifier:")
	selAccessibilityIdent       = objc.RegisterName("accessibilityIdentifier")
	selArray                    = objc.RegisterName("array")
	selAddObject                = objc.RegisterName("addObject:")
	selConvertRectToView        = objc.RegisterName("convertRect:toView:")
	selConvertRectToScreen      = objc.RegisterName("convertRectToScreen:")
	selWindowOfView             = objc.RegisterName("window")
)

// a11yMethods are the accessibility overrides added to the view class, kept
// apart from the presentation and input methods so registerClasses reads as
// three distinct jobs: showing pixels, receiving input, and describing both.
func a11yMethods() []objc.MethodDef {
	return []objc.MethodDef{
		{Cmd: objc.RegisterName("isAccessibilityElement"), Fn: viewIsAccessibilityElement},
		{Cmd: objc.RegisterName("accessibilityRole"), Fn: viewAccessibilityRole},
	}
}

// viewIsAccessibilityElement reports YES — see requirement 1 in the file
// comment. Answering NO is the intuitive choice and empties the tree.
func viewIsAccessibilityElement(_ objc.ID, _ objc.SEL) bool { return true }

// viewAccessibilityRole reports the view as a group: the role AppKit uses for
// something whose meaning is its contents.
func viewAccessibilityRole(_ objc.ID, _ objc.SEL) objc.ID { return objc.NSString("AXGroup") }

// a11yElementClass is a subclass of NSAccessibilityElement that can be pressed.
// A plain NSAccessibilityElement is readable and inert: VoiceOver announces
// "Settings, button" and then has no way to press it, which is half an
// interface.
var (
	a11yElementClass objc.Class
	a11yElementOnce  sync.Once
	a11yElementErr   error
)

func registerA11yElementClass() error {
	a11yElementOnce.Do(func() {
		a11yElementClass, a11yElementErr = objc.RegisterClass(
			"GoWidgetsA11yElement", objc.GetClass("NSAccessibilityElement"),
			[]objc.MethodDef{
				{Cmd: objc.RegisterName("accessibilityPerformPress"), Fn: elementPerformPress},
				{Cmd: objc.RegisterName("isAccessibilityEnabled"), Fn: elementIsEnabled},
			})
	})
	return a11yElementErr
}

// elementIsEnabled reports the element as enabled; a disabled one is announced
// as unavailable and cannot be pressed.
func elementIsEnabled(_ objc.ID, _ objc.SEL) bool { return true }

// elementPerformPress activates the element by replaying an ordinary click at
// its centre, through the SAME path a real click takes.
//
// Routing it through the input path rather than into a parallel "activate" API
// is what keeps the two in step: every behaviour a click has is had by a press,
// for free and forever, with no second implementation to drift.
func elementPerformPress(self objc.ID, _ objc.SEL) bool {
	return PerformPressAt(objc.GoString(self.Send(selAccessibilityIdent)))
}

// PerformPressAt is the press itself, split from the Objective-C entry point so
// it can be exercised without AppKit: instantiating an AppKit object off the
// main thread aborts the process, so the only way to test this path is to call
// it with the identifier a real element would carry.
func PerformPressAt(ident string) bool {
	w := active
	if w == nil || w.root == nil {
		return false
	}
	x, y, ok := ParsePressPoint(ident)
	if !ok {
		return false
	}
	w.dispatch(MapMouseDown(x, y, Mods{}))
	w.dispatch(MapMouseUp(x, y, Mods{}))
	return true
}

// refreshA11y republishes the accessibility tree for the frame on screen.
//
// It is driven from the same place that presents pixels, so the description
// never lags what a sighted user sees.
func (w *Window) refreshA11y(force bool) {
	if w == nil || w.view == 0 || w.root == nil {
		return
	}
	if registerA11yElementClass() != nil {
		// Without the pressable element class the tree would be readable and
		// inert. Publishing nothing is the honest state; publishing half an
		// interface is not.
		return
	}
	// Publishing the tree is thousands of ObjC round-trips (per node, main
	// thread), which floods the accessibility server when the window repaints
	// continuously. Compute a cheap Go-side signature of the tree (roles + names
	// + rects — no Cocoa calls); a11yShouldPublish then (a) skips an UNCHANGED
	// tree — a loading spinner leaves it identical — and (b) THROTTLES a changing
	// one — a scroll moves every element's frame every paint — to a11yMinInterval,
	// so the rebuild runs a few times a second instead of at 60 fps. force (a
	// resize) and the first publish always go through.
	sig := a11yTreeSig(A11yNodes(w.root))
	now := time.Now()
	if !a11yShouldPublish(force, w.a11yShown, sig, w.lastA11ySig, now.Sub(w.lastA11yTime), a11yMinInterval) {
		return
	}
	w.a11yShown = true
	w.lastA11ySig = sig
	w.lastA11yTime = now
	w.view.Send(selSetAccessibilityElement, true)
	w.view.Send(selSetAccessibilityRole, objc.NSString("AXGroup"))
	w.view.Send(selSetAccessibilityLabel, objc.NSString(w.title))
	if arr := w.buildA11yChildren(); arr != 0 {
		w.view.Send(selSetAccessibilityChildren, arr)
	}
}

// buildA11yChildren turns the widget tree into NSAccessibilityElements.
//
// Frames are converted here rather than by the pure layer because the last leg
// needs live Cocoa state: an element's rectangle starts in the framebuffer's
// render pixels with a top-left origin, and NSAccessibilityElement wants screen
// points with a bottom-left origin. The trip is render px → view points
// (A11yFrame) → window → screen through the view's own conversions, so a moved,
// resized or Retina window needs no special case.
func (w *Window) buildA11yChildren() objc.ID {
	w.mu.Lock()
	scale := w.scale
	w.mu.Unlock()

	// Collect into an NSMutableArray one object at a time. The obvious
	// +arrayWithObjects:count: takes a C array, which means handing
	// Objective-C a pointer into Go memory; adding them one by one keeps every
	// pointer on the Objective-C side.
	arr := objc.ID(objc.GetClass("NSMutableArray")).Send(selArray)
	if arr == 0 {
		return 0
	}
	win := w.view.Send(selWindowOfView)
	if win == 0 {
		return 0
	}

	count := 0
	for _, n := range A11yNodes(w.root) {
		x, y, fw, fh := A11yFrame(n, scale)
		frame := nsRect{Origin: nsPoint{X: x, Y: y}, Size: nsSize{W: fw, H: fh}}
		// A nil view means "the window's base coordinate space", which also
		// undoes the flipped view's top-left origin on the way.
		inWindow := objc.Send[nsRect](w.view, selConvertRectToView, frame, objc.ID(0))
		onScreen := objc.Send[nsRect](win, selConvertRectToScreen, inWindow)

		el := objc.ID(a11yElementClass).Send(selAlloc).Send(selInit)
		if el == 0 {
			continue
		}
		el.Send(selSetAccessibilityRole, objc.NSString(AXRole(n.Role)))
		el.Send(selSetAccessibilityLabel, objc.NSString(n.Name))
		el.Send(selSetAccessibilityFrame, onScreen)
		el.Send(selSetAccessibilityParent, w.view)
		el.Send(selSetAccessibilityIdent, objc.NSString(PressPoint(n)))
		if n.Value != "" {
			el.Send(selSetAccessibilityValue, objc.NSString(n.Value))
		}
		arr.Send(selAddObject, el)
		count++
	}
	if count == 0 {
		return 0
	}
	return arr
}
