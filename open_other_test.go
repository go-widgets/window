// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

//go:build !linux

package window

import (
	"errors"
	"testing"
)

func TestOpenUnsupported(t *testing.T) {
	w, err := Open(Config{Title: "x"})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("Open off Linux should return ErrUnsupported, got %v", err)
	}
	if w != nil {
		t.Fatalf("Open off Linux should return nil window")
	}
}
