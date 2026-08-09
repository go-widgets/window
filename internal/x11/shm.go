// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package x11

import "fmt"

// This file implements the client side of the MIT-SHM extension, version 1.2.
//
// Plain PutImage ships every pixel of an update through the X socket. MIT-SHM
// instead has the client write pixels into a shared-memory segment both peers
// map, then send a tiny ShmPutImage request telling the server to blit from
// that segment — so a full-window present costs a ~40-byte request instead of
// megabytes over the socket. The 1.2 AttachFd request passes the segment's
// descriptor to the server over SCM_RIGHTS (the same sovereign fd-passing
// machinery the Wayland backend uses), so no SysV shmget/shmid is involved.
//
// Everything degrades cleanly: when the server lacks the extension, lacks
// fd-passing support (a remote/TCP server, though this backend is unix-only),
// or version < 1.2, the caller keeps using plain PutImage.

// MIT-SHM minor opcodes (X11 MIT-SHM protocol, "MIT-SHM" extension).
const (
	shmReqQueryVersion = 0
	shmReqDetach       = 2
	shmReqPutImage     = 3
	shmReqAttachFd     = 6
)

// shmName is the extension name passed to QueryExtension.
const shmName = "MIT-SHM"

// Shm is a queried, ready-to-use MIT-SHM extension handle: the negotiated
// major opcode and version, and whether AttachFd (>= 1.2) is usable on this
// connection.
type Shm struct {
	c          *Conn
	major      byte
	VerMajor   uint16
	VerMinor   uint16
	SharedPix  bool  // server supports shared pixmaps
	PixmapFmt  uint8 // pixmap format for shared pixmaps
	FDCapable  bool  // AttachFd usable: version >= 1.2 AND transport passes fds
}

// QueryShm queries the MIT-SHM extension and its version. It returns
// (nil, nil) — no error — when the server does not implement the extension,
// so the caller simply falls back to PutImage. FDCapable additionally
// requires the connection's transport to support descriptor passing.
func (c *Conn) QueryShm() (*Shm, error) {
	present, major, _, _, err := c.QueryExtension(shmName)
	if err != nil {
		return nil, err
	}
	if !present {
		return nil, nil
	}
	reply, err := c.roundTrip(major, shmReqQueryVersion, nil)
	if err != nil {
		return nil, err
	}
	s := &Shm{
		c:         c,
		major:     major,
		SharedPix: reply[1] != 0,
		VerMajor:  c.order.Uint16(reply[8:10]),
		VerMinor:  c.order.Uint16(reply[10:12]),
		PixmapFmt: reply[16],
	}
	s.FDCapable = c.SupportsFDPassing() && shmAtLeast(s.VerMajor, s.VerMinor, 1, 2)
	return s, nil
}

// shmAtLeast reports whether version (maj.min) is >= (wantMaj.wantMin).
func shmAtLeast(maj, min, wantMaj, wantMin uint16) bool {
	return maj > wantMaj || (maj == wantMaj && min >= wantMin)
}

// AttachFd registers the shared-memory segment named by seg, backed by fd,
// with the server (MIT-SHM 1.2). The descriptor is passed over SCM_RIGHTS;
// readOnly declares whether the server may only read the segment. The server
// takes ownership of the passed descriptor.
func (s *Shm) AttachFd(seg uint32, fd int, readOnly bool) error {
	e := newEncoder(s.c.order)
	e.put32(seg)
	ro := byte(0)
	if readOnly {
		ro = 1
	}
	e.put8(ro)
	e.skip(3) // unused
	return s.c.sendRequestFD(s.major, shmReqAttachFd, e.buf, fd)
}

// Detach releases a previously attached segment.
func (s *Shm) Detach(seg uint32) error {
	e := newEncoder(s.c.order)
	e.put32(seg)
	return s.c.sendRequest(s.major, shmReqDetach, e.buf)
}

// PutImage blits a w×h source region located at byte offset in segment seg
// (whose full geometry is totalW×totalH) onto drawable at (dstX, dstY), taking
// its top-left from (srcX, srcY) within the segment image. depth and the
// visual's ZPixmap format come from the Presenter. It is a single fixed-size
// request regardless of image size — the pixels travel through shared memory.
func (s *Shm) PutImage(p *Presenter, drawable, gc uint32, seg uint32, offset uint32,
	totalW, totalH, srcX, srcY, w, h, dstX, dstY int) error {
	e := newEncoder(s.c.order)
	e.put32(drawable)
	e.put32(gc)
	e.put16(uint16(totalW))
	e.put16(uint16(totalH))
	e.put16(uint16(srcX))
	e.put16(uint16(srcY))
	e.put16(uint16(w))
	e.put16(uint16(h))
	e.put16(uint16(int16(dstX)))
	e.put16(uint16(int16(dstY)))
	e.put8(p.depth)
	e.put8(imageFormatZPixmap)
	e.put8(0) // send-event: no ShmCompletion event wanted
	e.put8(0) // bpad
	e.put32(seg)
	e.put32(offset)
	return s.c.sendRequest(s.major, shmReqPutImage, e.buf)
}

// EncodeRectInto packs the rectangle (sx, sy, w, h) of an RGBA source buffer
// (srcStride bytes per row) into seg — a shared segment laid out as a
// totalW-wide ZPixmap image for this visual — at the matching position, so
// seg mirrors the framebuffer and ShmPutImage can blit any sub-rectangle of
// it. seg must hold at least SegmentSize(totalW, sy+h) bytes.
func (p *Presenter) EncodeRectInto(seg []byte, totalW int, src []byte, srcStride, sx, sy, w, h int) error {
	line := p.scanlineBytes(totalW)
	bpx := p.bpp / 8
	need := line*(sy+h-1) + (sx+w)*bpx
	if len(seg) < need {
		return fmt.Errorf("x11: shm segment too small: have %d, need %d", len(seg), need)
	}
	p.packRect(seg, sy*line+sx*bpx, line, src, srcStride, sx, sy, w, h)
	return nil
}

// SegmentSize is the byte size a w×h ZPixmap image occupies in a shared
// segment for this visual (padded scanlines).
func (p *Presenter) SegmentSize(w, h int) int { return p.scanlineBytes(w) * h }
