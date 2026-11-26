// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"errors"
	"strings"
	"testing"

	"github.com/go-widgets/toolkit"
)

// plainBackend has no secondary-window capability.
type plainBackend struct{}

func (plainBackend) Run(toolkit.Widget) error { return nil }
func (plainBackend) Close() error             { return nil }
func (plainBackend) Size() (int, int)         { return 1, 1 }
func (plainBackend) String() string           { return "plain" }

// childBackend has it, and records what it was asked.
type childBackend struct {
	plainBackend
	cfg  Config
	root toolkit.Widget
	err  error
	cb   func()
}

type fakeChild struct{}

func (fakeChild) Close() error     { return nil }
func (fakeChild) Raise() error     { return nil }
func (fakeChild) Size() (int, int) { return 2, 3 }
func (fakeChild) String() string   { return "fake child" }

func (b *childBackend) OpenChild(cfg Config, root toolkit.Widget, onClose func()) (Child, error) {
	b.cfg, b.root, b.cb = cfg, root, onClose
	if b.err != nil {
		return nil, b.err
	}
	return fakeChild{}, nil
}

func TestOpenChildUnsupportedNamesTheBackend(t *testing.T) {
	_, err := OpenChild(plainBackend{}, Config{}, nil, nil)
	if !errors.Is(err, ErrNotSupported) {
		t.Fatalf("err = %v, want ErrNotSupported", err)
	}
	if !strings.Contains(err.Error(), "plain") {
		t.Fatalf("err %q does not name the backend", err)
	}
}

func TestOpenChildDelegates(t *testing.T) {
	b := &childBackend{}
	called := false
	c, err := OpenChild(b, Config{Title: "Add server", Width: 300, Height: 200}, nil, func() { called = true })
	if err != nil {
		t.Fatal(err)
	}
	if w, h := c.Size(); w != 2 || h != 3 {
		t.Fatalf("Size = %d,%d", w, h)
	}
	if b.cfg.Title != "Add server" || b.cfg.Width != 300 || b.cfg.Height != 200 {
		t.Fatalf("cfg = %+v", b.cfg)
	}
	b.cb()
	if !called {
		t.Fatal("onClose was not handed through")
	}
}

func TestOpenChildPropagatesTheBackendError(t *testing.T) {
	want := errors.New("boom")
	if _, err := OpenChild(&childBackend{err: want}, Config{}, nil, nil); !errors.Is(err, want) {
		t.Fatalf("err = %v, want %v", err, want)
	}
}
