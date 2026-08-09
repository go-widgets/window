// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package wayland

// transport moves whole Wayland messages (already framed byte blobs) plus
// the file descriptors they carry. It is the seam that lets the protocol
// machine run either over a real UNIX socket (fds via SCM_RIGHTS) or over
// an in-process fake compositor in tests. The interface is
// platform-independent so the transport-agnostic protocol machine (and its
// unit tests, driven by an in-memory stub) compiles on every GOOS; the real
// SCM_RIGHTS socket transport (unixTransport) is Linux-only and lives in
// transport_linux.go.
type transport interface {
	// write sends one framed message and, out-of-band, its fds.
	write(msg []byte, fds []int) error
	// read returns exactly one framed message. File descriptors that
	// arrive with it (or earlier) are queued and drained via popFD.
	read() ([]byte, error)
	// popFD returns the next received file descriptor, oldest first.
	popFD() (int, bool)
	// Close releases the transport.
	Close() error
}
