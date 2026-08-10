// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// The COM half of the Windows accessibility bridge.
//
// The window presents ONE HWND holding a rasterised widget tree. To UI
// Automation that is an opaque rectangle with no structure, so without this
// every go-widgets application is unreadable and unnavigable to Narrator, NVDA
// and everything else. macOS subclasses a view and Linux exports D-Bus objects;
// Windows answers WM_GETOBJECT with a COM object implementing a small family of
// UI Automation interfaces.
//
// Nothing is asked of the application: Run already receives the widget root and
// the toolkit already knows how to describe a widget, so the tree is derived
// from what is on screen.
//
// # COM objects without cgo
//
// A COM object is a pointer to a pointer to an array of function pointers, and
// Go can build exactly that: syscall.NewCallback turns a Go func into a
// C-callable pointer, and a run of uintptr at the front of a struct gives one
// embedded vtable pointer per interface. A method recovers its provider by
// subtracting its own vtable's offset from `this`, which is why those offsets
// are named constants rather than literals.
//
// The vtables are built ONCE and shared: NewCallback draws from a process-wide
// table with a hard limit, so building them per element would exhaust it.
//
// Providers are never freed. AddRef/Release keep a count nothing acts on and
// the providers stay in a Go slice for the life of the process: UI Automation
// may hold a reference across frames, and a client returning to an element
// after the tree changed must find a live object rather than freed memory.
//
//go:build windows

package win32

import (
	"sync"
	"syscall"
	"unsafe"

	"github.com/go-widgets/toolkit"
)

var (
	uiaCore                 = syscall.NewLazyDLL("UIAutomationCore.dll")
	oleaut32                = syscall.NewLazyDLL("oleaut32.dll")
	procUiaReturn           = uiaCore.NewProc("UiaReturnRawElementProvider")
	procUiaHostFromHwnd     = uiaCore.NewProc("UiaHostProviderFromHwnd")
	procSysAllocString      = oleaut32.NewProc("SysAllocString")
	procSafeArrayCreateVec  = oleaut32.NewProc("SafeArrayCreateVector")
	procSafeArrayPutElement = oleaut32.NewProc("SafeArrayPutElement")
	procClientToScreen      = user32.NewProc("ClientToScreen")
)

const (
	wmGetObject = 0x003D
	wmMove      = 0x0003
	// UiaRootObjectId is negative; WM_GETOBJECT delivers it in lParam, so the
	// comparison sign-extends before testing.
	uiaRootObjectID = -25

	sOK      = 0
	eNoIface = 0x80004002
	ePointer = 0x80004003
	eNotImpl = 0x80004001
	eFail    = 0x80004005

	vtEmpty   = 0
	vtI4      = 3
	vtBSTR    = 8
	vtBool    = 11
	variantTr = 0xFFFF // VARIANT_TRUE is -1 as an int16

	providerOptionsServerSide = 1

	// Property ids. These are the DOCUMENTED values, checked against what a
	// live client actually requests: an earlier table had IsControlElement at
	// 30010 — which is IsEnabled — and IsEnabled at 30019, which is
	// IsPassword. A provider answering those tells the client every element is
	// a password field.
	propBoundingRect     = 30001
	propControlType      = 30003
	propName             = 30005
	propIsEnabled        = 30010
	propAutomationID     = 30011
	propIsControlElement = 30016
	propIsContentElement = 30017
	propNativeWindow     = 30020
	propIsOffscreen      = 30022
	propValueValue       = 30045

	patternInvoke = 10000

	navParent      = 0
	navNextSibling = 1
	navPrevSibling = 2
	navFirstChild  = 3
	navLastChild   = 4

	uiaAppendRuntimeID = 3

	offSimple   = 0
	offFragment = 8
	offRoot     = 16
	offInvoke   = 24
)

// ptr converts a pointer that arrived from OUTSIDE Go — a COM `this`, or an
// out-parameter the caller allocated — back into one Go can dereference.
//
// go vet flags every uintptr-to-unsafe.Pointer conversion, rightly: if the
// integer named a Go heap object the collector could move or free it while only
// an integer referred to it. Neither hazard applies here. Out-parameters point
// at memory the CALLER owns, outside the Go heap; and the only Go objects whose
// addresses cross into COM are the providers, rooted in axKeep for the life of
// the process precisely so their addresses stay valid as long as a client holds
// them.
func ptr(u uintptr) unsafe.Pointer { return *(*unsafe.Pointer)(unsafe.Pointer(&u)) }

type uiaRect struct{ left, top, width, height float64 }

// variant is the 24-byte VARIANT of the 64-bit ABI: the tag, three reserved
// words and a 16-byte union.
type variant struct {
	vt         uint16
	r1, r2, r3 uint16
	val        [2]uintptr
}

func (v *variant) setBSTR(s string) {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		v.vt = vtEmpty
		return
	}
	b, _, _ := procSysAllocString.Call(uintptr(unsafe.Pointer(p)))
	v.vt, v.val[0] = vtBSTR, b
}

func (v *variant) setI4(n int32) { v.vt, v.val[0] = vtI4, uintptr(uint32(n)) }

func (v *variant) setBool(b bool) {
	v.vt = vtBool
	if b {
		v.val[0] = variantTr
	}
}

// provider is one accessible element: the fragment root when idx is rootIdx,
// otherwise the element at that index of the published snapshot.
//
// The four vtable pointers MUST stay first and in this order: their offsets are
// the constants above, and COM identifies an interface purely by which of them
// the client was handed.
type provider struct {
	vtblSimple   uintptr
	vtblFragment uintptr
	vtblRoot     uintptr
	vtblInvoke   uintptr

	refs int32
	idx  int32
}

const rootIdx = -1

var (
	axmu     sync.Mutex
	axNodes  []toolkit.A11yNode // the published snapshot
	axHWND   uintptr
	axOrigin [2]int // client-area top-left in screen pixels, recorded on the UI thread
	axScale  float64
	axProvs  = map[int32]*provider{}
	axKeep   []*provider
	axVtabls struct{ simple, fragment, root, invoke *[16]uintptr }
	axOnce   sync.Once
)

func axProvider(idx int32) *provider {
	axmu.Lock()
	defer axmu.Unlock()
	if p, ok := axProvs[idx]; ok {
		return p
	}
	p := &provider{
		vtblSimple:   uintptr(unsafe.Pointer(axVtabls.simple)),
		vtblFragment: uintptr(unsafe.Pointer(axVtabls.fragment)),
		vtblRoot:     uintptr(unsafe.Pointer(axVtabls.root)),
		vtblInvoke:   uintptr(unsafe.Pointer(axVtabls.invoke)),
		idx:          idx,
	}
	axProvs[idx] = p
	axKeep = append(axKeep, p)
	return p
}

func (p *provider) node() (toolkit.A11yNode, bool) {
	axmu.Lock()
	defer axmu.Unlock()
	if p.idx < 0 || int(p.idx) >= len(axNodes) {
		return toolkit.A11yNode{}, false
	}
	return axNodes[p.idx], true
}

func axCount() int {
	axmu.Lock()
	defer axmu.Unlock()
	return len(axNodes)
}

func fromSimple(this uintptr) *provider   { return (*provider)(ptr(this - offSimple)) }
func fromFragment(this uintptr) *provider { return (*provider)(ptr(this - offFragment)) }
func fromRoot(this uintptr) *provider     { return (*provider)(ptr(this - offRoot)) }
func fromInvoke(this uintptr) *provider   { return (*provider)(ptr(this - offInvoke)) }

// noteWindowOrigin records where the client area sits on screen. Only the UI
// thread may ask the window; UI Automation calls arrive on its own threads, and
// querying the window from one of them is exactly what made every child
// element's rectangle unreadable in the first implementation of this bridge.
func (w *Window) noteWindowOrigin() {
	var pt struct{ x, y int32 }
	if r, _, _ := procClientToScreen.Call(w.hwnd, uintptr(unsafe.Pointer(&pt))); r == 0 {
		return
	}
	axmu.Lock()
	axOrigin = [2]int{int(pt.x), int(pt.y)}
	axScale = w.scale
	axmu.Unlock()
}

// refreshA11y republishes the tree for the frame just drawn.
func (w *Window) refreshA11y() {
	if w == nil || w.root == nil {
		return
	}
	nodes := A11yNodes(w.root)
	axmu.Lock()
	axNodes = nodes
	if axHWND == 0 {
		axHWND = w.hwnd
	}
	axmu.Unlock()
}

// a11yGetObject answers WM_GETOBJECT.
//
// Only UiaRootObjectId means "give me your UI Automation tree"; every other
// object id on this message belongs to MSAA and must fall through to the
// default handling. The returned value is neither an HRESULT nor the provider:
// it is the LRESULT UiaReturnRawElementProvider marshals, and returning
// anything else leaves the window silently unreadable.
func (w *Window) a11yGetObject(wParam, lParam uintptr) (uintptr, bool) {
	if int32(lParam) != uiaRootObjectID || w.root == nil {
		return 0, false
	}
	axOnce.Do(buildVtables)
	axmu.Lock()
	if axHWND == 0 {
		axHWND = w.hwnd
	}
	axmu.Unlock()
	root := axProvider(rootIdx)
	ret, _, _ := procUiaReturn.Call(w.hwnd, wParam, lParam,
		uintptr(unsafe.Pointer(root))+offSimple)
	return ret, true
}
