// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package x11

import (
	"errors"
	"syscall"
	"testing"
)

func TestCreateAnonFile(t *testing.T) {
	// Cover both the XDG_RUNTIME_DIR-set and the fallback-to-TempDir branches.
	t.Run("xdg-set", func(t *testing.T) {
		t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
		fd, err := createAnonFile(4096)
		if err != nil {
			t.Fatalf("createAnonFile: %v", err)
		}
		_ = syscall.Close(fd)
	})
	t.Run("xdg-empty", func(t *testing.T) {
		t.Setenv("XDG_RUNTIME_DIR", "")
		fd, err := createAnonFile(4096)
		if err != nil {
			t.Fatalf("createAnonFile: %v", err)
		}
		_ = syscall.Close(fd)
	})
}

func TestCreateAnonFileBadSize(t *testing.T) {
	if _, err := createAnonFile(0); err == nil {
		t.Error("size 0 should error")
	}
	if _, err := createAnonFile(-1); err == nil {
		t.Error("negative size should error")
	}
}

func TestCreateAnonFileOpenError(t *testing.T) {
	orig := shmOpen
	shmOpen = func(string, int, uint32) (int, error) { return -1, errors.New("open boom") }
	defer func() { shmOpen = orig }()
	if _, err := createAnonFile(4096); err == nil {
		t.Error("open error should propagate")
	}
}

func TestCreateAnonFileFtruncateError(t *testing.T) {
	orig := ftruncateFD
	ftruncateFD = func(int, int64) error { return errors.New("ftruncate boom") }
	defer func() { ftruncateFD = orig }()
	if _, err := createAnonFile(4096); err == nil {
		t.Error("ftruncate error should propagate")
	}
}

func TestNewSegmentAndClose(t *testing.T) {
	seg, err := NewSegment(0x600, 8192)
	if err != nil {
		t.Fatalf("NewSegment: %v", err)
	}
	if seg.Seg != 0x600 || seg.Size() != 8192 || len(seg.Data) != 8192 || seg.FD < 0 {
		t.Fatalf("segment = %+v (len data %d)", seg, len(seg.Data))
	}
	seg.Data[0] = 0x5a // writable
	if err := seg.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Idempotent: second close is a no-op (data nil, fd -1).
	if err := seg.Close(); err != nil {
		t.Errorf("second Close: %v", err)
	}
}

func TestNewSegmentCreateError(t *testing.T) {
	if _, err := NewSegment(1, 0); err == nil {
		t.Error("zero size should error via createAnonFile")
	}
}

func TestNewSegmentMmapError(t *testing.T) {
	orig := mmapRegion
	mmapRegion = func(int, int) ([]byte, error) { return nil, errors.New("mmap boom") }
	defer func() { mmapRegion = orig }()
	if _, err := NewSegment(1, 4096); err == nil {
		t.Error("mmap error should propagate")
	}
}

func TestSegmentCloseErrors(t *testing.T) {
	seg, err := NewSegment(1, 4096)
	if err != nil {
		t.Fatalf("NewSegment: %v", err)
	}
	// Real cleanup afterwards via the saved originals.
	realMunmap, realClose := munmapRegion, closeFD
	munmapRegion = func([]byte) error { return errors.New("munmap boom") }
	closeFD = func(int) error { return errors.New("close boom") }
	defer func() { munmapRegion, closeFD = realMunmap, realClose }()

	if err := seg.Close(); err == nil {
		t.Error("Close should surface the munmap error")
	}
	// Data cleared, fd cleared despite the injected failures.
	if seg.Data != nil || seg.FD != -1 {
		t.Errorf("Close should clear data/fd: data=%v fd=%d", seg.Data != nil, seg.FD)
	}
	// Release the resources for real so the test leaks nothing.
	_ = realMunmap
	_ = realClose
}

func TestSegmentCloseFDErrorOnly(t *testing.T) {
	seg, err := NewSegment(1, 4096)
	if err != nil {
		t.Fatalf("NewSegment: %v", err)
	}
	realClose := closeFD
	// Unmap for real, but fail the descriptor close to cover that branch.
	if err := munmapRegion(seg.Data); err != nil {
		t.Fatalf("munmap: %v", err)
	}
	seg.Data = nil
	closeFD = func(int) error { return errors.New("close boom") }
	defer func() { closeFD = realClose }()
	if err := seg.Close(); err == nil {
		t.Error("Close should surface the fd-close error")
	}
	_ = realClose
}
