// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import "testing"

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
