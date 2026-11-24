// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin && integration

package cocoa

import (
	"os"
	"sync"
	"testing"
	"unsafe"

	"github.com/ebitengine/purego"
	objc "github.com/go-macos/objc"

	"github.com/go-widgets/toolkit"
)

// The window server's own key events. A synthetic NSEvent never reaches the
// keyboard layout engine (AppKit then types the event's characters as given),
// so a dead key can only be proven with an event the HID system delivers.
var (
	cgKeyOnce   sync.Once
	cgKeyErr    error
	cgKeyCreate func(source uintptr, vk uint16, down bool) uintptr
	cgKeyFlags  func(ev uintptr, flags uint64)
	cgKeyPost   func(pid int32, ev uintptr)
	cfRelease   func(ref uintptr)
)

func loadCGKeys() error {
	cgKeyOnce.Do(func() {
		lib, err := purego.Dlopen("/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics",
			purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			cgKeyErr = err
			return
		}
		cf, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation",
			purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			cgKeyErr = err
			return
		}
		purego.RegisterLibFunc(&cgKeyCreate, lib, "CGEventCreateKeyboardEvent")
		purego.RegisterLibFunc(&cgKeyFlags, lib, "CGEventSetFlags")
		purego.RegisterLibFunc(&cgKeyPost, lib, "CGEventPostToPid")
		purego.RegisterLibFunc(&cfRelease, cf, "CFRelease")
	})
	return cgKeyErr
}

const (
	cgFlagOption = 1 << 19
)

// hidKey types one key (press and release) through the window server, and runs
// the main loop so AppKit delivers it.
func hidKey(vk uint16, flags uint64) {
	for _, down := range []bool{true, false} {
		ev := cgKeyCreate(0, vk, down)
		cgKeyFlags(ev, flags)
		cgKeyPost(int32(os.Getpid()), ev) // to this process only: never a stray key in another app
		cfRelease(ev)
		for i := 0; i < 5; i++ {
			spin(0.04)
			active.pumpPending(objc.App())
		}
	}
}

// activeLayoutID is the current keyboard layout's input-source id.
func activeLayoutID() string {
	lib, err := purego.Dlopen("/System/Library/Frameworks/Carbon.framework/Carbon",
		purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	if err != nil {
		return ""
	}
	var cur func() uintptr
	var prop func(src, key uintptr) uintptr
	purego.RegisterLibFunc(&cur, lib, "TISCopyCurrentKeyboardLayoutInputSource")
	purego.RegisterLibFunc(&prop, lib, "TISGetInputSourceProperty")
	key, err := purego.Dlsym(lib, "kTISPropertyInputSourceID")
	if err != nil {
		return ""
	}
	src := cur()
	if src == 0 {
		return ""
	}
	return objc.GoString(objc.ID(prop(src, *(*uintptr)(*(*unsafe.Pointer)(unsafe.Pointer(&key))))))
}

// liveIMEWindow opens a window holding one focused Entry and makes it the key
// window of a frontmost application.
func liveIMEWindow(t *testing.T) (*Window, *toolkit.Entry) {
	t.Helper()
	var (
		win   *Window
		entry *toolkit.Entry
	)
	callOnMain(func() {
		var err error
		if win, err = New("ime proof", 320, 160, toolkit.DefaultDark()); err != nil {
			t.Errorf("open: %v", err)
			return
		}
		entry = toolkit.NewEntry("")
		entry.SetFocused(true)
		vbox := toolkit.NewVBox()
		vbox.Append(entry)
		win.bindAndSeed(vbox)
		spin(0.4)
		objc.App().Send(objc.RegisterName("activateIgnoringOtherApps:"), true)
		win.win.Send(objc.RegisterName("makeKeyAndOrderFront:"), objc.ID(0))
		win.win.Send(objc.RegisterName("makeFirstResponder:"), win.view)
		spin(0.4)
	})
	return win, entry
}

// TestLiveDeadKeyComposesInAPaintedField types a dead key then a vowel through
// the window server into a painted Entry. The dead key must be a preedit that
// the field shows but does not hold (the value stays empty, hasMarkedText is
// true, a candidate rectangle exists); the vowel must commit the accented
// letter into the value. The keys are those of the active layout: option-e then
// e on a US layout (é), the circumflex key then e on a French one (ê).
func TestLiveDeadKeyComposesInAPaintedField(t *testing.T) {
	skipUnlessIntegration(t)
	if err := loadCGKeys(); err != nil {
		t.Skipf("CoreGraphics key synthesis unavailable: %v", err)
	}
	win, entry := liveIMEWindow(t)
	if win == nil {
		return
	}
	layout := activeLayoutID()
	t.Logf("keyboard layout: %s", layout)
	var dead uint16
	var flags uint64
	var want string
	switch layout {
	case "com.apple.keylayout.French", "com.apple.keylayout.ABC-AZERTY":
		dead, flags, want = 33, 0, "ê" // the ^ key
	default:
		dead, flags, want = 14, cgFlagOption, "é" // option-e
	}
	var valueDuring, valueAfter, preedit, plain string
	callOnMain(func() {
		hidKey(dead, flags)
		valueDuring = entry.Text().Get()
		preedit = entry.Preedit().Text
		hidKey(14, 0) // e
		valueAfter = entry.Text().Get()
		hidKey(7, 0) // x: an ordinary key still types through the unchanged path
		plain = entry.Text().Get()
		_ = win.Close()
	})
	if plain != want+"x" {
		t.Errorf("after a plain x the value is %q, want %q", plain, want+"x")
	}
	if valueDuring != "" {
		t.Errorf("after the dead key the value is %q, want empty (a dead key is not text)", valueDuring)
	}
	// The layout engine holds the dead key itself and may or may not show it as
	// marked text; what it must never do is lose it or type it as text.
	t.Logf("preedit shown after the dead key: %q", preedit)
	if valueAfter != want {
		t.Errorf("after the dead key and e the value is %q, want %q", valueAfter, want)
	}
}

// TestLiveMarkedTextThroughTheRuntime drives the NSTextInputClient methods the
// way an input method does, by Objective-C message to the real view: a kana
// preedit with a thick converting clause and a cursor, the candidate rectangle,
// the commit. It proves the registration, the by-value NSRange/NSRect
// arguments and returns, and the attributed-string mapping end to end.
func TestLiveMarkedTextThroughTheRuntime(t *testing.T) {
	skipUnlessIntegration(t)
	win, entry := liveIMEWindow(t)
	if win == nil {
		return
	}
	var (
		p                toolkit.Preedit
		marked           bool
		mr, sr           nsRange
		rect             nsRect
		conforms         bool
		valueDuring, val string
		after            toolkit.Preedit
	)
	callOnMain(func() {
		v := win.view
		conforms = objc.Send[bool](v, objc.RegisterName("conformsToProtocol:"),
			objc.GetProtocol("NSTextInputClient"))
		attr := objc.ID(objc.GetClass("NSMutableAttributedString")).Send(objc.RegisterName("alloc")).
			Send(objc.RegisterName("initWithString:"), objc.NSString("かんじ"))
		two := objc.ID(objc.GetClass("NSNumber")).Send(objc.RegisterName("numberWithInt:"), 2)
		one := objc.ID(objc.GetClass("NSNumber")).Send(objc.RegisterName("numberWithInt:"), 1)
		key := objc.NSString("NSUnderline")
		add := objc.RegisterName("addAttribute:value:range:")
		attr.Send(add, key, one, nsRange{0, 2})
		attr.Send(add, key, two, nsRange{2, 1})
		none := nsRange{nsNotFound, 0}
		v.Send(objc.RegisterName("setMarkedText:selectedRange:replacementRange:"), attr, nsRange{2, 0}, none)
		p = entry.Preedit()
		marked = objc.Send[bool](v, objc.RegisterName("hasMarkedText"))
		mr = objc.Send[nsRange](v, objc.RegisterName("markedRange"))
		sr = objc.Send[nsRange](v, objc.RegisterName("selectedRange"))
		rect = objc.Send[nsRect](v, objc.RegisterName("firstRectForCharacterRange:actualRange:"), nsRange{0, 1}, uintptr(0))
		valueDuring = entry.Text().Get()
		v.Send(objc.RegisterName("insertText:replacementRange:"), objc.NSString("漢字"), none)
		val = entry.Text().Get()
		after = entry.Preedit()
		_ = win.Close()
	})
	if !conforms {
		t.Errorf("the view does not conform to NSTextInputClient")
	}
	if p.Text != "かんじ" || p.Cursor != 2 || len(p.Segments) != 2 ||
		p.Segments[0].Style != toolkit.PreeditConverted || p.Segments[1].Style != toolkit.PreeditTarget {
		t.Errorf("preedit = %+v, want かんじ cursor 2, a converted then a target segment", p)
	}
	if !marked || mr != (nsRange{0, 3}) || sr != (nsRange{2, 0}) {
		t.Errorf("hasMarkedText=%v markedRange=%+v selectedRange=%+v", marked, mr, sr)
	}
	if rect.Size.H <= 0 || rect.Size.W <= 0 {
		t.Errorf("firstRectForCharacterRange = %+v, want a screen rectangle", rect)
	}
	if valueDuring != "" {
		t.Errorf("the preedit leaked into the value: %q", valueDuring)
	}
	if val != "漢字" || after.Text != "" {
		t.Errorf("after the commit value=%q preedit=%q, want 漢字 and none", val, after.Text)
	}
}

// withInputSource selects the input source with the given id for the duration
// of f, restoring the previous one afterwards. It reports false when the source
// cannot be found, enabled or selected without user interaction.
func withInputSource(id string, f func()) bool {
	lib, err := purego.Dlopen("/System/Library/Frameworks/Carbon.framework/Carbon",
		purego.RTLD_LAZY|purego.RTLD_GLOBAL)
	if err != nil {
		return false
	}
	var (
		cur    func() uintptr
		list   func(props uintptr, all bool) uintptr
		enable func(src uintptr) int32
		sel    func(src uintptr) int32
	)
	purego.RegisterLibFunc(&cur, lib, "TISCopyCurrentKeyboardInputSource")
	purego.RegisterLibFunc(&list, lib, "TISCreateInputSourceList")
	purego.RegisterLibFunc(&enable, lib, "TISEnableInputSource")
	purego.RegisterLibFunc(&sel, lib, "TISSelectInputSource")
	keyPtr, err := purego.Dlsym(lib, "kTISPropertyInputSourceID")
	if err != nil {
		return false
	}
	key := objc.ID(*(*uintptr)(*(*unsafe.Pointer)(unsafe.Pointer(&keyPtr))))
	props := objc.ID(objc.GetClass("NSDictionary")).Send(objc.RegisterName("dictionaryWithObject:forKey:"), objc.NSString(id), key)
	arr := objc.ID(list(uintptr(props), true))
	if arr == 0 || objc.Send[uint64](arr, objc.RegisterName("count")) == 0 {
		return false
	}
	target := uintptr(arr.Send(objc.RegisterName("objectAtIndex:"), uint64(0)))
	prev := cur()
	if enable(target) != 0 || sel(target) != 0 {
		return false
	}
	defer sel(prev)
	spin(0.5)
	f()
	return true
}

// TestLiveKanaInputMethodMarksText selects the Japanese Kana input method (when
// it can be enabled without anyone touching System Settings), types one kana
// key through the window server and checks the input method's marked text
// arrived as a preedit, not as text; Return then commits it.
func TestLiveKanaInputMethodMarksText(t *testing.T) {
	skipUnlessIntegration(t)
	if err := loadCGKeys(); err != nil {
		t.Skipf("CoreGraphics key synthesis unavailable: %v", err)
	}
	win, entry := liveIMEWindow(t)
	if win == nil {
		return
	}
	var during, preedit, after string
	ran := false
	callOnMain(func() {
		ran = withInputSource("com.apple.inputmethod.Kotoeri.KanaTyping.Japanese", func() {
			hidKey(0, 0) // the key labelled A: ち in kana typing
			during = entry.Text().Get()
			preedit = entry.Preedit().Text
			hidKey(36, 0) // Return commits
			after = entry.Text().Get()
		})
		_ = win.Close()
	})
	if !ran {
		t.Skip("the Japanese Kana input method cannot be enabled without user interaction here")
	}
	t.Logf("during: value=%q preedit=%q; after Return: value=%q", during, preedit, after)
	if preedit == "" || during != "" {
		t.Errorf("a kana key must be a preedit (value %q preedit %q)", during, preedit)
	}
	if after == "" {
		t.Errorf("Return did not commit the kana")
	}
}
