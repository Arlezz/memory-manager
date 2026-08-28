package config

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(t.TempDir(), "claude"))
}

func TestSaveLoadRoundTrip(t *testing.T) {
	isolate(t)

	want := Config{PersonalRepo: "git@github.com:anton/claude-memory.git", PersonalBranch: "main"}
	if err := Save(want); err != nil {
		t.Fatal(err)
	}
	got, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Errorf("got %+v, want %+v", got, want)
	}
}

// TestLoadMissingNamesThePath means the error tells the user what to create
// instead of just saying no.
func TestLoadMissingNamesThePath(t *testing.T) {
	isolate(t)

	_, err := Load()
	if !errors.Is(err, ErrNotConfigured) {
		t.Fatalf("err = %v, want ErrNotConfigured", err)
	}
	path, _ := File()
	if !strings.Contains(err.Error(), path) {
		t.Errorf("error does not name the expected path: %v", err)
	}
}

func TestLoadInvalidJSON(t *testing.T) {
	isolate(t)

	path, err := File()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{oops"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Load(); err == nil {
		t.Error("invalid JSON loaded without complaint")
	}
}

func TestLoadTrimsWhitespace(t *testing.T) {
	isolate(t)

	path, _ := File()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"personal_repo": "  git@example.com:x.git \n"}`), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.PersonalRepo != "git@example.com:x.git" {
		t.Errorf("PersonalRepo = %q, want it trimmed", cfg.PersonalRepo)
	}
}

// TestSaveIsOwnerOnly matters because the config can hold a repository URL with
// an access token in it.
func TestSaveIsOwnerOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission bits are not meaningful on Windows")
	}
	isolate(t)

	if err := Save(Config{PersonalRepo: "git@example.com:x.git"}); err != nil {
		t.Fatal(err)
	}
	path, _ := File()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("mode = %o, want 600", perm)
	}
}

// TestPersonalClonePathHonoursConfigDir keeps the whole tool relocatable, which
// is what makes the isolated tests elsewhere possible.
func TestPersonalClonePathHonoursConfigDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "elsewhere")
	t.Setenv("CLAUDE_CONFIG_DIR", dir)

	got, err := PersonalClonePath()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(dir, "memory-manager", "personal")
	if got != want {
		t.Errorf("PersonalClonePath = %q, want %q", got, want)
	}
}
