// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package x11

// Core request opcodes used by this package (X11 protocol, appendix B).
const (
	opCreateWindow           = 1
	opChangeWindowAttributes = 2
	opMapWindow              = 8
	opInternAtom             = 16
	opChangeProperty         = 18
	opCreateGC               = 55
	opFreeGC                 = 60
	opPutImage               = 72
	opGetKeyboardMapping     = 101
	opQueryExtension         = 98
)

// Window class values for CreateWindow.
const (
	classInputOutput = 1
)

// CopyFromParent (0) is used for a CreateWindow depth/visual/border so the
// new window inherits the root's TrueColor visual with no BadMatch risk.
const CopyFromParent = 0

// Window-attribute value-mask bits (CreateWindow / ChangeWindowAttributes).
const (
	cwBackPixel   = 0x00000002
	cwBorderPixel = 0x00000008
	cwEventMask   = 0x00000800
)

// Event-mask bits selected on our window. These are the events the host
// loop translates into toolkit events plus the structure/exposure notifies
// needed to drive relayout and repaint.
const (
	EventMaskKeyPress         = 0x00000001
	EventMaskKeyRelease       = 0x00000002
	EventMaskButtonPress      = 0x00000004
	EventMaskButtonRelease    = 0x00000008
	EventMaskPointerMotion    = 0x00000040
	EventMaskButton1Motion    = 0x00000100
	EventMaskExposure         = 0x00008000
	EventMaskStructureNotify  = 0x00020000
	EventMaskButtonMotionMask = 0x00002000
)

// DefaultEventMask is the mask CreateWindow selects for the host window.
const DefaultEventMask = EventMaskKeyPress | EventMaskKeyRelease |
	EventMaskButtonPress | EventMaskButtonRelease |
	EventMaskPointerMotion | EventMaskButtonMotionMask |
	EventMaskExposure | EventMaskStructureNotify

// PutImage image formats.
const (
	imageFormatZPixmap = 2
)

// ChangeProperty modes.
const (
	propModeReplace = 0
)

// Predefined atoms (X11/Xatom.h). Interned atoms (WM_PROTOCOLS,
// WM_DELETE_WINDOW) are obtained at runtime via InternAtom.
const (
	AtomNone      = 0
	AtomPrimary   = 1
	AtomAtom      = 4
	AtomCardinal  = 6
	AtomString    = 31
	AtomWMName    = 39
	AtomWMClass   = 67
	AtomWMHints   = 35
	AtomWMIconNm  = 37
	AtomWMNormalH = 40
)

// X11 event codes (first byte of a 32-byte event; the high bit marks a
// SendEvent-synthesised event and is masked off before comparison).
const (
	evKeyPress        = 2
	evKeyRelease      = 3
	evButtonPress     = 4
	evButtonRelease   = 5
	evMotionNotify    = 6
	evExpose          = 12
	evMapNotify       = 19
	evReparentNotify  = 21
	evConfigureNotify = 22
	evClientMessage   = 33
)

// Reply/error stream discriminators (first byte of a 32-byte packet).
const (
	pktError = 0
	pktReply = 1
)

// sendEventBit is the high bit set on synthesised events.
const sendEventBit = 0x80
