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
	"os"

	"github.com/go-appdirs/outdir"
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
	// ⛔ The decision moved to go-appdirs/outdir, which owns the question and
	// is now used by go-macos/screencapture and go-mswin/screencapture too.
	// The sixty lines that were here also lived in both of those and in
	// go-aiquota/tray, and adopting the shared one CLOSED A HOLE: this copy
	// walked up from the path as given, resolving nothing, so a capture
	// directory reached through a symbolic link found no work tree and was
	// accepted. outdir resolves the path first and refuses it.
	//
	// The phrase stays here because it is this package's, not outdir's: an
	// error a person can act on has to say WHICH choice produced the path.
	chose = "the default capture directory"
	if os.Getenv(Env) != "" {
		chose = Env
	}
	dir, err = outdir.Ensure(outdir.Spec{
		App: "go-widgets-window",
		Env: Env,
		Sub: "captures",
	})
	if err != nil {
		return "", chose, err
	}
	return dir, chose, nil
}

// RepoRootOf returns the work tree dir is inside, or "" if it is in none.
//
// A .git that is a FILE rather than a directory is a worktree, and commits just
// as well — so both count.
func RepoRootOf(dir string) string { return outdir.RepoRootOf(dir) }
