// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

// Package capture answers one question for the live suites: where may a picture
// of somebody's screen be written?
//
// It carries no testing dependency and no build tag, so every backend's live
// tests reach the same answer through the same code instead of each keeping a
// copy of the rule.
package capture

import (
	"fmt"
	"os"
	"path/filepath"
)

// Env names the directory override.
const Env = "WINDOW_CAPTURE_DIR"

// Dir returns a directory that exists, is durable, and is inside no git work
// tree, with a phrase naming where the choice came from so an error message can
// be acted on.
//
// A capture of a whole output photographs whoever is running it, at work — their
// code, their mail, their tabs. These repositories are public, and a .gitignore
// entry is a safety net rather than a barrier: `git add -f`, a fresh clone, or
// any tool that does not consult it publishes the file anyway. So such captures
// do not go where they could be committed at all.
//
// Nor do they go somewhere that evaporates: the artefact exists SO THAT A
// PERSON CAN LOOK AT IT, and a temporary directory removed when the test ends is
// gone before anyone can open it. The default is the user's own configuration
// directory.
func Dir() (dir, chose string, err error) {
	dir, chose = os.Getenv(Env), Env
	if dir == "" {
		chose = "the default capture directory"
		base, uerr := os.UserConfigDir()
		if uerr != nil {
			return "", chose, fmt.Errorf("no user configuration directory to keep captures in: %w", uerr)
		}
		dir = filepath.Join(base, "go-widgets-window", "captures")
	}
	// Not reachable in an ordinary run -- Abs only fails when the working
	// directory cannot be read, and the default is already absolute -- but a
	// relative override with a deleted cwd would reach it, and answering a
	// half-resolved path is exactly how a capture escapes its directory.
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", chose, fmt.Errorf("%s (%q): %w", chose, dir, err)
	}
	if root := RepoRootOf(abs); root != "" {
		return "", chose, fmt.Errorf("%s (%q) is inside the git work tree at %s; a picture "+
			"of somebody's screen must never be written where it can be committed",
			chose, abs, root)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return "", chose, fmt.Errorf("%s (%q): %w", chose, abs, err)
	}
	return abs, chose, nil
}

// RepoRootOf returns the work tree dir is inside, or "" if it is in none.
//
// A .git that is a FILE rather than a directory is a worktree, and commits just
// as well — so both count.
func RepoRootOf(dir string) string {
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
