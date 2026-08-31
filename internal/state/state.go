// Package state persists what a sync produced.
//
// The manifest records which layer every merged file came from. Stage 2 needs
// it to route an edited memory back to the right repository: once the files sit
// merged in one directory, their origin is no longer visible on disk.
package state

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/Arlezz/memory-manager/internal/claudedir"
)

// Version is the manifest schema version, so a future change can migrate
// rather than misread an old file.
const Version = 1

// Entry describes one merged memory file.
type Entry struct {
	// Layer is "project" or "personal".
	Layer string `json:"layer"`
	// Origin is the absolute path the file was copied from.
	Origin string `json:"origin"`
	// SHA256 is the digest of the content as merged. A later mismatch means the
	// agent edited the file in place, which is the signal stage 2 acts on.
	SHA256 string `json:"sha256"`
}

// Manifest is the record of one project's last sync.
type Manifest struct {
	Version int `json:"version"`
	// Slug is the project identity.
	Slug string `json:"slug"`
	// Canonical is the human-readable identity.
	Canonical string `json:"canonical"`
	// MemoryDir is where the merge was written.
	MemoryDir string `json:"memory_dir"`
	// PersonalRepo is the personal repository the entries were recorded against.
	// Empty in manifests written before this field existed.
	PersonalRepo string `json:"personal_repo,omitempty"`
	// Entries is keyed by file name.
	Entries map[string]Entry `json:"entries"`
}

// ForPersonalRepo returns m unchanged when it was recorded against repo, and an
// empty manifest when it was not.
//
// Repointing the personal layer at a different repository invalidates every
// personal entry at once: the origin paths and hashes describe files in a clone
// that is gone. Trusting them makes push report "nothing to push" against an
// empty repository, which is the exact failure this tool exists to prevent —
// believing memory is synced while nothing is there.
//
// A manifest written before this field existed carries no repo and is kept, so
// an upgrade does not force a needless full rewrite. It is stamped on the next
// sync or push, and protected from then on.
//
// A missing origin file is deliberately NOT used as the signal here, tempting as
// it looks: a personal memory deleted on another machine also has no origin
// file, and that absence is exactly how a real deletion propagates.
func (m Manifest) ForPersonalRepo(repo string) Manifest {
	if m.PersonalRepo == "" || m.PersonalRepo == repo {
		return m
	}
	return Manifest{Version: Version, Slug: m.Slug, Canonical: m.Canonical, Entries: map[string]Entry{}}
}

// Dir returns the directory holding manifests.
func Dir() (string, error) {
	root, err := claudedir.Root()
	if err != nil {
		return "", err
	}
	return filepath.Join(root, "memory-manager", "state"), nil
}

// path returns the manifest file for a slug.
func path(slug string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, slug+".json"), nil
}

// Load reads the manifest for slug. A missing manifest yields an empty one, so
// a first run needs no special case.
func Load(slug string) (Manifest, error) {
	m := Manifest{Version: Version, Slug: slug, Entries: map[string]Entry{}}
	p, err := path(slug)
	if err != nil {
		return m, err
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if os.IsNotExist(err) {
			return m, nil
		}
		return m, err
	}
	if err := json.Unmarshal(data, &m); err != nil {
		// A corrupt manifest must not block a session; the next sync rebuilds it.
		return Manifest{Version: Version, Slug: slug, Entries: map[string]Entry{}},
			fmt.Errorf("manifest %s is unreadable and will be rebuilt: %w", p, err)
	}
	if m.Entries == nil {
		m.Entries = map[string]Entry{}
	}
	return m, nil
}

// Save writes the manifest atomically, so an interrupted run cannot leave a
// half-written file that the next run refuses to parse.
func Save(m Manifest) error {
	m.Version = Version
	p, err := path(m.Slug)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// Digest returns the SHA256 of content, hex encoded.
func Digest(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

// Names returns the entry names in sorted order.
func (m Manifest) Names() []string {
	out := make([]string, 0, len(m.Entries))
	for k := range m.Entries {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
