// Package claudedir locates Claude Code's own on-disk layout.
//
// The mangling scheme below is reverse-engineered from the directories Claude
// Code actually created on this machine, not from documentation. Every function
// that depends on it therefore prefers matching an existing directory over
// trusting the reconstruction.
package claudedir

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Root returns the user's ~/.claude directory, honouring CLAUDE_CONFIG_DIR.
func Root() (string, error) {
	if v := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude"), nil
}

// ProjectsRoot returns ~/.claude/projects, the parent of every mangled project
// directory.
func ProjectsRoot() (string, error) {
	root, err := Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "projects"), nil
}

// MemorySubdir is the memory directory inside a mangled project directory.
const MemorySubdir = "memory"

// nonAlnum matches every character Claude Code replaces with a dash.
var nonAlnum = regexp.MustCompile(`[^A-Za-z0-9]`)

// Mangle converts an absolute project path into the directory name Claude Code
// derives from it.
//
// Observed on this machine:
//
//	C:\Users\Anton\Documents\projects\memory-manager -> C--Users-Anton-Documents-projects-memory-manager
//	C:\Users\Anton\Documents\projects\ORBIT-X_core   -> C--Users-Anton-Documents-projects-ORBIT-X-core
//
// Note the second example: an underscore also collapses to a dash, so the
// mapping is lossy and cannot be inverted. Callers that need the original path
// must keep it, and this is one more reason the path is the wrong memory key.
func Mangle(absPath string) string {
	return nonAlnum.ReplaceAllString(absPath, "-")
}

// MemoryDir returns the memory directory Claude Code uses for absPath.
//
// It prefers an existing directory whose name matches case-insensitively,
// because the drive letter's case varies with the shell that launched the
// session: this machine has both "c--Users-..." and "C--Users-..." directories
// for sibling projects. Falling back to the exact reconstruction means a new
// project still gets a predictable path.
func MemoryDir(absPath string) (string, error) {
	projects, err := ProjectsRoot()
	if err != nil {
		return "", err
	}
	want := Mangle(absPath)

	if entries, err := os.ReadDir(projects); err == nil {
		for _, e := range entries {
			if e.IsDir() && strings.EqualFold(e.Name(), want) {
				return filepath.Join(projects, e.Name(), MemorySubdir), nil
			}
		}
	}
	return filepath.Join(projects, want, MemorySubdir), nil
}

// ProjectDir is one mangled project directory found on disk.
type ProjectDir struct {
	// Name is the mangled directory name.
	Name string
	// Path is the absolute path of the mangled directory.
	Path string
	// MemoryPath is the memory subdirectory, which may not exist.
	MemoryPath string
	// Files is the count of .md memory files, excluding the index.
	Files int
}

// ListProjects returns every mangled project directory that holds memory files.
//
// This is the inventory the migrate command works from.
func ListProjects() ([]ProjectDir, error) {
	projects, err := ProjectsRoot()
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(projects)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var out []ProjectDir
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		p := ProjectDir{
			Name:       e.Name(),
			Path:       filepath.Join(projects, e.Name()),
			MemoryPath: filepath.Join(projects, e.Name(), MemorySubdir),
		}
		p.Files = countMemories(p.MemoryPath)
		if p.Files == 0 {
			continue
		}
		out = append(out, p)
	}
	return out, nil
}

// countMemories counts .md files other than the generated index.
func countMemories(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() || !strings.EqualFold(filepath.Ext(e.Name()), ".md") {
			continue
		}
		if strings.EqualFold(e.Name(), "MEMORY.md") {
			continue
		}
		n++
	}
	return n
}
