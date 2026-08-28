package identity

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func writeOverride(t *testing.T, dir, slug string) {
	t.Helper()
	path := filepath.Join(dir, filepath.FromSlash(OverrideFile))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(slug+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestResolveOverride(t *testing.T) {
	dir := t.TempDir()
	writeOverride(t, dir, "github.com__orbit-dev__orbit-x_core")

	id, err := Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	if id.Source != SourceOverride {
		t.Errorf("Source = %q, want override", id.Source)
	}
	if id.Slug != "github.com__orbit-dev__orbit-x_core" {
		t.Errorf("Slug = %q", id.Slug)
	}
	if id.Canonical != "github.com/orbit-dev/orbit-x_core" {
		t.Errorf("Canonical = %q", id.Canonical)
	}
}

// TestResolveOverrideMarksRootWithoutGit is the behaviour that lets a non-git
// directory hold project memory: without a Root there is nowhere to put it, and
// a large share of the directories on this machine are not repositories.
func TestResolveOverrideMarksRootWithoutGit(t *testing.T) {
	dir := t.TempDir()
	writeOverride(t, dir, "local/scratch")

	id, err := Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	if id.Root != dir {
		t.Errorf("Root = %q, want %q", id.Root, dir)
	}
}

// TestResolveOverrideFoundFromSubdirectory matters because hooks run from
// whatever directory the session happens to be in.
func TestResolveOverrideFoundFromSubdirectory(t *testing.T) {
	root := t.TempDir()
	writeOverride(t, root, "local/scratch")
	sub := filepath.Join(root, "pkg", "deep")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	id, err := Resolve(sub)
	if err != nil {
		t.Fatal(err)
	}
	if id.Root != root {
		t.Errorf("Root = %q, want %q", id.Root, root)
	}
}

// TestResolveNearestOverrideWins lets one checkout pin its own identity without
// affecting a sibling that shares a parent directory.
func TestResolveNearestOverrideWins(t *testing.T) {
	root := t.TempDir()
	writeOverride(t, root, "outer/project")
	inner := filepath.Join(root, "inner")
	writeOverride(t, inner, "inner/project")

	id, err := Resolve(inner)
	if err != nil {
		t.Fatal(err)
	}
	if id.Canonical != "inner/project" {
		t.Errorf("Canonical = %q, want the nearest override", id.Canonical)
	}
}

func TestResolveNoIdentity(t *testing.T) {
	_, err := Resolve(t.TempDir())
	if !errors.Is(err, ErrNoIdentity) {
		t.Errorf("err = %v, want ErrNoIdentity", err)
	}
}

// TestResolveIgnoresCommentsInOverride keeps the generated file, which carries
// an explanatory header, readable by the tool that wrote it.
func TestResolveIgnoresCommentsInOverride(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, filepath.FromSlash(OverrideFile))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "# Project identity for memory-manager. Keep this value stable:\n\nlocal__scratch\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	id, err := Resolve(dir)
	if err != nil {
		t.Fatal(err)
	}
	if id.Slug != "local__scratch" {
		t.Errorf("Slug = %q", id.Slug)
	}
}
