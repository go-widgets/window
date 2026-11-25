// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build windows && !arm64

package wincb

import "syscall"

// New returns a C-callable entry for fn. Where the runtime already preserves
// every callee-saved register this is syscall.NewCallback itself.
func New(fn any) uintptr { return syscall.NewCallback(fn) }
