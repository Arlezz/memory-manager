package state

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const tokenURL = "https://arlezz:github_pat_11ABCDEFG0aBcDeFgHiJkL@github.com/acme-dev/orbit-x-memory.git"

// TestSaveRedactsThePersonalRepo covers the third leak path of A-03. config.json
// holds the same string at 0o600 while the manifest held it in the clear; a file
// that records what was synced has no use for the credential.
func TestSaveRedactsThePersonalRepo(t *testing.T) {
	isolate(t)

	if err := Save(Manifest{Slug: "x", PersonalRepo: tokenURL, Entries: map[string]Entry{}}); err != nil {
		t.Fatal(err)
	}

	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "x.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "github_pat_") {
		t.Error("the manifest on disk still contains the token")
	}
	if !strings.Contains(string(raw), "***@github.com/acme-dev/orbit-x-memory.git") {
		t.Errorf("the redacted URL is not in the manifest:\n%s", raw)
	}
}

// TestSaveUsesAnOwnerOnlyMode is A-11. The mode is inert on Windows, where ACLs
// are inherited, so the assertion runs where it means something — which is the
// platform this tool is moving to.
func TestSaveUsesAnOwnerOnlyMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("file modes do not govern access on Windows; the ACL is inherited")
	}
	isolate(t)

	if err := Save(Manifest{Slug: "x", Entries: map[string]Entry{}}); err != nil {
		t.Fatal(err)
	}
	dir, err := Dir()
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "x.json"))
	if err != nil {
		t.Fatal(err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Errorf("manifest mode = %04o, want 0600: it names the personal repo and every origin path", mode)
	}
}

// TestForPersonalRepoSurvivesATokenRotation is the trap that redacting the
// stored URL sets: if one side is redacted and the other is not, every sync
// decides the personal layer moved and drops every entry.
func TestForPersonalRepoSurvivesATokenRotation(t *testing.T) {
	m := Manifest{
		Slug:         "x",
		PersonalRepo: "https://***@github.com/acme-dev/orbit-x-memory.git",
		Entries:      map[string]Entry{"note.md": {Layer: "personal"}},
	}

	// The same repository, reached with a freshly rotated token.
	rotated := "https://arlezz:github_pat_22ZYXWVUT9zYxWvUtSrQpO@github.com/acme-dev/orbit-x-memory.git"
	if got := m.ForPersonalRepo(rotated); len(got.Entries) != 1 {
		t.Error("rotating the token emptied the manifest; the two sides are not comparing the same form")
	}

	// A genuinely different repository still invalidates everything.
	other := "https://github.com/acme-dev/some-other-memory.git"
	if got := m.ForPersonalRepo(other); len(got.Entries) != 0 {
		t.Error("pointing at another repository kept the old entries")
	}
}
