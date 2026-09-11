// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package window

import (
	"testing"

	"github.com/go-widgets/window/internal/capture"
)

// captureDir is where a picture of a whole OUTPUT may be written: somewhere
// durable, and never inside a repository. The rule itself lives in
// internal/capture, so this package and internal/cocoa cannot drift apart on it.
//
// It is for the grim captures. `grim <file>` with no geometry photographs the
// entire output — under headless sway in CI that is only the test window, but a
// person running the live suite in their own session photographs their screen:
// their code, their mail, their tabs. These repositories are public and a
// .gitignore entry is a safety net rather than a barrier, so such a capture does
// not go where it could be committed at all.
//
// The `import -window <id>` captures are a different thing and are left where
// they are: they photograph the test's OWN window and nothing else, and this
// project keeps dated proof images as reviewable evidence on purpose.
//
// This file carries NO build tag, on purpose. The live suite is behind
// `integration && linux`, and a guard that only compiles where the live suite
// runs is a guard no CI lane ever exercises.
func captureDir(t *testing.T) string {
	t.Helper()
	dir, chose, err := capture.Dir()
	if err != nil {
		t.Fatalf("%s: %v", chose, err)
	}
	return dir
}

// TestCaptureDirIsOutsideThisRepository is the assertion that matters: a guard
// nobody has seen hold is not a guard. It runs on every platform and lane.
func TestCaptureDirIsOutsideThisRepository(t *testing.T) {
	dir := captureDir(t)
	if root := capture.RepoRootOf(dir); root != "" {
		t.Fatalf("captures would be written to %s, inside the repository %s", dir, root)
	}
}
