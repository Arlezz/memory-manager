package state

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func isolate(t *testing.T) {
	t.Helper()
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(t.TempDir(), "claude"))
}

func TestSaveLoadRoundTrip(t *testing.T) {
	isolate(t)

	want := Manifest{
		Slug:      "github.com__orbit-dev__orbit-x_core",
		Canonical: "github.com/orbit-dev/orbit-x_core",
		MemoryDir: filepath.FromSlash("/claude/projects/x/memory"),
		Entries: map[string]Entry{
			"decision.md": {Layer: "project", Origin: "/repo/.claude/memory/decision.md", SHA256: "abc"},
			"spanish.md":  {Layer: "personal", Origin: "/clone/global/spanish.md", SHA256: "def"},
		},
	}
	if err := Save(want); err != nil {
		t.Fatal(err)
	}

	got, err := Load(want.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if got.Version != Version {
		t.Errorf("Version = %d, want %d", got.Version, Version)
	}
	if got.Canonical != want.Canonical || got.MemoryDir != want.MemoryDir {
		t.Errorf("got %+v, want %+v", got, want)
	}
	for name, entry := range want.Entries {
		if got.Entries[name] != entry {
			t.Errorf("entry %q = %+v, want %+v", name, got.Entries[name], entry)
		}
	}
}

// TestLoadMissingIsEmpty keeps the first run free of special cases.
func TestLoadMissingIsEmpty(t *testing.T) {
	isolate(t)

	m, err := Load("never__seen")
	if err != nil {
		t.Fatalf("a missing manifest must not be an error: %v", err)
	}
	if m.Entries == nil {
		t.Error("Entries is nil; callers would panic on assignment")
	}
	if len(m.Entries) != 0 {
		t.Errorf("Entries = %v, want empty", m.Entries)
	}
}

// TestLoadCorruptRebuilds is the degrade rule applied to state: a damaged
// manifest reports itself and yields an empty one, rather than blocking a
// session that could still run.
func TestLoadCorruptRebuilds(t *testing.T) {
	isolate(t)

	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "broken.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := Load("broken")
	if err == nil {
		t.Error("a corrupt manifest was loaded silently")
	} else if !strings.Contains(err.Error(), "rebuilt") {
		t.Errorf("error does not say what happens next: %v", err)
	}
	if m.Entries == nil || len(m.Entries) != 0 {
		t.Errorf("Entries = %v, want an empty usable map", m.Entries)
	}
}

// TestSaveLeavesNoTempFile checks the atomic write cleans up: a stray .tmp would
// accumulate one file per session forever.
func TestSaveLeavesNoTempFile(t *testing.T) {
	isolate(t)

	if err := Save(Manifest{Slug: "x", Entries: map[string]Entry{}}); err != nil {
		t.Fatal(err)
	}
	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

// TestSaveOverwrites covers the normal case of a second sync replacing the first.
func TestSaveOverwrites(t *testing.T) {
	isolate(t)

	if err := Save(Manifest{Slug: "x", Entries: map[string]Entry{"a.md": {Layer: "project"}}}); err != nil {
		t.Fatal(err)
	}
	if err := Save(Manifest{Slug: "x", Entries: map[string]Entry{"b.md": {Layer: "personal"}}}); err != nil {
		t.Fatal(err)
	}

	m, err := Load("x")
	if err != nil {
		t.Fatal(err)
	}
	if _, stale := m.Entries["a.md"]; stale {
		t.Error("the previous manifest was merged instead of replaced")
	}
	if _, ok := m.Entries["b.md"]; !ok {
		t.Error("the new entry is missing")
	}
}

func TestDigestIsStable(t *testing.T) {
	a := Digest([]byte("same content"))
	if a != Digest([]byte("same content")) {
		t.Error("Digest is not deterministic")
	}
	if a == Digest([]byte("other content")) {
		t.Error("Digest collided on different content")
	}
	if len(a) != 64 {
		t.Errorf("Digest length = %d, want 64 hex characters", len(a))
	}
}

func TestNamesAreSorted(t *testing.T) {
	m := Manifest{Entries: map[string]Entry{
		"c.md": {}, "a.md": {}, "b.md": {},
	}}
	got := m.Names()
	want := []string{"a.md", "b.md", "c.md"}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Names() = %v, want %v", got, want)
		}
	}
}

// TestLoadDropsEntriesThatAreNotPlainFileNames covers the key half of A-09.
// Entry keys are joined onto a layer root and reach os.WriteFile and os.Remove,
// and Load is the only place that sees them before they become paths. A sync
// can only produce keys that came from filepath.Base, so anything else did not
// come from a sync.
func TestLoadDropsEntriesThatAreNotPlainFileNames(t *testing.T) {
	isolate(t)

	m := Manifest{
		Slug: "test__project",
		Entries: map[string]Entry{
			"keep.md":          {Layer: "personal", Origin: "/clone/global/keep.md"},
			"../escape.md":     {Layer: "personal", Origin: "/clone/global/escape.md"},
			`..\escape.md`:     {Layer: "personal", Origin: "/clone/global/escape.md"},
			"global/nested.md": {Layer: "personal", Origin: "/clone/global/nested.md"},
			`sub\nested.md`:    {Layer: "personal", Origin: "/clone/global/nested.md"},
			"/etc/passwd":      {Layer: "personal", Origin: "/etc/passwd"},
			"..":               {Layer: "personal", Origin: "/clone"},
			".":                {Layer: "personal", Origin: "/clone"},
			"":                 {Layer: "personal", Origin: "/clone"},
		},
	}
	if err := Save(m); err != nil {
		t.Fatal(err)
	}

	got, err := Load(m.Slug)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Entries) != 1 {
		t.Errorf("kept %d entries, want only the plain file name: %v", len(got.Entries), got.Names())
	}
	if _, ok := got.Entries["keep.md"]; !ok {
		t.Error("the legitimate entry was dropped too")
	}
}
