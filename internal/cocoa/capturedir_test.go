// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package cocoa

import (
	"os"
	"path/filepath"
	"testing"
)

// captureDir is where a picture of somebody's screen may be written: somewhere
// durable, and never inside a repository.
//
// A live test that photographs a panel photographs whoever is running it, at
// work — their code, their mail, their tabs. This repository is public, and a
// .gitignore entry is a safety net rather than a barrier: `git add -f`, a fresh
// clone, or any tool that does not consult it publishes the file anyway. So
// captures do not go where they could be committed at all.
//
// Nor do they go somewhere that evaporates: the artefact exists SO THAT A PERSON
// CAN LOOK AT IT, and t.TempDir() is removed when the test ends. The default is
// the user's own application support directory, which survives both the test and
// the machine restarting. WINDOW_CAPTURE_DIR overrides it.
//
// This file carries NO build tag on purpose. The live suite is behind
// `darwin && integration`, and a guard that only compiles where the live suite
// runs is a guard no CI lane ever exercises.
func captureDir(t *testing.T) string {
	t.Helper()
	const env = "WINDOW_CAPTURE_DIR"
	dir, chose := os.Getenv(env), env
	if dir == "" {
		chose = "the default capture directory"
		base, err := os.UserConfigDir()
		if err != nil {
			t.Fatalf("no user configuration directory to keep captures in: %v", err)
		}
		dir = filepath.Join(base, "go-widgets-window", "captures")
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		t.Fatalf("%s (%q): %v", chose, dir, err)
	}
	if root := repoRootOf(abs); root != "" {
		t.Fatalf("%s (%q) is inside the git work tree at %s; a picture of somebody's "+
			"screen must never be written where it can be committed", chose, abs, root)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		t.Fatalf("%s (%q): %v", chose, abs, err)
	}
	return abs
}

// repoRootOf returns the work tree dir is inside, or "" if it is in none.
//
// A .git that is a FILE rather than a directory is a worktree, and commits just
// as well — so both count.
func repoRootOf(dir string) string {
	for d := dir; ; {
		if fi, err := os.Stat(filepath.Join(d, ".git")); err == nil &&
			(fi.IsDir() || fi.Mode().IsRegular()) {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
}

// TestCaptureDirRefusesARepository is the assertion that matters: a guard nobody
// has seen refuse is not a guard. It runs on every platform and every lane.
func TestCaptureDirRefusesARepository(t *testing.T) {
	// This source file is inside the work tree, so its directory must be
	// refused — whatever the checkout is called and wherever it lives.
	here, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	if root := repoRootOf(here); root == "" {
		t.Fatalf("%s was not recognised as being inside a repository", here)
	}
	// And somewhere that is not: the filesystem root has no .git above it.
	if root := repoRootOf(string(filepath.Separator)); root != "" {
		t.Errorf("the filesystem root was reported as the repository %s", root)
	}
}

// TestCaptureDirDefaultsOutsideEveryRepository checks the path a run with no
// environment set actually takes.
func TestCaptureDirDefaultsOutsideEveryRepository(t *testing.T) {
	t.Setenv("WINDOW_CAPTURE_DIR", "")
	dir := captureDir(t)
	if root := repoRootOf(dir); root != "" {
		t.Errorf("the default capture directory %s is inside the repository %s", dir, root)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Errorf("the default capture directory was not created: %v", err)
	}
}
