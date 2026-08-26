// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin && integration

// The window server's own account of what it is compositing, bound directly so
// that the level assertion does not go back through AppKit.
package window

import (
	"os"
	"sync"

	"github.com/ebitengine/purego"
	objc "github.com/go-macos/objc"
)

var (
	cgOnce                   sync.Once
	cgCopyWindowInfo         func(option, relativeTo uint32) uintptr
	cgErr                    error
	selObjectForKey          = objc.RegisterName("objectForKey:")
	selIntValue              = objc.RegisterName("intValue")
	selUTF8String            = objc.RegisterName("UTF8String")
	selRespondsToSelectorInt = objc.RegisterName("respondsToSelector:")
)

func loadCG() error {
	cgOnce.Do(func() {
		lib, err := purego.Dlopen(
			"/System/Library/Frameworks/CoreGraphics.framework/CoreGraphics",
			purego.RTLD_LAZY|purego.RTLD_GLOBAL)
		if err != nil {
			cgErr = err
			return
		}
		purego.RegisterLibFunc(&cgCopyWindowInfo, lib, "CGWindowListCopyWindowInfo")
	})
	return cgErr
}

// cgWindowList is CGWindowListCopyWindowInfo. Its CFArray is toll-free bridged
// to NSArray, and every dictionary in it is bridged to NSDictionary, so the
// entries are read with ordinary Objective-C messages.
func cgWindowList(option, relativeTo uint32) uintptr {
	if err := loadCG(); err != nil {
		return 0
	}
	return cgCopyWindowInfo(option, relativeTo)
}

func getpid() int { return os.Getpid() }

// numberFor reads one integer out of a CGWindowList dictionary.
//
// The keys really are the strings their constants are named after --
// kCGWindowOwnerPID is CFSTR("kCGWindowOwnerPID") -- so no symbol has to be
// looked up to address them. A missing key gives -1 rather than a panic,
// because the listing does not promise every key for every window.
func numberFor(dict objc.ID, key string) int {
	v := dict.Send(selObjectForKey, objc.NSString(key))
	if v == 0 {
		return -1
	}
	if v.Send(selRespondsToSelectorInt, selIntValue) == 0 {
		return -1
	}
	return int(v.Send(selIntValue))
}

// stringFor reads one string out of a CGWindowList dictionary, or "".
func stringFor(dict objc.ID, key string) string {
	v := dict.Send(selObjectForKey, objc.NSString(key))
	if v == 0 {
		return ""
	}
	if v.Send(selRespondsToSelectorInt, selUTF8String) == 0 {
		return ""
	}
	return objc.GoString(v)
}
