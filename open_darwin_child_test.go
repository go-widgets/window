// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package window

import (
	"errors"
	"testing"

	"github.com/go-widgets/window/internal/cocoa"
)

func TestOpenReportsAFailedNSWindow(t *testing.T) {
	old := newCocoaWindow
	defer func() { newCocoaWindow = old }()
	boom := errors.New("no NSWindow")
	newCocoaWindow = func(cocoa.Options) (*cocoa.Window, error) { return nil, boom }
	if b, err := Open(Config{}); err != boom || b != nil {
		t.Fatalf("Open = %v, %v; want nil and the creation error", b, err)
	}
}

func TestCocoaOptionsCarryTheConfig(t *testing.T) {
	o := cocoaOptions(Config{Title: "t", Width: 3, Height: 4, FixedSize: true, HideOnClose: true})
	if o.Title != "t" || o.Width != 3 || o.Height != 4 || !o.FixedSize || !o.HideOnClose {
		t.Fatalf("options = %+v", o)
	}
}
