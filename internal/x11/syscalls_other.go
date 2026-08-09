// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux

package x11

import "errors"

// ErrUnsupported reports that the sovereign X11 backend's shared-memory path
// is unavailable off Linux. The transport-agnostic protocol codec still
// compiles and is unit-tested on every platform; only the MIT-SHM syscalls
// (anonymous file, mmap) are Linux-only, so createAnonFile fails here and no
// segment is ever created.
var ErrUnsupported = errors.New("x11: shared-memory backend is only supported on Linux")

// createAnonFile is unavailable off Linux.
func createAnonFile(int) (int, error) { return -1, ErrUnsupported }

// mmapRegion, munmapRegion and closeFD are no-op / error stubs off Linux;
// they are never reached in practice because createAnonFile fails first.
var (
	mmapRegion   = func(int, int) ([]byte, error) { return nil, ErrUnsupported }
	munmapRegion = func([]byte) error { return nil }
	closeFD      = func(int) error { return nil }
)
