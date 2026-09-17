package fsx

import (
	"os"
	"path/filepath"
	"testing"
)

// TestWriteFileLeavesNoPartialFile is the point of the package: the destination
// is either the old content or the new one, never a prefix of the new one.
func TestWriteFileLeavesNoPartialFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "memory.md")

	if err := WriteFile(path, []byte("first"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := WriteFile(path, []byte("second and longer"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "second and longer" {
		t.Errorf("content = %q, want the second write", got)
	}

	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("the temporary file survived a successful write")
	}
}

// TestWriteFileRemovesItsTemporaryOnFailure keeps the memory directories clean:
// they are listed by extension, and an accumulating ".tmp" is litter in a
// directory the user reads.
func TestWriteFileRemovesItsTemporaryOnFailure(t *testing.T) {
	dir := t.TempDir()
	// A destination inside a directory that does not exist: the temporary file
	// cannot be created, which is the failure before the rename.
	path := filepath.Join(dir, "missing", "memory.md")

	if err := WriteFile(path, []byte("x"), 0o644); err == nil {
		t.Fatal("WriteFile succeeded into a missing directory, want an error")
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("the temporary file survived a failed write")
	}
}

// TestWithinComparesPathBoundaries covers the difference between this and the
// string prefix it replaces, in both directions.
func TestWithinComparesPathBoundaries(t *testing.T) {
	root := filepath.Join("home", "anton", "personal")

	inside := []string{
		filepath.Join(root, "global", "note.md"),
		filepath.Join(root, "note.md"),
		// A sibling whose name merely starts with dots is inside, and the
		// relative path for it starts with ".." as a string.
		filepath.Join(root, "..keep"),
		root,
	}
	for _, p := range inside {
		if !Within(root, p) {
			t.Errorf("Within(%q, %q) = false, want true", root, p)
		}
	}

	outside := []string{
		// The case a string prefix gets wrong.
		filepath.Join("home", "anton", "personal-other", "note.md"),
		filepath.Join("home", "anton", "project", "note.md"),
		filepath.Join(root, "..", "escape.md"),
		filepath.Join("home", "anton"),
	}
	for _, p := range outside {
		if Within(root, p) {
			t.Errorf("Within(%q, %q) = true, want false", root, p)
		}
	}

	// An unavailable layer has an empty root, and must not end up containing
	// everything or nothing-checked.
	if Within("", filepath.Join(root, "note.md")) {
		t.Error(`Within("", path) = true, want false`)
	}
	if Within(root, "") {
		t.Error(`Within(root, "") = true, want false`)
	}
}
