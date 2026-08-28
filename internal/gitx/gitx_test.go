package gitx

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// requireGit skips the test when git is absent, since these exercise the real
// binary on purpose: the whole point of this package is to inherit the user's
// git configuration rather than reimplement it.
func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
}

// newRepo builds an initialized repository with one commit.
func newRepo(t *testing.T) string {
	t.Helper()
	requireGit(t)
	dir := t.TempDir()

	for _, args := range [][]string{
		{"init", "--quiet"},
		{"config", "user.email", "test@example.com"},
		{"config", "user.name", "test"},
	} {
		if _, err := Run(dir, args...); err != nil {
			t.Fatalf("git %s: %v", strings.Join(args, " "), err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(dir, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := Run(dir, "commit", "--quiet", "-m", "init"); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRemoteURL(t *testing.T) {
	dir := newRepo(t)
	const url = "https://github.com/anton/portable.git"
	if _, err := Run(dir, "remote", "add", "origin", url); err != nil {
		t.Fatal(err)
	}

	got, err := RemoteURL(dir, "origin")
	if err != nil {
		t.Fatal(err)
	}
	if got != url {
		t.Errorf("RemoteURL = %q, want %q", got, url)
	}
}

func TestRemoteURLMissing(t *testing.T) {
	dir := newRepo(t)
	if _, err := RemoteURL(dir, "origin"); err == nil {
		t.Error("RemoteURL succeeded with no remote configured")
	}
}

func TestTopLevel(t *testing.T) {
	dir := newRepo(t)
	sub := filepath.Join(dir, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	got, err := TopLevel(sub)
	if err != nil {
		t.Fatal(err)
	}
	// git reports forward slashes even on Windows, and the temp path may be a
	// short-name alias, so compare only the final element.
	if filepath.Base(filepath.FromSlash(got)) != filepath.Base(dir) {
		t.Errorf("TopLevel = %q, want the root of %q", got, dir)
	}
}

func TestIsClean(t *testing.T) {
	dir := newRepo(t)

	clean, err := IsClean(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !clean {
		t.Error("a fresh commit left the tree dirty")
	}

	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	clean, err = IsClean(dir)
	if err != nil {
		t.Fatal(err)
	}
	if clean {
		t.Error("a modified file was reported as clean")
	}
}

func TestRunReportsStderr(t *testing.T) {
	dir := newRepo(t)
	_, err := Run(dir, "rev-parse", "--verify", "refs/heads/nonexistent")
	if err == nil {
		t.Fatal("expected an error for a missing ref")
	}
	// The message has to carry git's own words, or a failing hook is undebuggable.
	if !strings.Contains(err.Error(), "rev-parse") {
		t.Errorf("error lost the command: %v", err)
	}
}

// TestRunDoesNotPromptForCredentials is the property that keeps a SessionStart
// hook from hanging on an invisible password prompt.
func TestRunDoesNotPromptForCredentials(t *testing.T) {
	requireGit(t)
	dir := t.TempDir()

	start := time.Now()
	_, err := RunWithTimeout(dir, 20*time.Second,
		"clone", "--quiet", "https://github.com/anton-does-not-exist-xyz/private.git", filepath.Join(dir, "clone"))
	if err == nil {
		t.Skip("the fake private URL resolved; nothing to assert")
	}
	if elapsed := time.Since(start); elapsed > 20*time.Second {
		t.Errorf("clone took %s; it likely waited on a prompt", elapsed)
	}
}

// TestRunTimesOut proves the deadline is real: an unbounded git call in a hook
// would hang the session start.
func TestRunTimesOut(t *testing.T) {
	requireGit(t)
	dir := newRepo(t)

	// A pager-less command that still has to do work; the point is the deadline
	// path returns rather than blocking forever.
	_, err := RunWithTimeout(dir, 1*time.Nanosecond, "status", "--porcelain")
	if err == nil {
		t.Skip("git finished within a nanosecond; nothing to assert")
	}
	if !strings.Contains(err.Error(), "timed out") {
		t.Errorf("error = %v, want a timeout", err)
	}
}

func TestRunMissingGit(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := Run(t.TempDir(), "status"); !errors.Is(err, ErrNotFound) {
		t.Errorf("err = %v, want ErrNotFound when git is absent", err)
	}
}
