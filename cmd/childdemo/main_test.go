// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package main

import (
	"testing"

	"github.com/go-widgets/toolkit"
	"github.com/go-widgets/window"
)

// noChild is a backend without the secondary-window capability.
type noChild struct{}

func (noChild) Run(toolkit.Widget) error { return nil }
func (noChild) Close() error             { return nil }
func (noChild) Size() (int, int)         { return 1, 1 }
func (noChild) String() string           { return "noChild" }

func TestOpenSaysWhyItCannot(t *testing.T) {
	status := toolkit.NewLabel("x")
	var form window.Child
	open(noChild{}, status, &form)
	if form != nil || status.Text().Get() == "second window open" {
		t.Fatalf("a window that could not open was reported open: %q", status.Text().Get())
	}
}

func TestScenesBuild(t *testing.T) {
	if mainScene(noChild{}) == nil || formScene(func() {}) == nil {
		t.Fatal("nil scene")
	}
}
