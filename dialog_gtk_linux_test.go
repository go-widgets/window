// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build linux && !android

package window

import (
	"errors"
	"reflect"
	"testing"

	"github.com/go-gtk/gtk4"
)

func TestGTKFilters(t *testing.T) {
	got := gtkFilters([]Filter{{Name: "Img", Extensions: []string{"png", "jpg"}}, {Name: "T", Extensions: []string{"txt"}}})
	want := []gtk4.FileFilter{{Name: "Img", Suffixes: []string{"png", "jpg"}}, {Name: "T", Suffixes: []string{"txt"}}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
	if got := gtkFilters(nil); len(got) != 0 {
		t.Fatalf("no filters: %+v", got)
	}
}

func TestGTKFileErr(t *testing.T) {
	boom := errors.New("boom")
	for _, c := range []struct {
		name string
		in   gtk4.FileDialogResult
		want error
		none bool
	}{
		{"ok", gtk4.FileDialogResult{Paths: []string{"/a"}}, nil, true},
		{"cancelled", gtk4.FileDialogResult{Cancelled: true}, ErrCancelled, false},
		{"failure", gtk4.FileDialogResult{Err: boom}, boom, false},
		{"no path", gtk4.FileDialogResult{}, nil, false},
	} {
		got := gtkFileErr(c.in)
		switch {
		case c.none && got != nil:
			t.Errorf("%s: %v, want nil", c.name, got)
		case c.want != nil && !errors.Is(got, c.want):
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		case c.want == nil && !c.none && got == nil:
			t.Errorf("%s: nil, want an error", c.name)
		}
	}
}

func TestGTKUnsupported(t *testing.T) {
	if err := gtkUnsupported(nil); err != nil {
		t.Errorf("nil: %v", err)
	}
	if err := gtkUnsupported(gtk4.ErrDialogUnsupported); !errors.Is(err, ErrNotSupported) {
		t.Errorf("old GTK: %v, want ErrNotSupported", err)
	}
	other := errors.New("other")
	if err := gtkUnsupported(other); err != other {
		t.Errorf("other: %v, want it unchanged", err)
	}
}
