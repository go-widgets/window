// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"slices"
	"testing"
)

func TestParseDisplay(t *testing.T) {
	cases := []struct {
		in      string
		host    string
		number  string
		screen  int
		wantErr bool
	}{
		{":0", "", "0", 0, false},
		{":0.0", "", "0", 0, false},
		{":1.2", "", "1", 2, false},
		{"unix:3", "unix", "3", 0, false},
		{"host:0.2", "host", "0", 2, false},
		{"nocolon", "", "", 0, true},
		{":", "", "", 0, true},
		{":x", "", "", 0, true},
		{":0.z", "", "", 0, true},
	}
	for _, c := range cases {
		d, err := parseDisplay(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseDisplay(%q) expected error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseDisplay(%q): %v", c.in, err)
			continue
		}
		if d.host != c.host || d.number != c.number || d.screen != c.screen {
			t.Errorf("parseDisplay(%q) = %+v want host=%q num=%q screen=%d", c.in, d, c.host, c.number, c.screen)
		}
	}
}

func TestDisplayPaths(t *testing.T) {
	d := display{host: "", number: "0"}
	if !d.isLocal() {
		t.Fatal("empty host should be local")
	}
	if d.unixPath() != "/tmp/.X11-unix/X0" {
		t.Fatalf("unixPath = %q", d.unixPath())
	}
	if d.abstractPath() != "@/tmp/.X11-unix/X0" {
		t.Fatalf("abstractPath = %q", d.abstractPath())
	}
	if (display{host: "unix"}).isLocal() != true {
		t.Fatal("unix host should be local")
	}
	if (display{host: "remote"}).isLocal() != false {
		t.Fatal("remote host should not be local")
	}
}

func TestDisplayTmpdirPath(t *testing.T) {
	d := display{number: "7"}
	// No $TMPDIR: nothing to try beyond the conventional locations.
	t.Setenv("TMPDIR", "")
	if p := d.tmpdirPath(); p != "" {
		t.Fatalf("tmpdirPath with no TMPDIR = %q, want empty", p)
	}
	// $TMPDIR that resolves to /tmp is the conventional path already, so it
	// must not produce a duplicate dial — with or without a trailing slash.
	for _, dir := range []string{"/tmp", "/tmp/"} {
		t.Setenv("TMPDIR", dir)
		if p := d.tmpdirPath(); p != "" {
			t.Fatalf("tmpdirPath with TMPDIR=%q = %q, want empty", dir, p)
		}
	}
	// A relocated $TMPDIR (Android/Termux) yields its own .X11-unix socket.
	t.Setenv("TMPDIR", "/data/data/com.termux/files/usr/tmp")
	if p, want := d.tmpdirPath(), "/data/data/com.termux/files/usr/tmp/.X11-unix/X7"; p != want {
		t.Fatalf("tmpdirPath = %q, want %q", p, want)
	}
}

func TestDisplaySocketPaths(t *testing.T) {
	d := display{number: "0"}
	// Without a relocated $TMPDIR the candidates are the two conventional ones.
	t.Setenv("TMPDIR", "")
	want := []string{"/tmp/.X11-unix/X0", "@/tmp/.X11-unix/X0"}
	if got := d.socketPaths(); !slices.Equal(got, want) {
		t.Fatalf("socketPaths = %q, want %q", got, want)
	}
	// With one, it is tried FIRST: it is the only socket that exists on a
	// system with no /tmp, and dialing it first keeps that system's start-up
	// free of a doomed dial.
	t.Setenv("TMPDIR", "/data/data/com.termux/files/usr/tmp")
	want = []string{"/data/data/com.termux/files/usr/tmp/.X11-unix/X0", "/tmp/.X11-unix/X0", "@/tmp/.X11-unix/X0"}
	if got := d.socketPaths(); !slices.Equal(got, want) {
		t.Fatalf("socketPaths = %q, want %q", got, want)
	}
}
