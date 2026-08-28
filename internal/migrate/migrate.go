// Package migrate adopts memory that already lives in Claude Code's
// path-keyed directories.
//
// There is a real corpus to rescue — over a hundred files across several
// projects on this machine — so migration is a first-class command rather than
// a manual copy. It always produces a reviewable plan first: the project layer
// is committed into shared repositories, and a secret that lands in a shared
// history is not undone by a revert.
package migrate

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Arlezz/memory-manager/internal/claudedir"
	"github.com/Arlezz/memory-manager/internal/config"
	"github.com/Arlezz/memory-manager/internal/frontmatter"
	"github.com/Arlezz/memory-manager/internal/identity"
	"github.com/Arlezz/memory-manager/internal/layer"
	"github.com/Arlezz/memory-manager/internal/personal"
	"github.com/Arlezz/memory-manager/internal/secrets"
)

// maxScanDepth bounds the search for working directories below each search root.
// Five levels reaches the observed layout (~/Documents/projects/<repo>) with room
// to spare, without walking an entire disk.
const maxScanDepth = 5

// skipDirs are never descended into: they are large and never hold a project
// root worth matching.
var skipDirs = map[string]bool{
	".git": true, "node_modules": true, "vendor": true, "venv": true,
	".venv": true, "__pycache__": true, "dist": true, "build": true,
	"target": true, ".next": true, ".cache": true, "AppData": true,
}

// Action is one planned file move.
type Action struct {
	// Source is the file in the path-keyed memory directory.
	Source string
	// Base is the file name.
	Base string
	// Layer is the destination layer.
	Layer layer.Layer
	// Dest is the absolute destination path, empty when the action is blocked.
	Dest string
	// Type is the memory's declared type.
	Type string
	// Blocked explains why this file cannot be migrated, when set.
	Blocked string
	// Problems are frontmatter defects carried over from parsing.
	Problems []string
	// Notes are advisory frontmatter findings, reported here but not at every sync.
	Notes []string
	// Secrets are suspected credentials found in the file.
	Secrets []secrets.Finding
	// Overwrites reports that Dest already exists with different content.
	Overwrites bool
}

// Group is the plan for one path-keyed project directory.
type Group struct {
	// MangledDir is the directory name under ~/.claude/projects.
	MangledDir string
	// WorkDir is the working directory it was matched back to, if any.
	WorkDir string
	// Identity is the resolved identity, valid only when Resolved is true.
	Identity identity.Identity
	// Resolved reports whether an identity was found.
	Resolved bool
	// Reason explains an unresolved group.
	Reason string
	// Actions are the per-file plans.
	Actions []Action
}

// Plan is the full migration plan.
type Plan struct {
	Groups []Group
	// PersonalRoot is the personal clone the plan targets, empty when the
	// personal layer is unavailable.
	PersonalRoot string
	// Notes are plan-level warnings.
	Notes []string
}

// Options configures planning.
type Options struct {
	// SearchRoots are the directories scanned for working copies. Defaults to
	// the user's home directory.
	SearchRoots []string
	// OnlySlug limits the plan to one identity slug.
	OnlySlug string
}

// Build produces a migration plan without writing anything.
func Build(opts Options) (Plan, error) {
	var plan Plan

	projects, err := claudedir.ListProjects()
	if err != nil {
		return plan, err
	}
	if len(projects) == 0 {
		plan.Notes = append(plan.Notes, "no path-keyed memory directories hold memory files; nothing to migrate")
		return plan, nil
	}

	roots := opts.SearchRoots
	if len(roots) == 0 {
		home, err := os.UserHomeDir()
		if err != nil {
			return plan, err
		}
		roots = []string{home}
	}
	dirIndex := indexWorkDirs(roots)

	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		plan.Notes = append(plan.Notes, cfgErr.Error()+"; personal memories cannot be placed until it exists")
	} else {
		repo, warn, err := personal.Open(cfg)
		if err != nil {
			plan.Notes = append(plan.Notes, fmt.Sprintf("personal layer: %v", err))
		}
		if warn != "" {
			plan.Notes = append(plan.Notes, warn)
		}
		if repo.Present {
			plan.PersonalRoot = repo.Path
		}
	}

	for _, p := range projects {
		g := Group{MangledDir: p.Name}

		work, ok := dirIndex[strings.ToLower(p.Name)]
		if !ok {
			g.Reason = "no working directory on disk mangles back to this name; pass --search-root or migrate it by hand"
			plan.Groups = append(plan.Groups, g)
			continue
		}
		g.WorkDir = work

		id, err := identity.Resolve(work)
		if err != nil {
			g.Reason = err.Error()
			plan.Groups = append(plan.Groups, g)
			continue
		}
		g.Identity, g.Resolved = id, true

		if opts.OnlySlug != "" && id.Slug != opts.OnlySlug {
			continue
		}

		memories, err := layer.Read(p.MemoryPath)
		if err != nil {
			g.Reason = fmt.Sprintf("unreadable: %v", err)
			plan.Groups = append(plan.Groups, g)
			continue
		}

		for _, m := range memories {
			g.Actions = append(g.Actions, planFile(m, id, plan.PersonalRoot))
		}
		sort.Slice(g.Actions, func(i, j int) bool { return g.Actions[i].Base < g.Actions[j].Base })
		plan.Groups = append(plan.Groups, g)
	}

	return plan, nil
}

// planFile decides where one memory goes and what is wrong with it.
func planFile(m frontmatter.Memory, id identity.Identity, personalRoot string) Action {
	a := Action{
		Source:   m.Path,
		Base:     m.Base,
		Type:     m.Type,
		Layer:    layer.For(m),
		Problems: m.Problems,
		Notes:    m.Notes,
	}

	if raw, err := os.ReadFile(m.Path); err == nil {
		a.Secrets = secrets.Scan(string(raw))
	}

	switch a.Layer {
	case layer.Project:
		if id.Root == "" {
			a.Blocked = "project memory needs a git work tree and none was found"
			return a
		}
		a.Dest = filepath.Join(id.Root, filepath.FromSlash(layer.ProjectDir), m.Base)
	case layer.Personal:
		if personalRoot == "" {
			a.Blocked = "personal layer is not available; configure personal_repo first"
			return a
		}
		a.Dest = filepath.Join(layer.PersonalPath(personalRoot, layer.PersonalScope(m, id.Slug)), m.Base)
	}

	if a.Dest != "" {
		if existing, err := os.ReadFile(a.Dest); err == nil {
			if src, err := os.ReadFile(m.Path); err == nil && string(existing) != string(src) {
				a.Overwrites = true
			}
		}
	}
	return a
}

// Apply writes the plan.
//
// Source files are never deleted: the path-keyed directory stays as a backup, so
// a wrong classification costs a second run rather than a lost fact.
func Apply(plan Plan, allowSecrets bool) (written int, skipped int, err error) {
	for _, g := range plan.Groups {
		for _, a := range g.Actions {
			if a.Blocked != "" || a.Dest == "" {
				skipped++
				continue
			}
			if len(a.Secrets) > 0 && !allowSecrets {
				skipped++
				continue
			}
			data, readErr := os.ReadFile(a.Source)
			if readErr != nil {
				skipped++
				continue
			}
			if mkErr := os.MkdirAll(filepath.Dir(a.Dest), 0o755); mkErr != nil {
				return written, skipped, mkErr
			}
			if wErr := os.WriteFile(a.Dest, data, 0o644); wErr != nil {
				return written, skipped, wErr
			}
			written++
		}
	}
	return written, skipped, nil
}

// indexWorkDirs maps a mangled directory name to the working directory that
// produces it.
//
// The mangling is lossy — underscores and separators both become dashes — so it
// cannot be inverted. Walking the disk and mangling forward is the only reliable
// way back, and the lowercase key absorbs the drive-letter case difference seen
// among the existing directories.
func indexWorkDirs(roots []string) map[string]string {
	out := map[string]string{}
	for _, root := range roots {
		absRoot, err := filepath.Abs(root)
		if err != nil {
			continue
		}
		baseDepth := strings.Count(absRoot, string(filepath.Separator))

		_ = filepath.WalkDir(absRoot, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				// Unreadable subtrees are common under a home directory and must
				// not abort the scan.
				return nil
			}
			if !d.IsDir() {
				return nil
			}
			name := d.Name()
			if path != absRoot && (skipDirs[name] || strings.HasPrefix(name, ".")) {
				return fs.SkipDir
			}
			if strings.Count(path, string(filepath.Separator))-baseDepth > maxScanDepth {
				return fs.SkipDir
			}
			key := strings.ToLower(claudedir.Mangle(path))
			if _, exists := out[key]; !exists {
				out[key] = path
			}
			return nil
		})
	}
	return out
}
