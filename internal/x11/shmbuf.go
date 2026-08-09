// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package x11

import "fmt"

// Segment is an mmap'd anonymous shared-memory region backing a MIT-SHM
// attachment: Data is the client-writable pixel store, FD is handed to the X
// server over SCM_RIGHTS by Shm.AttachFd, and Seg is the resource id the
// server knows it by.
//
// The segment struct and its lifecycle are transport-agnostic; the actual
// shared-memory syscalls (anonymous file, mmap/munmap, close) live behind
// the mmapRegion/munmapRegion/closeFD indirection and createAnonFile, which
// are provided per-platform (syscalls_linux.go / syscalls_other.go). Off
// Linux there is no X server to attach to, so createAnonFile returns
// ErrUnsupported and no segment is ever created.
type Segment struct {
	Seg  uint32
	FD   int
	Data []byte
	size int
}

// NewSegment allocates and maps a shared-memory segment of size bytes and
// assigns it the resource id seg. The caller registers it with the server via
// Shm.AttachFd and frees it with (*Segment).Close.
func NewSegment(seg uint32, size int) (*Segment, error) {
	fd, err := createAnonFile(size)
	if err != nil {
		return nil, err
	}
	data, err := mmapRegion(fd, size)
	if err != nil {
		_ = closeFD(fd)
		return nil, fmt.Errorf("x11: shm mmap: %w", err)
	}
	return &Segment{Seg: seg, FD: fd, Data: data, size: size}, nil
}

// Size returns the segment's byte length.
func (s *Segment) Size() int { return s.size }

// Close unmaps the region and closes its descriptor, returning the first
// error (both steps are attempted regardless).
func (s *Segment) Close() error {
	var first error
	if s.Data != nil {
		if err := munmapRegion(s.Data); err != nil && first == nil {
			first = err
		}
		s.Data = nil
	}
	if s.FD >= 0 {
		if err := closeFD(s.FD); err != nil && first == nil {
			first = err
		}
		s.FD = -1
	}
	return first
}
