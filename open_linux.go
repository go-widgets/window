// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package window

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"

	"github.com/go-widgets/window/internal/wayland"
	"github.com/go-widgets/window/internal/x11"
)

// Open connects to the running display server and returns a window ready for
// Run. It auto-selects the backend: Wayland when $WAYLAND_DISPLAY is set
// (the modern default on contemporary Linux desktops), otherwise the X11
// backend driven by $DISPLAY. Both are sovereign, pure-Go, CGO-free
// implementations of their wire protocols.
func Open(cfg Config) (Backend, error) {
	if name := os.Getenv("WAYLAND_DISPLAY"); name != "" {
		return openWayland(cfg, name)
	}
	return openX11(cfg)
}

// openWayland dials the compositor socket named by $WAYLAND_DISPLAY (resolved
// against $XDG_RUNTIME_DIR) and brings up an xdg-shell toplevel.
func openWayland(cfg Config, name string) (Backend, error) {
	path, err := waylandSocketPath(name)
	if err != nil {
		return nil, err
	}
	nc, err := net.Dial("unix", path)
	if err != nil {
		return nil, fmt.Errorf("window: cannot connect to Wayland compositor: %w", err)
	}
	uc, ok := nc.(*net.UnixConn)
	if !ok { // net.Dial("unix", ...) always yields *net.UnixConn
		nc.Close()
		return nil, fmt.Errorf("window: Wayland dial returned %T, want *net.UnixConn", nc)
	}
	conn := wayland.New(uc)
	w, err := newWaylandWindow(conn, cfg)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return w, nil
}

// waylandSocketPath resolves the compositor socket path. An absolute
// $WAYLAND_DISPLAY is used verbatim; a bare name is joined onto
// $XDG_RUNTIME_DIR.
func waylandSocketPath(name string) (string, error) {
	if filepath.IsAbs(name) {
		return name, nil
	}
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		return "", fmt.Errorf("window: XDG_RUNTIME_DIR is not set (needed for WAYLAND_DISPLAY=%q)", name)
	}
	return filepath.Join(dir, name), nil
}

// openX11 connects to the X11 server named by cfg.Display (or $DISPLAY),
// authenticates with the matching MIT-MAGIC-COOKIE-1 from the Xauthority
// file, creates and maps a window and returns it ready for Run.
func openX11(cfg Config) (Backend, error) {
	disp := cfg.Display
	if disp == "" {
		disp = os.Getenv("DISPLAY")
	}
	if disp == "" {
		return nil, fmt.Errorf("window: neither WAYLAND_DISPLAY nor DISPLAY is set")
	}
	d, err := parseDisplay(disp)
	if err != nil {
		return nil, err
	}
	nc, err := dialDisplay(d)
	if err != nil {
		return nil, err
	}
	host, _ := os.Hostname()
	authName, authData, err := x11.LoadAuthCookie(authFilePathEnv(), host, d.number)
	if err != nil {
		nc.Close()
		return nil, err
	}
	// Wrap a local unix socket so the connection can pass a shared-memory
	// descriptor to the server (MIT-SHM AttachFd) over SCM_RIGHTS; every other
	// request still travels as an ordinary socket write.
	var rw io.ReadWriteCloser = nc
	if uc, ok := nc.(*net.UnixConn); ok {
		rw = x11.WrapUnix(uc)
	}
	conn, err := x11.Handshake(rw, binary.LittleEndian, authName, authData)
	if err != nil {
		nc.Close()
		return nil, err
	}
	win, err := newWindow(conn, cfg)
	if err != nil {
		conn.Close()
		return nil, err
	}
	return win, nil
}

// dialDisplay connects to a local unix-domain X server, trying the
// filesystem socket then the Linux abstract socket. Remote (TCP) displays
// are not supported by this sovereign backend.
func dialDisplay(d display) (net.Conn, error) {
	if !d.isLocal() {
		return nil, fmt.Errorf("window: remote display %q not supported (unix sockets only)", d.host)
	}
	if nc, err := net.Dial("unix", d.unixPath()); err == nil {
		return nc, nil
	}
	nc, err := net.Dial("unix", d.abstractPath())
	if err != nil {
		return nil, fmt.Errorf("window: cannot connect to X server: %w", err)
	}
	return nc, nil
}

// authFilePathEnv resolves the Xauthority file location (XAUTHORITY, then
// $HOME/.Xauthority).
func authFilePathEnv() string {
	if p := os.Getenv("XAUTHORITY"); p != "" {
		return p
	}
	if home, err := os.UserHomeDir(); err == nil {
		return home + "/.Xauthority"
	}
	return ""
}

// Both Linux back-ends carry the OS clipboard — X11 through selection
// ownership, Wayland through wl_data_device — and both read the desktop's
// appearance from the XDG portal, which is display-server-independent.
// Asserting them where the capability is declared stops a rename inside a
// back-end from silently turning w.(window.Clipboard) into a failed assertion.
var (
	_ Clipboard        = (*Window)(nil)
	_ AppearanceReader = (*Window)(nil)

	_ Clipboard        = (*wlWindow)(nil)
	_ AppearanceReader = (*wlWindow)(nil)
)
