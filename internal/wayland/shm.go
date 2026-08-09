// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package wayland

import "fmt"

// wl_shm pixel formats (a subset of the DRM fourcc set). ARGB8888 stores a
// 32-bit value 0xAARRGGBB per pixel; on the wire the region is filled with
// that value in the machine's native byte order, which the compositor —
// running on the same machine — reads back identically.
const (
	ShmFormatARGB8888 = 0
	ShmFormatXRGB8888 = 1
)

// shmRegion is an mmap'd anonymous shared-memory region: the descriptor is
// handed to the compositor, and data is the client-writable pixel store the
// compositor reads from.
type shmRegion struct {
	fd   int
	data []byte
	size int
}

// newShmRegion allocates and maps a shared-memory region of size bytes.
func newShmRegion(size int) (*shmRegion, error) {
	fd, err := createAnonFile(size)
	if err != nil {
		return nil, err
	}
	data, err := mmapRegion(fd, size)
	if err != nil {
		_ = closeFD(fd)
		return nil, err
	}
	return &shmRegion{fd: fd, data: data, size: size}, nil
}

// Close unmaps the region and closes its descriptor, returning the first
// error encountered (both steps are attempted regardless).
func (r *shmRegion) Close() error {
	var first error
	if r.data != nil {
		if err := munmapRegion(r.data); err != nil && first == nil {
			first = err
		}
		r.data = nil
	}
	if r.fd >= 0 {
		if err := closeFD(r.fd); err != nil && first == nil {
			first = err
		}
		r.fd = -1
	}
	return first
}

// --- wl_shm / wl_shm_pool / wl_buffer -------------------------------------

// Shm is the wl_shm global: it advertises supported pixel formats and
// creates shared-memory pools.
type Shm struct {
	conn    *Conn
	id      uint32
	formats []uint32
}

const shmIfaceVersion = 1

// wl_shm request opcode.
const shmReqCreatePool = 0

// wl_shm event opcode.
const shmEvtFormat = 0

// bindShm binds the wl_shm global from the registry.
func bindShm(reg *Registry, g Global) (*Shm, error) {
	ver := min32(g.Version, shmIfaceVersion)
	id, err := reg.bind(g.Name, "wl_shm", ver)
	if err != nil {
		return nil, err
	}
	s := &Shm{conn: reg.conn, id: id}
	reg.conn.register(id, s.handle)
	return s, nil
}

// handle records advertised formats.
func (s *Shm) handle(opcode uint16, d *decoder) error {
	if opcode != shmEvtFormat {
		return nil
	}
	f := d.getU32()
	if !d.ok {
		return fmt.Errorf("wayland: truncated wl_shm.format")
	}
	s.formats = append(s.formats, f)
	return nil
}

// Supports reports whether the compositor advertised the given format.
func (s *Shm) Supports(format uint32) bool {
	for _, f := range s.formats {
		if f == format {
			return true
		}
	}
	return false
}

// CreatePool allocates a shared-memory region of size bytes and creates a
// wl_shm_pool over it, passing the descriptor to the compositor.
func (s *Shm) CreatePool(size int) (*ShmPool, error) {
	region, err := newShmRegion(size)
	if err != nil {
		return nil, err
	}
	poolID := s.conn.allocID()
	e := newEncoder(s.conn.order)
	e.putU32(poolID)      // new_id pool
	e.putI32(int32(size)) // size
	if err := s.conn.send(s.id, shmReqCreatePool, e.buf, []int{region.fd}); err != nil {
		_ = region.Close()
		return nil, err
	}
	p := &ShmPool{conn: s.conn, id: poolID, region: region}
	s.conn.register(poolID, p.handle)
	return p, nil
}

// ShmPool is a wl_shm_pool: a mapped memory region from which buffers are
// carved.
type ShmPool struct {
	conn   *Conn
	id     uint32
	region *shmRegion
}

// wl_shm_pool request opcodes.
const (
	shmPoolReqCreateBuffer = 0
	shmPoolReqDestroy      = 1
)

// handle: wl_shm_pool has no events; the method exists so the object has a
// registered dispatcher (an unexpected event is ignored).
func (p *ShmPool) handle(uint16, *decoder) error { return nil }

// Data returns the writable pixel store backing the pool.
func (p *ShmPool) Data() []byte { return p.region.data }

// CreateBuffer carves a wl_buffer from the pool at byte offset with the
// given geometry and pixel format.
func (p *ShmPool) CreateBuffer(offset, width, height, stride int, format uint32) (*Buffer, error) {
	bufID := p.conn.allocID()
	e := newEncoder(p.conn.order)
	e.putU32(bufID)
	e.putI32(int32(offset))
	e.putI32(int32(width))
	e.putI32(int32(height))
	e.putI32(int32(stride))
	e.putU32(format)
	if err := p.conn.send(p.id, shmPoolReqCreateBuffer, e.buf, nil); err != nil {
		return nil, err
	}
	b := &Buffer{conn: p.conn, id: bufID, offset: offset, released: true}
	p.conn.register(bufID, b.handle)
	return b, nil
}

// Destroy releases the pool object and its backing region. The already
// created buffers remain valid until they too are destroyed.
func (p *ShmPool) Destroy() error {
	err := p.conn.send(p.id, shmPoolReqDestroy, nil, nil)
	p.conn.unregister(p.id)
	if cerr := p.region.Close(); err == nil {
		err = cerr
	}
	return err
}

// Buffer is a wl_buffer: a rectangular view into a pool the compositor can
// read while it is attached to a surface. released tracks whether the
// compositor currently holds it (false) or has handed it back (true).
type Buffer struct {
	conn     *Conn
	id       uint32
	offset   int
	released bool
}

// wl_buffer request opcode.
const bufferReqDestroy = 0

// wl_buffer event opcode.
const bufferEvtRelease = 0

// handle marks the buffer released when the compositor is done reading it.
func (b *Buffer) handle(opcode uint16, _ *decoder) error {
	if opcode == bufferEvtRelease {
		b.released = true
	}
	return nil
}

// Released reports whether the buffer is free for the client to redraw.
func (b *Buffer) Released() bool { return b.released }

// Destroy releases the buffer object.
func (b *Buffer) Destroy() error {
	err := b.conn.send(b.id, bufferReqDestroy, nil, nil)
	b.conn.unregister(b.id)
	return err
}

// min32 returns the smaller of two uint32 values.
func min32(a, b uint32) uint32 {
	if a < b {
		return a
	}
	return b
}
