// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows && arm64

package wincb

import (
	"sync"
	"syscall"
)

//go:generate go run gen_table.go

// Slots is how many callbacks can be made. The provider needs about forty.
const Slots = 256

// entrySize is the byte size of one generated entry: two instructions.
const entrySize = 8

var (
	mu    sync.Mutex
	next  int
	table [Slots]uintptr // the syscall.NewCallback entry each thunk calls
)

// cbEntries is the generated table of entries; cbCommon is the code they share.
// Both are implemented in assembly and only ever called by native code.
func cbEntries()
func cbCommon()
func cbEntriesAddr() uintptr

// New returns a C-callable entry for fn that preserves D8-D15 around it. It
// panics when the Slots are used up, rather than failing later and silently.
func New(fn any) uintptr { return wrap(syscall.NewCallback(fn)) }

// wrap gives target, an entry that follows the C calling convention, a thunk.
func wrap(target uintptr) uintptr {
	mu.Lock()
	defer mu.Unlock()
	if next == Slots {
		panic("wincb: out of callback slots")
	}
	i := next
	next++
	table[i] = target
	return cbEntriesAddr() + uintptr(i)*entrySize
}
