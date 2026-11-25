// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// Package wincb makes callbacks that native Windows code can call safely.
//
// # Why syscall.NewCallback is not enough on windows/arm64
//
// The Windows ARM64 calling convention makes D8-D15 callee-saved: a function
// that changes them must put them back. Go's entry for a syscall.NewCallback
// function, runtime·callbackasm1 in src/runtime/sys_windows_arm64.s (Go 1.27.4),
// saves R19-R28 and nothing else, while sigtramp, in the same file, saves
// F8-F15 with SAVE_F8_TO_F15. Go code is free to use F8-F15, so a callback can
// return with the CALLER's doubles replaced.
//
// UI Automation's core is such a caller: it keeps the doubles of a bounding
// rectangle in D8-D15 across its call into IRawElementProviderFragment's
// get_BoundingRectangle, and so every rectangle read from a provider written
// in Go reached the client as (0,0 0x0) on ARM64 — go-widgets/window#188. On
// amd64 the runtime saves X6-X15 and nothing is lost.
//
// New returns an entry that saves D8-D15 around the Go callback and restores
// them afterwards; on every other architecture it is syscall.NewCallback.
//
// A wrapped callback takes at most eight arguments, which all arrive in
// registers: the thunk adds a stack frame, so arguments passed on the stack
// would be read from the wrong place.
package wincb
