// Package layer defines the two memory layers and where each memory belongs.
package layer

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Arlezz/memory-manager/internal/frontmatter"
)

// Layer is a memory storage layer.
type Layer string

const (
	// Project memory is committed inside the project repository: architecture
	// decisions, conventions, rejected alternatives. It travels with the code
	// and is reviewed in the same pull request.
	Project Layer = "project"
	// Personal memory lives in a private per-person repository: preferences,
	// working-style feedback, cross-project context. It follows the person.
	Personal Layer = "personal"
)

// ProjectDir is the repo-relative directory holding the project layer.
const ProjectDir = ".claude/memory"

// For returns the layer a memory belongs to.
//
// An explicit scope wins over the type. Otherwise only "project" memories are
// shared, so the default never leaks a personal note into a team repository —
// the conservative direction is the recoverable one.
func For(m frontmatter.Memory) Layer {
	switch m.Scope {
	case "project":
		return Project
	case "personal":
		return Personal
	}
	if m.Type == "project" {
		return Project
	}
	return Personal
}

// PersonalGlobalDir is where personal memories that apply everywhere live.
const PersonalGlobalDir = "global"

// PersonalProjectsDir holds per-project personal memory, keyed by identity slug.
const PersonalProjectsDir = "projects"

// PersonalPath returns the directory inside the personal repo for a memory.
//
// A personal memory tied to a project stays scoped to that project so unrelated
// sessions do not get it injected; slug is empty for a global memory.
func PersonalPath(root, slug string) string {
	if slug == "" {
		return filepath.Join(root, PersonalGlobalDir)
	}
	return filepath.Join(root, PersonalProjectsDir, slug)
}

// PersonalScope decides whether a personal memory is global or project-scoped,
// returning the slug to file it under. An empty result means global.
//
// Who the user is applies everywhere, so a "user" memory goes global. Feedback
// and references discovered inside a project stay scoped to that project until
// the user promotes them, which keeps unrelated sessions from being flooded with
// context they cannot use.
func PersonalScope(m frontmatter.Memory, slug string) string {
	if m.Type == "user" {
		return ""
	}
	return slug
}

// IndexFile is the generated index Claude Code loads at session start.
//
// It is regenerated from frontmatter on every sync and is never committed:
// every contributor touches it, so versioning it would make it the one
// guaranteed merge conflict in the store.
const IndexFile = "MEMORY.md"

// Read parses every memory file in dir, skipping the generated index.
//
// A missing directory is not an error: most repos have no project layer yet.
func Read(dir string) ([]frontmatter.Memory, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var out []frontmatter.Memory
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".md") {
			continue
		}
		if strings.EqualFold(e.Name(), IndexFile) {
			continue
		}
		m, err := frontmatter.Parse(filepath.Join(dir, e.Name()))
		if err != nil {
			return out, err
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Base < out[j].Base })
	return out, nil
}
