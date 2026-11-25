// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows && arm64

package wincb

// The test's assembly, declared here so go vet checks it against its frames.
// Nothing outside the tests refers to it, so the linker drops it.

// probe calls fn(a, b) by the C convention with known doubles in D8-D15, writes
// the D8-D15 it finds afterwards into out, and returns fn's result.
func probe(fn uintptr, out *[8]uint64, a, b uint64) uint64

// clobber is a C-convention function that overwrites D8-D15 and returns a+b.
func clobber()

func clobberAddr() uintptr
