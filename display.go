// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"fmt"
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

// unixPath is the filesystem socket for a local display.
func (d display) unixPath() string { return "/tmp/.X11-unix/X" + d.number }

// abstractPath is the Linux abstract-namespace socket for a local display
// (Go expresses the leading NUL as a leading "@").
func (d display) abstractPath() string { return "@/tmp/.X11-unix/X" + d.number }
