// Package personal manages the local clone of the private personal memory repo.
//
// Every network operation here is best-effort: a failed fetch degrades to the
// clone already on disk and returns a warning. Blocking a session because a
// remote is unreachable would make the tool worse than the manual workaround it
// replaces.
package personal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Arlezz/memory-manager/internal/config"
	"github.com/Arlezz/memory-manager/internal/gitx"
)

// Repo is a local personal memory clone.
type Repo struct {
	// Path is the clone directory. It may not exist yet.
	Path string
	// Present reports whether the clone exists on disk.
	Present bool
}

// Open locates the clone described by cfg, cloning it if absent.
//
// The returned warning is non-fatal and meant for the session summary; a
// non-nil error means the personal layer is unusable this run.
func Open(cfg config.Config) (Repo, string, error) {
	path, err := config.PersonalClonePath()
	if err != nil {
		return Repo{}, "", err
	}
	r := Repo{Path: path}

	if cfg.PersonalRepo == "" {
		return r, "personal layer disabled: no personal_repo in config", nil
	}

	if _, err := os.Stat(filepath.Join(path, ".git")); err == nil {
		r.Present = true
		return r, "", nil
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return r, "", err
	}
	if err := clone(cfg, path); err != nil {
		if errors.Is(err, gitx.ErrNotFound) {
			return r, "", err
		}
		// No clone and no network: the personal layer is simply absent this run.
		return r, fmt.Sprintf("personal layer unavailable: %v", err), nil
	}
	r.Present = true
	return r, "", nil
}

// clone fetches the personal repository, tolerating a brand new empty one.
//
// A repository the user just created on GitHub has no branches at all, so
// "clone --branch main" fails on it. That is the very first run of the tool, so
// it has to work: the branch is requested when it can be, and otherwise the
// clone is plain and the configured branch name is adopted locally so the first
// push creates it.
func clone(cfg config.Config, path string) error {
	if cfg.PersonalBranch != "" {
		if _, err := gitx.Run("", "clone", "--quiet", "--branch", cfg.PersonalBranch, cfg.PersonalRepo, path); err == nil {
			return nil
		}
		// Remove whatever a failed attempt left behind, or the retry refuses to
		// write into a non-empty directory.
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}

	if _, err := gitx.Run("", "clone", "--quiet", cfg.PersonalRepo, path); err != nil {
		return err
	}

	if cfg.PersonalBranch == "" {
		return nil
	}
	// Only meaningful while the clone has no commits; with commits present the
	// requested branch either existed or the user is on the default one.
	if _, err := gitx.Run(path, "rev-parse", "--verify", "--quiet", "HEAD"); err == nil {
		return nil
	}
	_, err := gitx.Run(path, "symbolic-ref", "HEAD", "refs/heads/"+cfg.PersonalBranch)
	return err
}

// Pull fast-forwards the clone.
//
// It refuses to pull over local modifications rather than risking a conflict in
// a hook the user cannot interact with. Local work is preserved and reported.
func (r Repo) Pull() (string, error) {
	if !r.Present {
		return "", nil
	}
	clean, err := gitx.IsClean(r.Path)
	if err != nil {
		return fmt.Sprintf("personal layer: could not check status (%v); using local copy", err), nil
	}
	if !clean {
		return "personal layer has uncommitted local changes; skipped pull and used the local copy", nil
	}
	// A clone of a repository that has no commits yet has no upstream branch, so
	// there is nothing to pull. Reporting git's "no tracking information" error
	// here would make a normal first run look broken.
	if _, err := gitx.Run(r.Path, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); err != nil {
		return "", nil
	}
	if _, err := gitx.Run(r.Path, "pull", "--ff-only", "--quiet"); err != nil {
		return fmt.Sprintf("personal layer: pull failed (%v); using local copy", err), nil
	}
	return "", nil
}

// ErrNothingToCommit reports that the staged set was empty.
var ErrNothingToCommit = errors.New("nothing to commit")

// Commit stages the given paths and commits only those.
//
// Staging by explicit path rather than "git add -A" matters: the clone is a real
// repository the user may have touched, and this tool has no business committing
// changes it did not make.
func (r Repo) Commit(paths []string, message string) error {
	if !r.Present {
		return errors.New("personal layer is not available")
	}
	if len(paths) == 0 {
		return ErrNothingToCommit
	}

	// "add -A" with a pathspec also records deletions, which a plain "add" skips.
	// Paths are staged one at a time so that one path matching nothing does not
	// abort the rest: a memory moved out of this layer can name a file that was
	// never committed here.
	var staged []string
	for _, p := range dedupe(paths) {
		if _, err := gitx.Run(r.Path, "add", "-A", "--", p); err != nil {
			continue
		}
		staged = append(staged, p)
	}
	if len(staged) == 0 {
		return ErrNothingToCommit
	}

	// On the very first commit there is no HEAD to diff against, and anything
	// staged is by definition a change.
	if _, err := gitx.Run(r.Path, "rev-parse", "--verify", "--quiet", "HEAD"); err == nil {
		changed, err := gitx.Run(r.Path, append([]string{"diff", "--cached", "--name-only", "--"}, staged...)...)
		if err != nil {
			return err
		}
		if strings.TrimSpace(changed) == "" {
			return ErrNothingToCommit
		}
	}

	// The pathspec keeps the commit to our files even if the user staged others.
	args := withIdentity("commit", "--quiet", "--only", "-m", message, "--")
	_, err := gitx.Run(r.Path, append(args, staged...)...)
	return err
}

// gitIdentity is passed to every git command that writes a commit.
//
// It is set per-command so the tool works in a fresh clone on a machine with no
// global git identity, without ever writing to the user's git config. Rebase
// replays commits, so it needs this exactly as commit does; keeping a single
// definition is what stops those two paths from drifting apart again.
var gitIdentity = []string{
	"-c", "user.name=memory-manager",
	"-c", "user.email=memory-manager@localhost",
}

// withIdentity prefixes args with gitIdentity. It copies rather than appending
// in place, so no caller can alias the shared slice.
func withIdentity(args ...string) []string {
	out := make([]string, 0, len(gitIdentity)+len(args))
	out = append(out, gitIdentity...)
	return append(out, args...)
}

func dedupe(paths []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, p := range paths {
		if p == "" || seen[p] {
			continue
		}
		seen[p] = true
		out = append(out, p)
	}
	return out
}

// ErrRebaseConflict reports a genuine conflict that a human has to resolve. The
// clone is left clean: no half-finished rebase, nothing pushed.
type ErrRebaseConflict struct {
	// Files are the conflicting paths, when git named them.
	Files []string
}

func (e *ErrRebaseConflict) Error() string {
	if len(e.Files) == 0 {
		return "personal memory conflicts with the remote and needs manual resolution"
	}
	return "personal memory conflicts with the remote in " + strings.Join(e.Files, ", ") +
		"; resolve it by hand in the personal clone"
}

// ErrRebaseFailed reports that the rebase could not run at all: an unreachable
// remote, a timeout, a git that refuses to replay commits.
//
// It is deliberately a different type from ErrRebaseConflict. Reporting every
// failure as a conflict sends the user hunting for conflicting files that do not
// exist, and this runs from a SessionEnd hook where nobody sees the real stderr.
type ErrRebaseFailed struct{ Err error }

func (e *ErrRebaseFailed) Error() string {
	return "personal memory could not be rebased onto the remote: " + e.Err.Error()
}

func (e *ErrRebaseFailed) Unwrap() error { return e.Err }

// Push publishes local commits, rebasing once if the remote moved.
//
// One fact per file means a concurrent edit from another machine almost always
// rebases cleanly. When it does not, the rebase is aborted so the clone is never
// left mid-operation for a hook the user cannot see or interact with.
func (r Repo) Push() error {
	if !r.Present {
		return errors.New("personal layer is not available")
	}

	if _, err := gitx.Run(r.Path, "push", "--quiet"); err == nil {
		return nil
	}

	// A brand new repository has no upstream configured yet.
	if _, err := gitx.Run(r.Path, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"); err != nil {
		if _, err := gitx.Run(r.Path, "push", "--quiet", "--set-upstream", "origin", "HEAD"); err != nil {
			return err
		}
		return nil
	}

	if _, err := gitx.Run(r.Path, withIdentity("pull", "--rebase", "--quiet")...); err != nil {
		conflicts, _ := gitx.Run(r.Path, "diff", "--name-only", "--diff-filter=U")
		files := nonEmptyLines(conflicts)
		// Abort unconditionally: if no rebase is in progress this fails harmlessly,
		// and leaving one in progress would break every later run.
		_, _ = gitx.Run(r.Path, "rebase", "--abort")
		// Unmerged paths are what separates a real conflict from a rebase that
		// never got far enough to produce one.
		if len(files) == 0 {
			return &ErrRebaseFailed{Err: err}
		}
		return &ErrRebaseConflict{Files: files}
	}

	if _, err := gitx.Run(r.Path, "push", "--quiet"); err != nil {
		return err
	}
	return nil
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}
