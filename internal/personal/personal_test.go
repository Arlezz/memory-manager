package personal

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Arlezz/memory-manager/internal/config"
	"github.com/Arlezz/memory-manager/internal/gitx"
)

func requireGit(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not on PATH")
	}
}

// fixture is a bare "remote" plus the clone Open will produce, wired through a
// CLAUDE_CONFIG_DIR so the real clone path is used.
type fixture struct {
	t    *testing.T
	bare string
	cfg  config.Config
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	requireGit(t)

	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude"))

	bare := filepath.Join(root, "remote.git")
	if _, err := gitx.Run("", "init", "--bare", "--quiet", "--initial-branch=main", bare); err != nil {
		t.Fatalf("init bare: %v", err)
	}
	seed(t, bare)

	return &fixture{t: t, bare: bare, cfg: config.Config{PersonalRepo: bare, PersonalBranch: "main"}}
}

// seed gives the bare repo an initial commit, since a clone of an empty
// repository behaves differently and is covered separately.
func seed(t *testing.T, bare string) {
	t.Helper()
	work := filepath.Join(t.TempDir(), "seed")
	if _, err := gitx.Run("", "clone", "--quiet", bare, work); err != nil {
		t.Fatalf("clone for seed: %v", err)
	}
	writeMemory(t, filepath.Join(work, "global", "seeded.md"), "seeded")
	commitAll(t, work, "seed")
	if _, err := gitx.Run(work, "push", "--quiet", "origin", "HEAD:refs/heads/main"); err != nil {
		t.Fatalf("push seed: %v", err)
	}
}

func writeMemory(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	name := strings.TrimSuffix(filepath.Base(path), ".md")
	content := "---\nname: " + name + "\ndescription: " + body + "\nmetadata:\n  type: feedback\n---\n\n" + body + "\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func commitAll(t *testing.T, dir, message string) {
	t.Helper()
	if _, err := gitx.Run(dir, "add", "-A"); err != nil {
		t.Fatal(err)
	}
	if _, err := gitx.Run(dir,
		"-c", "user.email=test@example.com", "-c", "user.name=test",
		"commit", "--quiet", "-m", message); err != nil {
		t.Fatal(err)
	}
}

func TestOpenClonesAndPulls(t *testing.T) {
	f := newFixture(t)

	repo, warn, err := Open(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !repo.Present {
		t.Fatalf("clone did not happen: %s", warn)
	}
	if _, err := os.Stat(filepath.Join(repo.Path, "global", "seeded.md")); err != nil {
		t.Errorf("seeded memory missing from the clone: %v", err)
	}

	// A second Open must reuse the existing clone rather than clone again.
	again, _, err := Open(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if again.Path != repo.Path || !again.Present {
		t.Errorf("second Open = %+v, want the same clone", again)
	}
	if warn, err := again.Pull(); err != nil || warn != "" {
		t.Errorf("Pull on a clean clone gave (%q, %v), want no warning", warn, err)
	}
}

func TestOpenDisabledWithoutRepo(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(t.TempDir(), "claude"))

	repo, warn, err := Open(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if repo.Present {
		t.Error("Present = true with no personal_repo configured")
	}
	if !strings.Contains(warn, "disabled") {
		t.Errorf("warn = %q, want it to say the layer is disabled", warn)
	}
}

// TestOpenUnreachableDegrades is the rule that a hook never blocks: an
// unreachable remote is a warning, not an error.
func TestOpenUnreachableDegrades(t *testing.T) {
	requireGit(t)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(t.TempDir(), "claude"))

	repo, warn, err := Open(config.Config{PersonalRepo: "https://gitlab.invalid.example/nope.git"})
	if err != nil {
		t.Fatalf("an unreachable remote must not be a hard error: %v", err)
	}
	if repo.Present {
		t.Error("Present = true for a failed clone")
	}
	if !strings.Contains(warn, "unavailable") {
		t.Errorf("warn = %q, want it to report the layer as unavailable", warn)
	}
}

// TestPullSkipsDirtyClone protects local work: a hook cannot resolve a conflict,
// so it must not start one.
func TestPullSkipsDirtyClone(t *testing.T) {
	f := newFixture(t)
	repo, _, err := Open(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	writeMemory(t, filepath.Join(repo.Path, "global", "uncommitted.md"), "local work")

	warn, err := repo.Pull()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(warn, "uncommitted local changes") {
		t.Errorf("warn = %q, want the dirty tree reported", warn)
	}
	if _, err := os.Stat(filepath.Join(repo.Path, "global", "uncommitted.md")); err != nil {
		t.Errorf("local work was lost: %v", err)
	}
}

func TestCommitStagesOnlyGivenPaths(t *testing.T) {
	f := newFixture(t)
	repo, _, err := Open(f.cfg)
	if err != nil {
		t.Fatal(err)
	}

	writeMemory(t, filepath.Join(repo.Path, "global", "mine.md"), "written by the tool")
	// Something the user was doing in the clone that we must not commit.
	writeMemory(t, filepath.Join(repo.Path, "global", "theirs.md"), "user work in progress")

	if err := repo.Commit([]string{"global/mine.md"}, "memory: 1 written"); err != nil {
		t.Fatal(err)
	}

	committed, err := gitx.Run(repo.Path, "show", "--name-only", "--format=", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(committed, "mine.md") {
		t.Errorf("the requested file was not committed: %q", committed)
	}
	if strings.Contains(committed, "theirs.md") {
		t.Errorf("committed a file we were not asked to: %q", committed)
	}
}

func TestCommitRecordsDeletions(t *testing.T) {
	f := newFixture(t)
	repo, _, err := Open(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(repo.Path, "global", "seeded.md")); err != nil {
		t.Fatal(err)
	}

	if err := repo.Commit([]string{"global/seeded.md"}, "memory: 1 removed"); err != nil {
		t.Fatal(err)
	}
	tracked, err := gitx.Run(repo.Path, "ls-files", "global/seeded.md")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(tracked) != "" {
		t.Error("the deletion was not committed")
	}
}

func TestCommitNothingToCommit(t *testing.T) {
	f := newFixture(t)
	repo, _, err := Open(f.cfg)
	if err != nil {
		t.Fatal(err)
	}

	if err := repo.Commit(nil, "empty"); !errors.Is(err, ErrNothingToCommit) {
		t.Errorf("err = %v, want ErrNothingToCommit for no paths", err)
	}
	if err := repo.Commit([]string{"global/seeded.md"}, "unchanged"); !errors.Is(err, ErrNothingToCommit) {
		t.Errorf("err = %v, want ErrNothingToCommit for an unchanged file", err)
	}
}

func TestPush(t *testing.T) {
	f := newFixture(t)
	repo, _, err := Open(f.cfg)
	if err != nil {
		t.Fatal(err)
	}
	writeMemory(t, filepath.Join(repo.Path, "global", "pushed.md"), "goes to the remote")
	if err := repo.Commit([]string{"global/pushed.md"}, "memory: 1 written"); err != nil {
		t.Fatal(err)
	}

	if err := repo.Push(); err != nil {
		t.Fatal(err)
	}
	remote, err := gitx.Run(f.bare, "ls-tree", "-r", "--name-only", "main")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(remote, "pushed.md") {
		t.Errorf("the remote does not have the file: %q", remote)
	}
}

// TestUnpushedCountsStrandedCommits is the write-back hook being cancelled
// between its commit and its push: the clone looks finished and only the commit
// count says the memory never left the machine.
func TestUnpushedCountsStrandedCommits(t *testing.T) {
	f := newFixture(t)
	repo, _, err := Open(f.cfg)
	if err != nil {
		t.Fatal(err)
	}

	if n, err := repo.Unpushed(); err != nil || n != 0 {
		t.Fatalf("a fresh clone reports %d, %v; want 0, nil", n, err)
	}

	writeMemory(t, filepath.Join(repo.Path, "global", "stranded.md"), "never reached the remote")
	if err := repo.Commit([]string{"global/stranded.md"}, "memory: 1 written"); err != nil {
		t.Fatal(err)
	}
	writeMemory(t, filepath.Join(repo.Path, "global", "also-stranded.md"), "nor did this one")
	if err := repo.Commit([]string{"global/also-stranded.md"}, "memory: 1 written"); err != nil {
		t.Fatal(err)
	}

	n, err := repo.Unpushed()
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Errorf("Unpushed = %d, want 2", n)
	}

	if err := repo.Push(); err != nil {
		t.Fatal(err)
	}
	if n, err := repo.Unpushed(); err != nil || n != 0 {
		t.Errorf("after a push Unpushed = %d, %v; want 0, nil", n, err)
	}
}

// TestUnpushedWithoutUpstream covers the very first run, where there is no
// remote-tracking branch to compare against. Reporting an error there would
// turn a normal first session into a warning.
func TestUnpushedWithoutUpstream(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude"))

	bare := filepath.Join(root, "empty.git")
	if _, err := gitx.Run("", "init", "--bare", "--quiet", "--initial-branch=main", bare); err != nil {
		t.Fatal(err)
	}
	repo, warn, err := Open(config.Config{PersonalRepo: bare, PersonalBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if !repo.Present {
		t.Fatalf("clone failed: %s", warn)
	}

	writeMemory(t, filepath.Join(repo.Path, "global", "first.md"), "the very first memory")
	if err := repo.Commit([]string{"global/first.md"}, "memory: 1 written"); err != nil {
		t.Fatal(err)
	}
	if n, err := repo.Unpushed(); err != nil || n != 0 {
		t.Errorf("Unpushed = %d, %v; want 0, nil with no upstream", n, err)
	}
}

// TestUnpushedWithoutCloneIsZero keeps the absent personal layer quiet: it is
// already reported as a warning of its own and must not also look unpushed.
func TestUnpushedWithoutCloneIsZero(t *testing.T) {
	n, err := Repo{Path: filepath.Join(t.TempDir(), "nothing")}.Unpushed()
	if err != nil || n != 0 {
		t.Errorf("Unpushed = %d, %v; want 0, nil", n, err)
	}
}

// TestPushRebasesWhenRemoteMoved is the everyday case for two machines: one fact
// per file means the rebase almost always succeeds without help.
func TestPushRebasesWhenRemoteMoved(t *testing.T) {
	f := newFixture(t)
	repo, _, err := Open(f.cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Another machine pushes a different memory first.
	other := filepath.Join(t.TempDir(), "other")
	if _, err := gitx.Run("", "clone", "--quiet", f.bare, other); err != nil {
		t.Fatal(err)
	}
	writeMemory(t, filepath.Join(other, "global", "from-laptop.md"), "written elsewhere")
	commitAll(t, other, "from the laptop")
	if _, err := gitx.Run(other, "push", "--quiet"); err != nil {
		t.Fatal(err)
	}

	writeMemory(t, filepath.Join(repo.Path, "global", "from-desktop.md"), "written here")
	if err := repo.Commit([]string{"global/from-desktop.md"}, "memory: 1 written"); err != nil {
		t.Fatal(err)
	}

	if err := repo.Push(); err != nil {
		t.Fatalf("Push should have rebased and succeeded: %v", err)
	}
	remote, err := gitx.Run(f.bare, "ls-tree", "-r", "--name-only", "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"from-laptop.md", "from-desktop.md"} {
		if !strings.Contains(remote, want) {
			t.Errorf("the remote lost %s: %q", want, remote)
		}
	}
}

// TestPushConflictLeavesCloneClean is the important failure path: on a real
// conflict nothing is published and no rebase is left in progress, because the
// next run would otherwise fail on a repository stuck mid-operation.
func TestPushConflictLeavesCloneClean(t *testing.T) {
	f := newFixture(t)
	repo, _, err := Open(f.cfg)
	if err != nil {
		t.Fatal(err)
	}

	other := filepath.Join(t.TempDir(), "other")
	if _, err := gitx.Run("", "clone", "--quiet", f.bare, other); err != nil {
		t.Fatal(err)
	}
	// Both machines edit the same memory differently.
	writeMemory(t, filepath.Join(other, "global", "seeded.md"), "laptop version")
	commitAll(t, other, "laptop edit")
	if _, err := gitx.Run(other, "push", "--quiet"); err != nil {
		t.Fatal(err)
	}

	writeMemory(t, filepath.Join(repo.Path, "global", "seeded.md"), "desktop version")
	if err := repo.Commit([]string{"global/seeded.md"}, "desktop edit"); err != nil {
		t.Fatal(err)
	}

	err = repo.Push()
	if err == nil {
		t.Fatal("Push succeeded despite a conflicting edit")
	}
	var conflict *ErrRebaseConflict
	if !errors.As(err, &conflict) {
		t.Fatalf("err = %v (%T), want ErrRebaseConflict", err, err)
	}

	// No rebase in progress, or every later run breaks.
	if _, statErr := os.Stat(filepath.Join(repo.Path, ".git", "rebase-merge")); !os.IsNotExist(statErr) {
		t.Error("a rebase was left in progress")
	}
	if _, statErr := os.Stat(filepath.Join(repo.Path, ".git", "rebase-apply")); !os.IsNotExist(statErr) {
		t.Error("a rebase-apply was left in progress")
	}
	// The remote must still hold only the other machine's version.
	remote, err := gitx.Run(f.bare, "show", "main:global/seeded.md")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(remote, "laptop version") {
		t.Errorf("the remote was modified by a failed push: %q", remote)
	}
}

// TestOpenEmptyRepositoryWithBranch is the very first run: a repository the user
// just created has no branches, so "clone --branch main" cannot work.
func TestOpenEmptyRepositoryWithBranch(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude"))

	bare := filepath.Join(root, "empty.git")
	if _, err := gitx.Run("", "init", "--bare", "--quiet", bare); err != nil {
		t.Fatal(err)
	}

	repo, warn, err := Open(config.Config{PersonalRepo: bare, PersonalBranch: "main"})
	if err != nil {
		t.Fatal(err)
	}
	if !repo.Present {
		t.Fatalf("clone of a branchless repository failed: %s", warn)
	}

	// The configured branch name must be adopted, so the first push creates it
	// instead of whatever init.defaultBranch happens to be on this machine.
	head, err := gitx.Run(repo.Path, "symbolic-ref", "--short", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(head) != "main" {
		t.Errorf("HEAD = %q, want main", strings.TrimSpace(head))
	}

	writeMemory(t, filepath.Join(repo.Path, "global", "first.md"), "the very first memory")
	if err := repo.Commit([]string{"global/first.md"}, "memory: 1 written"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Push(); err != nil {
		t.Fatalf("first push failed: %v", err)
	}
	branches, err := gitx.Run(bare, "branch", "--format=%(refname:short)")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(branches, "main") {
		t.Errorf("branches = %q, want main", branches)
	}
}

// TestPushToEmptyRepository covers the first push when no branch is configured
// and there is no upstream yet.
func TestPushToEmptyRepository(t *testing.T) {
	requireGit(t)
	root := t.TempDir()
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(root, "claude"))

	bare := filepath.Join(root, "empty.git")
	if _, err := gitx.Run("", "init", "--bare", "--quiet", "--initial-branch=main", bare); err != nil {
		t.Fatal(err)
	}

	repo, warn, err := Open(config.Config{PersonalRepo: bare})
	if err != nil {
		t.Fatal(err)
	}
	if !repo.Present {
		t.Fatalf("clone of an empty repo failed: %s", warn)
	}

	writeMemory(t, filepath.Join(repo.Path, "global", "first.md"), "the very first memory")
	if err := repo.Commit([]string{"global/first.md"}, "memory: 1 written"); err != nil {
		t.Fatal(err)
	}
	if err := repo.Push(); err != nil {
		t.Fatalf("first push failed: %v", err)
	}

	branches, err := gitx.Run(bare, "branch", "--format=%(refname:short)")
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(branches) == "" {
		t.Error("the first push created no branch on the remote")
	}
}

// TestPushRebasesWithNoGlobalGitIdentity is the regression test for a bug that
// could only ever show up off the development machine: Commit passed an identity
// per command but the rebase inside Push did not, so anywhere without a global
// git identity — every CI runner, and any fresh machine — the rebase failed and
// was then reported as a conflict that did not exist.
func TestPushRebasesWithNoGlobalGitIdentity(t *testing.T) {
	t.Setenv("GIT_CONFIG_GLOBAL", os.DevNull)
	t.Setenv("GIT_CONFIG_SYSTEM", os.DevNull)

	f := newFixture(t)
	repo, _, err := Open(f.cfg)
	if err != nil {
		t.Fatal(err)
	}

	// Another machine pushes first, so the rebase has something to replay onto.
	other := filepath.Join(t.TempDir(), "other")
	if _, err := gitx.Run("", "clone", "--quiet", f.bare, other); err != nil {
		t.Fatal(err)
	}
	writeMemory(t, filepath.Join(other, "global", "from-laptop.md"), "written elsewhere")
	commitAll(t, other, "from the laptop")
	if _, err := gitx.Run(other, "push", "--quiet"); err != nil {
		t.Fatal(err)
	}

	writeMemory(t, filepath.Join(repo.Path, "global", "from-desktop.md"), "written here")
	if err := repo.Commit([]string{"global/from-desktop.md"}, "memory: 1 written"); err != nil {
		t.Fatal(err)
	}

	if err := repo.Push(); err != nil {
		t.Fatalf("Push must rebase with no git identity configured: %v", err)
	}
	remote, err := gitx.Run(f.bare, "ls-tree", "-r", "--name-only", "main")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"from-laptop.md", "from-desktop.md"} {
		if !strings.Contains(remote, want) {
			t.Errorf("the remote lost %s: %q", want, remote)
		}
	}
}

// TestPushReportsUnreachableRemoteAsFailureNotConflict pins the distinction the
// error types exist for. Push runs unattended from a SessionEnd hook, so calling
// an unreachable remote a conflict sends the user looking for conflicting files
// that were never there.
func TestPushReportsUnreachableRemoteAsFailureNotConflict(t *testing.T) {
	f := newFixture(t)
	repo, _, err := Open(f.cfg)
	if err != nil {
		t.Fatal(err)
	}

	writeMemory(t, filepath.Join(repo.Path, "global", "note.md"), "written here")
	if err := repo.Commit([]string{"global/note.md"}, "memory: 1 written"); err != nil {
		t.Fatal(err)
	}

	// The upstream stays configured, so Push reaches the rebase and fails there.
	gone := filepath.Join(t.TempDir(), "gone.git")
	if _, err := gitx.Run(repo.Path, "remote", "set-url", "origin", gone); err != nil {
		t.Fatal(err)
	}

	err = repo.Push()
	if err == nil {
		t.Fatal("Push succeeded against a remote that does not exist")
	}
	var conflict *ErrRebaseConflict
	if errors.As(err, &conflict) {
		t.Errorf("an unreachable remote was reported as a conflict: %v", err)
	}
	var failed *ErrRebaseFailed
	if !errors.As(err, &failed) {
		t.Errorf("error = %T (%v), want *ErrRebaseFailed", err, err)
	}
}
