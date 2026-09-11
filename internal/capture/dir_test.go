// Copyright (c) the go-widgets/window authors. All rights reserved.
//
// SPDX-License-Identifier: BSD-3-Clause

package capture

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestDirRefusesARepository is the assertion that matters: a guard nobody has
// seen refuse is not a guard.
func TestDirRefusesARepository(t *testing.T) {
	here, err := filepath.Abs(".")
	if err != nil {
		t.Fatalf("Abs: %v", err)
	}
	t.Setenv(Env, here)
	dir, _, err := Dir()
	if err == nil {
		t.Fatalf("Dir accepted %s, which is inside this repository, and answered %q", here, dir)
	}
	if !strings.Contains(err.Error(), "committed") {
		t.Errorf("err = %v, want it to say why a repository is refused", err)
	}
}

func TestDirTakesTheOverrideAndMakesIt(t *testing.T) {
	want := filepath.Join(t.TempDir(), "shots")
	t.Setenv(Env, want)
	dir, chose, err := Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if dir != want {
		t.Errorf("Dir = %q, want %q", dir, want)
	}
	if chose != Env {
		t.Errorf("chose = %q, want the environment variable named", chose)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Errorf("Dir did not create %s: %v", dir, err)
	}
}

// With nothing set, the path a real run takes must land outside every
// repository — the case the override can never exercise.
func TestDirDefaultsOutsideEveryRepository(t *testing.T) {
	t.Setenv(Env, "")
	dir, chose, err := Dir()
	if err != nil {
		t.Fatalf("Dir: %v", err)
	}
	if root := RepoRootOf(dir); root != "" {
		t.Errorf("the default capture directory %s is inside the repository %s", dir, root)
	}
	if chose == Env {
		t.Errorf("chose = %q with nothing set, want the default named", chose)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Errorf("Dir did not create %s: %v", dir, err)
	}
}

func TestRepoRootOf(t *testing.T) {
	here, _ := filepath.Abs(".")
	if root := RepoRootOf(here); root == "" {
		t.Errorf("%s was not recognised as being inside a repository", here)
	}
	// The filesystem root has no .git above it, which is the loop's exit.
	if root := RepoRootOf(string(filepath.Separator)); root != "" {
		t.Errorf("the filesystem root was reported as the repository %s", root)
	}
	// A .git FILE counts: that is a worktree, and it commits just as well.
	wt := t.TempDir()
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: /elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	deep := filepath.Join(wt, "a", "b")
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}
	if root := RepoRootOf(deep); root != wt {
		t.Errorf("RepoRootOf(worktree) = %q, want %q", root, wt)
	}
}

// The two failures a run can really meet, since an untested error path is a
// promise nobody has heard kept.
func TestDirWhenThereIsNowhereToPutIt(t *testing.T) {
	// No configuration directory to derive a default from.
	t.Setenv(Env, "")
	t.Setenv("HOME", "")
	t.Setenv("XDG_CONFIG_HOME", "")
	if dir, _, err := Dir(); err == nil {
		t.Errorf("Dir answered %q with no configuration directory to put it in", dir)
	}
}

func TestDirWhenTheDirectoryCannotBeMade(t *testing.T) {
	base := t.TempDir()
	blocker := filepath.Join(base, "not-a-directory")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv(Env, filepath.Join(blocker, "shots"))
	if dir, _, err := Dir(); err == nil {
		t.Errorf("Dir answered %q under a path that is a file", dir)
	}
}
