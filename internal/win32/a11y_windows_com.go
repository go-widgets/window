// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// The vtables and interface methods behind the UI Automation provider.
//
// # Two methods that decline, for two different reasons
//
// IRawElementProviderFragmentRoot::ElementProviderFromPoint takes two DOUBLES,
// which arrive in floating-point registers syscall.NewCallback never reads.
// Hit-testing on coordinates it cannot see would point a screen reader at the
// wrong element, so it declines.
//
// IRawElementProviderFragment::get_BoundingRectangle declines too, and that one
// is the outcome of a long measurement rather than a limitation of Go. UI
// Automation never calls it: per-slot counters show Navigate, GetRuntimeId and
// get_FragmentRoot all being exercised while this one is never entered, so the
// provider is never given the chance to answer. Answering ANYWAY publishes
// zeros — the values written never reach the client — and those zeros then
// OVERRIDE the HWND host provider's correct window rectangle, costing the
// window its bounds as well. Declining keeps the window correctly placed and
// reports the children's position as unknown, which is true. Eliminated on the
// way there, each by measurement: four calling conventions for the returned
// UiaRect, the property route, IsOffscreen, ProviderOptions with
// UseComThreading, a real RuntimeId on the root, NativeWindowHandle answered
// versus silent, and UiaRaiseStructureChangedEvent.
//
//go:build windows

package win32

import (
	"syscall"
	"unsafe"
)

// guid is the COM interface identity a client asks for by value.
type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

var (
	iidIUnknown = guid{0x00000000, 0x0000, 0x0000,
		[8]byte{0xC0, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x46}}
	iidSimple = guid{0xD6DD68D1, 0x86FD, 0x4332,
		[8]byte{0x86, 0x66, 0x9A, 0xBE, 0xDE, 0xA2, 0xD2, 0x4C}}
	iidFragment = guid{0xF7063DA8, 0x8359, 0x439C,
		[8]byte{0x92, 0x97, 0xBB, 0xC5, 0x29, 0x9A, 0x7D, 0x87}}
	iidFragmentRoot = guid{0x620CE2A5, 0xAB8F, 0x40A9,
		[8]byte{0x86, 0xCB, 0xDE, 0x3C, 0x75, 0x59, 0x9B, 0x58}}
	iidInvoke = guid{0x54FCB24B, 0xE18E, 0x47A2,
		[8]byte{0xB4, 0xD3, 0xEC, 0xCB, 0xE7, 0x75, 0x99, 0xA2}}
)

func guidEqual(a, b *guid) bool {
	return a.Data1 == b.Data1 && a.Data2 == b.Data2 && a.Data3 == b.Data3 && a.Data4 == b.Data4
}

// queryInterface hands back the embedded vtable pointer for the interface the
// client named. Handing back the wrong one is undetectable to the client: it
// would call through a vtable of a different shape and land in the middle of
// another method, so the offsets here are the whole contract.
func (p *provider) queryInterface(riid, ppv uintptr) uintptr {
	if ppv == 0 {
		return ePointer
	}
	out := (*uintptr)(ptr(ppv))
	id := (*guid)(ptr(riid))
	base := uintptr(unsafe.Pointer(p))
	switch {
	case guidEqual(id, &iidIUnknown), guidEqual(id, &iidSimple):
		*out = base + offSimple
	case guidEqual(id, &iidFragment):
		*out = base + offFragment
	case guidEqual(id, &iidFragmentRoot):
		// Only the root is a fragment root. A child claiming to be one would
		// make the client treat its subtree as a separate window.
		if p.idx != rootIdx {
			*out = 0
			return eNoIface
		}
		*out = base + offRoot
	case guidEqual(id, &iidInvoke):
		if !p.invokable() {
			*out = 0
			return eNoIface
		}
		*out = base + offInvoke
	default:
		*out = 0
		return eNoIface
	}
	p.refs++
	return sOK
}

// invokable reports whether this element can be activated. Only elements the
// toolkit describes as buttons are, which is also what decides whether
// IInvokeProvider is offered at all.
func (p *provider) invokable() bool {
	n, ok := p.node()
	return ok && n.Role == "button"
}

// unknownMethods builds the three IUnknown entries for one embedded vtable.
// Each interface needs its own set, because `this` arrives pointing at that
// interface's slot and only a matching offset recovers the provider.
func unknownMethods(off uintptr) (qi, addref, release uintptr) {
	qi = syscall.NewCallback(func(this, riid, ppv uintptr) uintptr {
		return (*provider)(ptr(this-off)).queryInterface(riid, ppv)
	})
	addref = syscall.NewCallback(func(this uintptr) uintptr {
		p := (*provider)(ptr(this - off))
		p.refs++
		return uintptr(p.refs)
	})
	release = syscall.NewCallback(func(this uintptr) uintptr {
		p := (*provider)(ptr(this - off))
		if p.refs > 0 {
			p.refs--
		}
		return uintptr(p.refs)
	})
	return
}

func buildVtables() {
	simple := new([16]uintptr)
	simple[0], simple[1], simple[2] = unknownMethods(offSimple)
	simple[3] = syscall.NewCallback(func(this, pRetVal uintptr) uintptr {
		if pRetVal == 0 {
			return ePointer
		}
		*(*int32)(ptr(pRetVal)) = providerOptionsServerSide
		return sOK
	})
	simple[4] = syscall.NewCallback(func(this, patternID, ppRetVal uintptr) uintptr {
		if ppRetVal == 0 {
			return ePointer
		}
		out := (*uintptr)(ptr(ppRetVal))
		*out = 0
		p := fromSimple(this)
		if int32(patternID) == patternInvoke && p.invokable() {
			*out = uintptr(unsafe.Pointer(p)) + offInvoke
			p.refs++
		}
		return sOK
	})
	simple[5] = syscall.NewCallback(func(this, propertyID, pRetVal uintptr) uintptr {
		if pRetVal == 0 {
			return ePointer
		}
		return fromSimple(this).propertyValue(int32(propertyID), (*variant)(ptr(pRetVal)))
	})
	simple[6] = syscall.NewCallback(func(this, ppRetVal uintptr) uintptr {
		if ppRetVal == 0 {
			return ePointer
		}
		*(*uintptr)(ptr(ppRetVal)) = 0
		// Only the root has a host: the HWND provider supplies the window-level
		// properties — bounds, focus, the native handle — that a drawn element
		// has no way to answer. Without it UI Automation does not use this
		// provider at all: the tree collapses to one unnamed element.
		if fromSimple(this).idx == rootIdx {
			axmu.Lock()
			hwnd := axHWND
			axmu.Unlock()
			procUiaHostFromHwnd.Call(hwnd, ppRetVal)
		}
		return sOK
	})

	frag := new([16]uintptr)
	frag[0], frag[1], frag[2] = unknownMethods(offFragment)
	frag[3] = syscall.NewCallback(func(this, direction, ppRetVal uintptr) uintptr {
		if ppRetVal == 0 {
			return ePointer
		}
		return fromFragment(this).navigate(int32(direction), (*uintptr)(ptr(ppRetVal)))
	})
	frag[4] = syscall.NewCallback(func(this, ppRetVal uintptr) uintptr {
		if ppRetVal == 0 {
			return ePointer
		}
		return fromFragment(this).runtimeID((*uintptr)(ptr(ppRetVal)))
	})
	frag[5] = syscall.NewCallback(func(this, pRetVal uintptr) uintptr { return eNotImpl })
	frag[6] = syscall.NewCallback(func(this, ppRetVal uintptr) uintptr {
		if ppRetVal != 0 {
			*(*uintptr)(ptr(ppRetVal)) = 0
		}
		return sOK
	})
	frag[7] = syscall.NewCallback(func(this uintptr) uintptr { return sOK })
	frag[8] = syscall.NewCallback(func(this, ppRetVal uintptr) uintptr {
		if ppRetVal == 0 {
			return ePointer
		}
		root := axProvider(rootIdx)
		*(*uintptr)(ptr(ppRetVal)) = uintptr(unsafe.Pointer(root)) + offRoot
		root.refs++
		return sOK
	})

	root := new([16]uintptr)
	root[0], root[1], root[2] = unknownMethods(offRoot)
	root[3] = syscall.NewCallback(func(this, x, y, ppRetVal uintptr) uintptr {
		if ppRetVal != 0 {
			*(*uintptr)(ptr(ppRetVal)) = 0
		}
		return sOK
	})
	root[4] = syscall.NewCallback(func(this, ppRetVal uintptr) uintptr {
		if ppRetVal != 0 {
			*(*uintptr)(ptr(ppRetVal)) = 0
		}
		return sOK
	})

	inv := new([16]uintptr)
	inv[0], inv[1], inv[2] = unknownMethods(offInvoke)
	inv[3] = syscall.NewCallback(func(this uintptr) uintptr {
		return fromInvoke(this).invoke()
	})

	axVtabls.simple, axVtabls.fragment, axVtabls.root, axVtabls.invoke = simple, frag, root, inv
}

// propertyValue answers the properties a client reads to announce an element.
func (p *provider) propertyValue(id int32, v *variant) uintptr {
	*v = variant{}
	if p.idx == rootIdx {
		switch id {
		// Name and ControlType are deliberately NOT answered: the root stands
		// for the window, and leaving them empty lets the HWND host provider
		// supply the real ones. Answering ControlType made every client report
		// the application as a Pane rather than a Window.
		case propIsControlElement, propIsContentElement, propIsEnabled:
			v.setBool(true)
		case propIsOffscreen:
			v.setBool(false)
		}
		return sOK
	}
	n, ok := p.node()
	if !ok {
		return sOK
	}
	switch id {
	case propName:
		v.setBSTR(n.Name)
	case propControlType:
		v.setI4(UIAControlType(n.Role))
	case propValueValue:
		if n.Value != "" {
			v.setBSTR(n.Value)
		}
	case propAutomationID:
		// Carries the element's centre so Invoke can replay a click; see
		// PressPoint for why the point rides on the element itself.
		v.setBSTR(PressPoint(n))
	case propIsControlElement, propIsContentElement, propIsEnabled:
		// Both control and content must be true or the element is invisible to
		// the two views a screen reader actually walks.
		v.setBool(true)
	case propIsOffscreen:
		v.setBool(false)
	case propNativeWindow:
		// A drawn element has no window of its own. Saying so is not the same
		// as staying silent: a client asks this first, to decide whether it can
		// go to the window for what the provider cannot supply.
		v.setI4(0)
	}
	return sOK
}

// navigate walks the flat tree: the root holds every element as a direct child,
// the same shape the macOS and AT-SPI bridges publish.
func (p *provider) navigate(direction int32, out *uintptr) uintptr {
	*out = 0
	n := int32(axCount())
	set := func(idx int32) {
		q := axProvider(idx)
		*out = uintptr(unsafe.Pointer(q)) + offFragment
		q.refs++
	}
	if p.idx == rootIdx {
		switch direction {
		case navFirstChild:
			if n > 0 {
				set(0)
			}
		case navLastChild:
			if n > 0 {
				set(n - 1)
			}
		}
		return sOK
	}
	switch direction {
	case navParent:
		root := axProvider(rootIdx)
		*out = uintptr(unsafe.Pointer(root)) + offFragment
		root.refs++
	case navNextSibling:
		if p.idx+1 < n {
			set(p.idx + 1)
		}
	case navPrevSibling:
		if p.idx > 0 {
			set(p.idx - 1)
		}
	}
	return sOK
}

// runtimeID gives each element an identity a client can compare across calls.
// The root returns nothing, so UI Automation derives its identity from the
// window handle — which is what makes the window and the fragment root the same
// object to a client.
func (p *provider) runtimeID(out *uintptr) uintptr {
	*out = 0
	if p.idx == rootIdx {
		return sOK
	}
	sa, _, _ := procSafeArrayCreateVec.Call(vtI4, 0, 2)
	if sa == 0 {
		return eFail
	}
	vals := [2]int32{uiaAppendRuntimeID, p.idx}
	for i := int32(0); i < 2; i++ {
		idx := i
		procSafeArrayPutElement.Call(sa,
			uintptr(unsafe.Pointer(&idx)), uintptr(unsafe.Pointer(&vals[i])))
	}
	*out = sa
	return sOK
}

// invoke activates the element by replaying an ordinary click at its centre,
// through the SAME path a real click takes.
//
// Routing it through the input path rather than into a parallel "activate" API
// keeps the two in step: every behaviour a click has is had by an invoke, for
// free and forever, with no second implementation to drift.
func (p *provider) invoke() uintptr {
	n, ok := p.node()
	if !ok || active == nil {
		return eFail
	}
	x, y, ok := ParsePressPoint(PressPoint(n))
	if !ok {
		return eFail
	}
	active.dispatch(MapMouseDown(x, y, Mods{}))
	active.dispatch(MapMouseUp(x, y, Mods{}))
	return sOK
}
