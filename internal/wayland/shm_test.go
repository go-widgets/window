// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package wayland

import (
	"encoding/binary"
	"errors"
	"syscall"
	"testing"
)

func TestCreateAnonFile(t *testing.T) {
	// With XDG_RUNTIME_DIR unset, the backing file lands in os.TempDir()
	// (this branch is taken on macOS but not Linux, so force it here for a
	// deterministic 100% regardless of the host environment).
	t.Setenv("XDG_RUNTIME_DIR", "")
	fd, err := createAnonFile(4096)
	if err != nil {
		t.Fatalf("createAnonFile (TempDir fallback): %v", err)
	}
	_ = syscall.Close(fd)

	// With XDG_RUNTIME_DIR set to a real directory, the file lands there.
	t.Setenv("XDG_RUNTIME_DIR", t.TempDir())
	fd, err = createAnonFile(4096)
	if err != nil {
		t.Fatalf("createAnonFile: %v", err)
	}
	if fd < 0 {
		t.Fatalf("bad fd %d", fd)
	}
	region := &shmRegion{fd: fd, size: 4096}
	data, err := mmapRegion(fd, 4096)
	if err != nil {
		t.Fatalf("mmap: %v", err)
	}
	region.data = data
	region.data[0] = 0xAB
	if region.data[0] != 0xAB {
		t.Error("shm region not writable")
	}
	if err := region.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Second Close is a no-op.
	if err := region.Close(); err != nil {
		t.Fatalf("double Close: %v", err)
	}
}

func TestShmRegionCloseErrors(t *testing.T) {
	origM, origC := munmapRegion, closeFD
	defer func() { munmapRegion, closeFD = origM, origC }()
	// Munmap fails: its error is returned and close still runs.
	munmapRegion = func([]byte) error { return errors.New("munmap boom") }
	closeFD = func(int) error { return nil }
	r := &shmRegion{fd: 3, data: []byte{0}, size: 1}
	if err := r.Close(); err == nil {
		t.Error("munmap error should be returned")
	}
	// Close fails while munmap succeeds.
	munmapRegion = func([]byte) error { return nil }
	closeFD = func(int) error { return errors.New("close boom") }
	r2 := &shmRegion{fd: 3, data: []byte{0}, size: 1}
	if err := r2.Close(); err == nil {
		t.Error("close error should be returned")
	}
}

func TestCreateAnonFileBadSize(t *testing.T) {
	if _, err := createAnonFile(0); err == nil {
		t.Error("size 0 should error")
	}
	if _, err := createAnonFile(-1); err == nil {
		t.Error("negative size should error")
	}
}

func TestCreateAnonFileBadDir(t *testing.T) {
	t.Setenv("XDG_RUNTIME_DIR", "/no/such/dir/for/wayland/shm")
	if _, err := createAnonFile(4096); err == nil {
		t.Error("open in nonexistent dir should error")
	}
}

func TestCreateAnonFileFtruncateError(t *testing.T) {
	orig := ftruncateFD
	ftruncateFD = func(int, int64) error { return errors.New("ftruncate boom") }
	defer func() { ftruncateFD = orig }()
	if _, err := createAnonFile(4096); err == nil {
		t.Error("ftruncate failure should propagate")
	}
}

func TestShmBindSuccess(t *testing.T) {
	c := NewConn(&stubTransport{}, binary.LittleEndian)
	reg := &Registry{conn: c}
	reg.globals = []Global{{Name: 1, Interface: "wl_shm", Version: 1}}
	s, err := reg.Shm()
	if err != nil {
		t.Fatalf("Shm: %v", err)
	}
	if _, ok := c.handlers[s.id]; !ok {
		t.Error("bound wl_shm should register a handler")
	}
}

func TestNewShmRegionMmapError(t *testing.T) {
	orig := mmapRegion
	mmapRegion = func(int, int) ([]byte, error) { return nil, errors.New("mmap boom") }
	defer func() { mmapRegion = orig }()
	if _, err := newShmRegion(4096); err == nil {
		t.Error("mmap failure should propagate")
	}
}

func TestNewShmRegionBadSize(t *testing.T) {
	if _, err := newShmRegion(0); err == nil {
		t.Error("size 0 should error before mmap")
	}
}

func TestShmHandle(t *testing.T) {
	order := binary.LittleEndian
	s := &Shm{}
	if err := s.handle(shmEvtFormat, newDecoder(order, bodyOf(order, func(e *encoder) { e.putU32(ShmFormatARGB8888) }))); err != nil {
		t.Fatal(err)
	}
	if err := s.handle(shmEvtFormat, newDecoder(order, bodyOf(order, func(e *encoder) { e.putU32(ShmFormatXRGB8888) }))); err != nil {
		t.Fatal(err)
	}
	if !s.Supports(ShmFormatARGB8888) || !s.Supports(ShmFormatXRGB8888) {
		t.Error("advertised formats should be supported")
	}
	if s.Supports(0x99) {
		t.Error("unadvertised format should not be supported")
	}
	// Unknown opcode ignored.
	if err := s.handle(99, newDecoder(order, nil)); err != nil {
		t.Errorf("unknown shm opcode = %v", err)
	}
	// Truncated format errors.
	if err := s.handle(shmEvtFormat, newDecoder(order, nil)); err == nil {
		t.Error("truncated format should error")
	}
}

func TestShmBindNoGlobal(t *testing.T) {
	c := NewConn(&stubTransport{}, binary.LittleEndian)
	reg := &Registry{conn: c}
	if _, err := reg.Shm(); err == nil {
		t.Error("Shm with no global should error")
	}
	if _, err := reg.Compositor(); err == nil {
		t.Error("Compositor with no global should error")
	}
	if _, err := reg.XdgWmBase(); err == nil {
		t.Error("XdgWmBase with no global should error")
	}
}

func TestShmBindWriteError(t *testing.T) {
	c := NewConn(&stubTransport{writeErr: errors.New("nope")}, binary.LittleEndian)
	reg := &Registry{conn: c}
	reg.globals = []Global{{Name: 1, Interface: "wl_shm", Version: 1}}
	if _, err := reg.Shm(); err == nil {
		t.Error("bind write error should propagate")
	}
}

func TestCreatePool(t *testing.T) {
	order := binary.LittleEndian
	st := &stubTransport{}
	c := NewConn(st, order)
	s := &Shm{conn: c, id: 3}
	pool, err := s.CreatePool(8192)
	if err != nil {
		t.Fatalf("CreatePool: %v", err)
	}
	if len(pool.Data()) != 8192 {
		t.Fatalf("pool data len = %d", len(pool.Data()))
	}
	obj, op, d := lastWrite(t, st, order)
	if obj != 3 || op != shmReqCreatePool {
		t.Fatalf("create_pool obj=%d op=%d", obj, op)
	}
	if id := d.getU32(); id != pool.id {
		t.Errorf("pool new_id = %d, want %d", id, pool.id)
	}
	if sz := d.getI32(); sz != 8192 {
		t.Errorf("pool size = %d", sz)
	}
	if err := pool.handle(0, nil); err != nil {
		t.Errorf("pool.handle = %v", err)
	}
	if err := pool.Destroy(); err != nil {
		t.Fatalf("pool Destroy: %v", err)
	}
}

func TestCreatePoolRegionError(t *testing.T) {
	c := NewConn(&stubTransport{}, binary.LittleEndian)
	s := &Shm{conn: c, id: 3}
	if _, err := s.CreatePool(0); err == nil {
		t.Error("CreatePool with bad size should error")
	}
}

func TestCreatePoolSendError(t *testing.T) {
	c := NewConn(&stubTransport{writeErr: errors.New("nope")}, binary.LittleEndian)
	s := &Shm{conn: c, id: 3}
	if _, err := s.CreatePool(4096); err == nil {
		t.Error("CreatePool send error should propagate (and free the region)")
	}
}

func TestCreateBuffer(t *testing.T) {
	order := binary.LittleEndian
	st := &stubTransport{}
	c := NewConn(st, order)
	s := &Shm{conn: c, id: 3}
	pool, err := s.CreatePool(4096)
	if err != nil {
		t.Fatalf("CreatePool: %v", err)
	}
	buf, err := pool.CreateBuffer(0, 16, 16, 64, ShmFormatARGB8888)
	if err != nil {
		t.Fatalf("CreateBuffer: %v", err)
	}
	if !buf.Released() {
		t.Error("fresh buffer should be released (client-owned)")
	}
	obj, op, d := lastWrite(t, st, order)
	if obj != pool.id || op != shmPoolReqCreateBuffer {
		t.Fatalf("create_buffer obj=%d op=%d", obj, op)
	}
	if id := d.getU32(); id != buf.id {
		t.Errorf("buffer new_id = %d", id)
	}
	d.getI32() // offset
	if w := d.getI32(); w != 16 {
		t.Errorf("buffer width = %d", w)
	}
	// release event flips ownership back to client.
	buf.released = false
	if err := buf.handle(bufferEvtRelease, nil); err != nil {
		t.Fatal(err)
	}
	if !buf.Released() {
		t.Error("release event should mark buffer released")
	}
	// non-release event ignored.
	buf.released = false
	if err := buf.handle(99, nil); err != nil {
		t.Fatal(err)
	}
	if buf.Released() {
		t.Error("unknown event should not release buffer")
	}
	if err := buf.Destroy(); err != nil {
		t.Fatalf("buffer Destroy: %v", err)
	}
}

func TestCreateBufferSendError(t *testing.T) {
	order := binary.LittleEndian
	st := &stubTransport{}
	c := NewConn(st, order)
	s := &Shm{conn: c, id: 3}
	pool, err := s.CreatePool(4096)
	if err != nil {
		t.Fatalf("CreatePool: %v", err)
	}
	st.writeErr = errors.New("nope")
	if _, err := pool.CreateBuffer(0, 16, 16, 64, ShmFormatARGB8888); err == nil {
		t.Error("CreateBuffer send error should propagate")
	}
	if err := pool.Destroy(); err == nil {
		t.Error("Destroy send error should propagate")
	}
}

func TestBufferDestroySendError(t *testing.T) {
	c := NewConn(&stubTransport{writeErr: errors.New("nope")}, binary.LittleEndian)
	b := &Buffer{conn: c, id: 9, released: true}
	if err := b.Destroy(); err == nil {
		t.Error("buffer Destroy send error should propagate")
	}
}

func TestMin32(t *testing.T) {
	if min32(3, 5) != 3 || min32(5, 3) != 3 || min32(4, 4) != 4 {
		t.Error("min32 wrong")
	}
}
