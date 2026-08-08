// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux

package window

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"

	"github.com/go-widgets/window/internal/x11"
)

// Open connects to the X11 server named by cfg.Display (or $DISPLAY),
// authenticates with the matching MIT-MAGIC-COOKIE-1 from the Xauthority
// file, creates and maps a window and returns it ready for Run.
func Open(cfg Config) (*Window, error) {
	disp := cfg.Display
	if disp == "" {
		disp = os.Getenv("DISPLAY")
	}
	if disp == "" {
		return nil, fmt.Errorf("window: DISPLAY is not set")
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
	conn, err := x11.Handshake(nc, binary.LittleEndian, authName, authData)
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
