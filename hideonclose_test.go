// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"errors"
	"testing"
)

// Config.HideOnClose on a back-end that cannot hide its window is an error the
// caller can test for, never a window that quits on close after promising not to.
func TestRefuseHideOnClose(t *testing.T) {
	if err := refuseHideOnClose(Config{}, "GTK"); err != nil {
		t.Fatalf("no HideOnClose asked: %v, want nil", err)
	}
	err := refuseHideOnClose(Config{HideOnClose: true}, "GTK")
	if !errors.Is(err, ErrNotSupported) {
		t.Fatalf("HideOnClose on GTK: %v, want ErrNotSupported", err)
	}
}
