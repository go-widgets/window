// Copyright (c) the go-widgets/window authors. All rights reserved.
// SPDX-License-Identifier: BSD-3-Clause

//go:build darwin

package cocoa

import (
	"errors"
	"testing"
)

// TestAnEmptyDisplayListIsAnErrorAndNotAnAnswer.
//
// ⛔⛔ liveDisplays USED TO ANSWER (nil, nil) WHEN THE WINDOW SERVER COUNTED
// ZERO, so "I could not tell you" and "there are none" were the same value, and
// Screens() passed the second one on. A machine running a window server has at
// least one display; CGGetActiveDisplayList counting zero with no CGError means
// this process could not see the list.
//
// Measured, 2026-09-08, twice in one evening on a desk somebody was wearing.
// go-xrkit/desk looks its display up by name and quit with
//
//	desk: "VITURE Beast" is not attached any more; there is  -- stopping
//
// Nothing after "there is": that message names every attached display, and it
// named none. A person lost their screens over an answer this package never had.
//
// ⭐ THE COUNT IS FAKED RATHER THAN THE MACHINE EMPTIED, which is the only way
// to reach the branch: unplugging every display from a Mac is not a test, and a
// runner with one display would never take it.
func TestAnEmptyDisplayListIsAnErrorAndNotAnAnswer(t *testing.T) {
	if err := loadCoreGraphics(); err != nil {
		t.Skipf("CoreGraphics unavailable: %v", err)
	}
	saved := cgGetActiveDisplayList
	t.Cleanup(func() { cgGetActiveDisplayList = saved })
	// A window server that counts nothing, and reports no error doing it --
	// exactly the shape that was believed.
	cgGetActiveDisplayList = func(_ uint32, _ *uint32, count *uint32) int32 {
		*count = 0
		return 0
	}

	ds, err := liveDisplays()
	if !errors.Is(err, ErrDisplayList) {
		t.Fatalf("liveDisplays() with a count of 0 returned %v, want ErrDisplayList", err)
	}
	if len(ds) != 0 {
		t.Errorf("liveDisplays() returned %d displays alongside its error", len(ds))
	}

	// ⛔ AND Screens() MUST NOT TRANSLATE IT BACK INTO A FACT. This is the call
	// the desk makes, and the one whose empty slice ended the session.
	ss, err := Screens()
	if err == nil {
		t.Fatalf("Screens() reported success with %d screens; an empty list is "+
			"not a Mac with no screens, it is a read that failed", len(ss))
	}
	if !errors.Is(err, ErrDisplayList) {
		t.Errorf("Screens() returned %v, want it to wrap ErrDisplayList", err)
	}
	if len(ss) != 0 {
		t.Errorf("Screens() returned %d screens alongside its error", len(ss))
	}
}
