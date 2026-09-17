package personal

import (
	"path/filepath"
	"testing"

	"github.com/Arlezz/memory-manager/internal/gitx"
)

// TestDivergenceTellsAheadFromConflict is A-14. "ahead 1" and "ahead 1, behind
// 1" printed the same line and the same advice, and for the second one that
// advice — run push — fails identically every time: the user follows it, the
// push fails on the rebase, the count persists, the next session repeats the
// suggestion. The two numbers are what lets the caller stop saying that.
func TestDivergenceTellsAheadFromConflict(t *testing.T) {
	f := newFixture(t)

	repo, _, err := Open(f.cfg)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// Nothing has happened yet.
	ahead, behind, err := repo.Divergence()
	if err != nil {
		t.Fatalf("Divergence: %v", err)
	}
	if ahead != 0 || behind != 0 {
		t.Fatalf("a freshly cloned repo is ahead %d behind %d, want 0 and 0", ahead, behind)
	}

	// This machine writes a memory and the run dies before the network step,
	// which is the cancellation that happens here as a matter of routine.
	writeMemory(t, filepath.Join(repo.Path, "global", "local.md"), "written on this machine")
	commitAll(t, repo.Path, "local")

	if ahead, behind, err = repo.Divergence(); err != nil {
		t.Fatal(err)
	}
	if ahead != 1 || behind != 0 {
		t.Errorf("after a local commit: ahead %d behind %d, want 1 and 0", ahead, behind)
	}

	// Another machine edits the same memory and pushes first.
	other := filepath.Join(t.TempDir(), "other")
	if _, err := gitx.Run("", "clone", "--quiet", f.bare, other); err != nil {
		t.Fatalf("clone the second device: %v", err)
	}
	writeMemory(t, filepath.Join(other, "global", "local.md"), "written on the other machine")
	commitAll(t, other, "other")
	if _, err := gitx.Run(other, "push", "--quiet", "origin", "HEAD:refs/heads/main"); err != nil {
		t.Fatalf("push from the second device: %v", err)
	}
	// Fetch is what makes the remote commit visible to a local-refs comparison.
	if _, err := gitx.Run(repo.Path, "fetch", "--quiet"); err != nil {
		t.Fatalf("fetch: %v", err)
	}

	ahead, behind, err = repo.Divergence()
	if err != nil {
		t.Fatal(err)
	}
	if ahead != 1 || behind != 1 {
		t.Errorf("after both sides commit: ahead %d behind %d, want 1 and 1", ahead, behind)
	}

	// Unpushed keeps answering the narrower question its callers already ask.
	if n, err := repo.Unpushed(); err != nil || n != 1 {
		t.Errorf("Unpushed = %d, %v; want 1, nil", n, err)
	}
}

// TestDivergenceWithoutUpstreamIsZero keeps a clone of a repository that has
// never been pushed to from reporting a backlog that does not exist.
func TestDivergenceWithoutUpstreamIsZero(t *testing.T) {
	requireGit(t)

	dir := filepath.Join(t.TempDir(), "solo")
	if _, err := gitx.Run("", "init", "--quiet", "--initial-branch=main", dir); err != nil {
		t.Fatalf("init: %v", err)
	}
	writeMemory(t, filepath.Join(dir, "global", "first.md"), "first")
	commitAll(t, dir, "first")

	repo := Repo{Path: dir, Present: true}
	ahead, behind, err := repo.Divergence()
	if err != nil {
		t.Fatalf("Divergence: %v", err)
	}
	if ahead != 0 || behind != 0 {
		t.Errorf("with no upstream: ahead %d behind %d, want 0 and 0", ahead, behind)
	}
}
