// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"errors"
	"testing"

	"github.com/go-widgets/tray"
)

type noMenuBackend struct{ Backend }

func (noMenuBackend) String() string { return "nomenu" }

type menuBackend struct {
	noMenuBackend
	name string
	got  *tray.Menu
	err  error
}

func (b *menuBackend) SetMenuBar(name string, m *tray.Menu) error {
	b.name, b.got = name, m
	return b.err
}

func TestSetMenuBarWithoutTheCapabilityIsNotSupported(t *testing.T) {
	err := SetMenuBar(noMenuBackend{}, "App", tray.NewMenu())
	if !errors.Is(err, ErrNotSupported) {
		t.Fatalf("SetMenuBar = %v, want ErrNotSupported", err)
	}
}

func TestSetMenuBarDelegatesToTheBackend(t *testing.T) {
	m := tray.NewMenu()
	boom := errors.New("boom")
	b := &menuBackend{err: boom}
	if err := SetMenuBar(b, "App", m); !errors.Is(err, boom) {
		t.Fatalf("SetMenuBar = %v, want the back-end's error", err)
	}
	if b.name != "App" || b.got != m {
		t.Errorf("the back-end saw (%q, %p), want (App, %p)", b.name, b.got, m)
	}
}
