// Package identity resolves a stable project identity.
//
// Claude Code keys project memory by absolute filesystem path, which is why
// memory does not survive a clone into a different folder or onto a different
// machine. This package replaces that key with the normalized git remote URL.
package identity

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Arlezz/memory-manager/internal/gitx"
)

// Source records where an identity came from, for diagnostics.
type Source string

const (
	// SourceOverride means an explicit .claude/memory-id file supplied the slug.
	SourceOverride Source = "override"
	// SourceRemote means the slug was derived from the git remote URL.
	SourceRemote Source = "remote"
)

// OverrideFile is the repo-relative path of the explicit identity override.
// It wins over the remote so worktrees, backup copies and repos transferred
// between orgs can pin their own identity.
const OverrideFile = ".claude/memory-id"

// ErrNoIdentity reports that neither an override file nor a usable remote was
// found. Callers must degrade to local memory and warn; this is not fatal.
var ErrNoIdentity = errors.New("no project identity: no " + OverrideFile + " and no usable git remote")

// Identity is a resolved project identity.
type Identity struct {
	// Slug is the filesystem-safe key, e.g. "github.com__orbit-dev__orbit-x_core".
	Slug string
	// Canonical is the human-readable form, e.g. "github.com/orbit-dev/orbit-x_core".
	Canonical string
	// Source says how the identity was obtained.
	Source Source
	// Root is the repository root the identity was resolved from. Empty when the
	// override was found outside a git repo.
	Root string
}

// Resolve determines the identity of the project containing dir.
//
// Precedence: the override file first, then the git remote. A local-path remote
// is rejected on purpose — a path-derived identity is the exact bug this tool
// exists to fix, and silently accepting one would make memory look synced when
// it is not.
func Resolve(dir string) (Identity, error) {
	root := repoRoot(dir)

	if slug, holder, ok := findOverride(dir, root); ok {
		// Outside a git repository the override file is what marks the project
		// root, so the project layer still has somewhere to live. Without this,
		// the ~40% of directories here that are not git repos could hold no
		// project memory at all.
		if root == "" {
			root = holder
		}
		canonical := strings.ReplaceAll(slug, "__", "/")
		return Identity{
			Slug:      Slugify(slug),
			Canonical: canonical,
			Source:    SourceOverride,
			Root:      root,
		}, nil
	}

	if root == "" {
		return Identity{}, ErrNoIdentity
	}

	raw, err := gitx.RemoteURL(root, "origin")
	if err != nil || raw == "" {
		return Identity{}, ErrNoIdentity
	}

	canonical, err := Normalize(raw)
	if err != nil {
		return Identity{}, fmt.Errorf("%w: %v", ErrNoIdentity, err)
	}

	return Identity{
		Slug:      Slugify(canonical),
		Canonical: canonical,
		Source:    SourceRemote,
		Root:      root,
	}, nil
}

// repoRoot walks up from dir looking for a .git entry and returns the directory
// holding it, or "" when dir is not inside a repository.
//
// We walk the tree ourselves rather than asking git, so that Resolve works the
// same way when git is missing and so a bare "not a repository" error does not
// have to be parsed out of git's output.
func repoRoot(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(abs, ".git")); err == nil {
			return abs
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return ""
		}
		abs = parent
	}
}

// findOverride walks up from dir looking for the override file and returns the
// slug plus the directory holding it.
//
// The walk stops at stopAt when it is non-empty, so a repository's own override
// is found from any subdirectory while a file above the repository root is
// ignored. The nearest file wins, which lets one checkout pin its own identity
// without affecting its siblings.
func findOverride(dir, stopAt string) (slug, holder string, ok bool) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", "", false
	}
	for {
		if s, found := readOverride(abs); found {
			return s, abs, true
		}
		if stopAt != "" && abs == stopAt {
			return "", "", false
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", "", false
		}
		abs = parent
	}
}

// readOverride reads the override file from root, ignoring blank lines and
// comments so the file can explain itself to whoever opens it.
func readOverride(root string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(OverrideFile)))
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		return line, true
	}
	return "", false
}
