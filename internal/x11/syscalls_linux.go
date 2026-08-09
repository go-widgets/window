// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package x11

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
)

// anonCounter uniquifies the backing-file name within a process.
var anonCounter uint64

// createAnonFile creates an unlinked, size-byte file in $XDG_RUNTIME_DIR (a
// tmpfs) or the temp dir and returns its descriptor. The file is unlinked
// immediately, so it lives only as long as the descriptor — which is what is
// passed to the X server. This is the portable equivalent of memfd_create:
// it needs only Open/Unlink/Ftruncate, present on every Linux target.
func createAnonFile(size int) (int, error) {
	if size <= 0 {
		return -1, fmt.Errorf("x11: shm size %d must be positive", size)
	}
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	n := atomic.AddUint64(&anonCounter, 1)
	name := filepath.Join(dir, fmt.Sprintf("gw-x11-shm-%d-%d", os.Getpid(), n))
	fd, err := shmOpen(name, syscall.O_RDWR|syscall.O_CREAT|syscall.O_EXCL|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return -1, fmt.Errorf("x11: shm open: %w", err)
	}
	_ = syscall.Unlink(name)
	if err := ftruncateFD(fd, int64(size)); err != nil {
		_ = syscall.Close(fd)
		return -1, fmt.Errorf("x11: shm ftruncate: %w", err)
	}
	return fd, nil
}

// shmOpen, ftruncateFD, mmapRegion, munmapRegion and closeFD wrap the
// syscalls behind package variables so tests can force their (kernel-rare)
// failure paths and reach full branch coverage without a real fault.
var (
	shmOpen     = syscall.Open
	ftruncateFD = syscall.Ftruncate
	mmapRegion  = func(fd, size int) ([]byte, error) {
		return syscall.Mmap(fd, 0, size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	}
	munmapRegion = syscall.Munmap
	closeFD      = syscall.Close
)
