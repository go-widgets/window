// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"fmt"
	"os"
	"path"
	"strconv"
	"strings"
)

// display is a parsed $DISPLAY value.
type display struct {
	host   string // "" or "unix" means a local unix-domain connection
	number string // display number, e.g. "0"
	screen int    // screen index (default 0)
}

// parseDisplay parses an X11 DISPLAY string of the form
// "[host]:number[.screen]" (e.g. ":0", ":0.0", "unix:1", "host:0.2").
func parseDisplay(s string) (display, error) {
	var d display
	colon := strings.LastIndexByte(s, ':')
	if colon < 0 {
		return d, fmt.Errorf("window: DISPLAY %q has no ':'", s)
	}
	d.host = s[:colon]
	rest := s[colon+1:]
	if rest == "" {
		return d, fmt.Errorf("window: DISPLAY %q has no display number", s)
	}
	if dot := strings.IndexByte(rest, '.'); dot >= 0 {
		scr, err := strconv.Atoi(rest[dot+1:])
		if err != nil {
			return d, fmt.Errorf("window: DISPLAY %q has a bad screen: %w", s, err)
		}
		d.screen = scr
		rest = rest[:dot]
	}
	if _, err := strconv.Atoi(rest); err != nil {
		return d, fmt.Errorf("window: DISPLAY %q has a bad number: %w", s, err)
	}
	d.number = rest
	return d, nil
}

// isLocal reports whether the display denotes a local unix-domain server.
func (d display) isLocal() bool { return d.host == "" || d.host == "unix" }

// unixPath is the filesystem socket for a local display in the conventional
// /tmp/.X11-unix directory.
func (d display) unixPath() string { return "/tmp/.X11-unix/X" + d.number }

// abstractPath is the Linux abstract-namespace socket for a local display
// (Go expresses the leading NUL as a leading "@").
func (d display) abstractPath() string { return "@/tmp/.X11-unix/X" + d.number }

// tmpdirPath is the filesystem socket for a local display under $TMPDIR.
//
// /tmp/.X11-unix is only a convention, and one an OS is free not to offer: an
// Android system has no /tmp at all, so its X servers (Termux:X11 and the
// Termux X.Org builds) put the socket in the private directory named by
// $TMPDIR instead. Trying that first costs one dial on the platforms that DO
// have /tmp, where $TMPDIR is either unset or /tmp itself — both of which
// return "" so the caller skips the duplicate.
//
// The joining is "path", not "path/filepath": this is a UNIX-DOMAIN SOCKET
// path on the machine running the X server, and it is separated by forward
// slashes whatever the host's own convention is. filepath was indistinguishable
// from path here until this file's (untagged) tests were first run on a Windows
// host, where it produced \tmp\.X11-unix\X0.
func (d display) tmpdirPath() string {
	dir := os.Getenv("TMPDIR")
	if dir == "" || path.Clean(dir) == "/tmp" {
		return ""
	}
	return path.Join(dir, ".X11-unix", "X"+d.number)
}

// socketPaths lists, in dial order, the local endpoints an X server for this
// display may be listening on: the $TMPDIR directory when one is set and is
// not /tmp, then the conventional filesystem socket, then the Linux abstract
// socket. The list is never empty, so a caller that dials each in turn always
// has a last error to report.
func (d display) socketPaths() []string {
	paths := make([]string, 0, 3)
	if p := d.tmpdirPath(); p != "" {
		paths = append(paths, p)
	}
	return append(paths, d.unixPath(), d.abstractPath())
}
