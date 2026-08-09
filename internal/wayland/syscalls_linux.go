// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package wayland

import (
	"fmt"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
)

// anonCounter uniquifies the backing-file name within a process.
var anonCounter uint64

// createAnonFile creates an unlinked, zero-length-then-truncated file of
// size bytes in $XDG_RUNTIME_DIR (a tmpfs) and returns its descriptor. The
// file is unlinked immediately, so it lives only as long as the descriptor;
// the descriptor is what gets passed to the compositor over SCM_RIGHTS.
//
// This is the portable equivalent of memfd_create: it needs only
// syscall.Open/Unlink/Ftruncate.
func createAnonFile(size int) (int, error) {
	if size <= 0 {
		return -1, fmt.Errorf("wayland: shm size %d must be positive", size)
	}
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = os.TempDir()
	}
	n := atomic.AddUint64(&anonCounter, 1)
	name := filepath.Join(dir, fmt.Sprintf("gw-wl-shm-%d-%d", os.Getpid(), n))
	fd, err := syscall.Open(name, syscall.O_RDWR|syscall.O_CREAT|syscall.O_EXCL|syscall.O_CLOEXEC, 0o600)
	if err != nil {
		return -1, fmt.Errorf("wayland: shm open: %w", err)
	}
	_ = syscall.Unlink(name)
	if err := ftruncateFD(fd, int64(size)); err != nil {
		_ = syscall.Close(fd)
		return -1, fmt.Errorf("wayland: shm ftruncate: %w", err)
	}
	return fd, nil
}

// ftruncateFD, mmapRegion, munmapRegion and closeFD wrap the shared-memory
// syscalls behind package variables so tests can force the (kernel-rare)
// failure paths and reach full branch coverage without a real fault.
var (
	ftruncateFD = syscall.Ftruncate
	mmapRegion  = func(fd, size int) ([]byte, error) {
		data, err := syscall.Mmap(fd, 0, size, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
		if err != nil {
			return nil, fmt.Errorf("wayland: shm mmap: %w", err)
		}
		return data, nil
	}
	munmapRegion = syscall.Munmap
	closeFD      = syscall.Close
)

// mapReadOnly and unmapReadOnly wrap the keymap-fd mmap syscalls behind
// package variables so a test can exercise the failure path.
var (
	mapReadOnly = func(fd, size int) ([]byte, error) {
		return syscall.Mmap(fd, 0, size, syscall.PROT_READ, syscall.MAP_PRIVATE)
	}
	unmapReadOnly = syscall.Munmap
)
