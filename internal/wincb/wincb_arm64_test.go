// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows && arm64

package wincb

import (
	"os"
	"strings"
	"testing"
)

func want() [8]uint64 {
	var w [8]uint64
	for i := range w {
		w[i] = 0x7ff0abcd00000001 + uint64(i)
	}
	return w
}

// The control: without the thunk the callee's writes to D8-D15 reach the
// caller. If this ever passes, the test below proves nothing.
func TestControlCalleeClobbersCallerDoubles(t *testing.T) {
	var got [8]uint64
	if r := probe(clobberAddr(), &got, 40, 2); r != 42 {
		t.Fatalf("result = %d, want 42", r)
	}
	if got == want() {
		t.Fatal("the callee did not change D8-D15: the probe cannot see a clobber")
	}
}

// What #188 is: a callee that overwrites D8-D15 must not be visible to its
// native caller, and its arguments and result must pass through unchanged.
func TestThunkPreservesCallerDoubles(t *testing.T) {
	var got [8]uint64
	if r := probe(wrap(clobberAddr()), &got, 40, 2); r != 42 {
		t.Fatalf("result = %d through the thunk, want 42", r)
	}
	if got != want() {
		t.Fatalf("D8-D15 after the call = %#x, want %#x", got, want())
	}
}

// Every slot has to land on its own callback, including the last one.
func TestEverySlotReachesItsOwnTarget(t *testing.T) {
	mu.Lock()
	start := next
	mu.Unlock()
	var entries []uintptr
	for i := start; i < Slots; i++ {
		entries = append(entries, wrap(clobberAddr()))
	}
	for i, e := range entries {
		var got [8]uint64
		if r := probe(e, &got, uint64(i), 1); r != uint64(i)+1 || got != want() {
			t.Fatalf("slot %d: result %d, doubles %#x", start+i, r, got)
		}
	}
	defer func() {
		if recover() == nil {
			t.Fatal("a call past the last slot did not panic")
		}
	}()
	wrap(clobberAddr())
}

func TestGeneratedTableMatchesSlots(t *testing.T) {
	b, err := os.ReadFile("table_windows_arm64.s")
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(b), "\tB\t·cbCommon(SB)"); n != Slots {
		t.Fatalf("table holds %d entries, Slots is %d: run go generate", n, Slots)
	}
}
