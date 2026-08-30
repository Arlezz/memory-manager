package migrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Arlezz/memory-manager/internal/claudedir"
	"github.com/Arlezz/memory-manager/internal/frontmatter"
	"github.com/Arlezz/memory-manager/internal/identity"
)

// TestIndexWorkDirsInvertsTheMangling is the core trick of this package: the
// path-keyed directory name cannot be decoded, so every candidate directory is
// mangled forward and matched instead.
func TestIndexWorkDirsInvertsTheMangling(t *testing.T) {
	root := t.TempDir()
	project := filepath.Join(root, "ORBIT-X_core")
	if err := os.MkdirAll(filepath.Join(project, "src"), 0o755); err != nil {
		t.Fatal(err)
	}

	index := indexWorkDirs([]string{root})

	key := strings.ToLower(claudedir.Mangle(project))
	got, ok := index[key]
	if !ok {
		t.Fatalf("no entry for %q; index has %d entries", key, len(index))
	}
	if got != project {
		t.Errorf("index[%q] = %q, want %q", key, got, project)
	}
}

// TestIndexWorkDirsSkipsNoise keeps the scan from walking dependency trees, which
// on a real machine dwarfs everything else.
func TestIndexWorkDirsSkipsNoise(t *testing.T) {
	root := t.TempDir()
	noisy := filepath.Join(root, "app", "node_modules", "pkg")
	hidden := filepath.Join(root, ".cache", "thing")
	for _, d := range []string{noisy, hidden} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	index := indexWorkDirs([]string{root})
	for _, skipped := range []string{noisy, hidden} {
		if _, found := index[strings.ToLower(claudedir.Mangle(skipped))]; found {
			t.Errorf("walked into %q", skipped)
		}
	}
	if _, found := index[strings.ToLower(claudedir.Mangle(filepath.Join(root, "app")))]; !found {
		t.Error("the real project directory was skipped along with the noise")
	}
}

// TestIndexWorkDirsRespectsDepth stops a stray search root from scanning a whole
// disk.
func TestIndexWorkDirsRespectsDepth(t *testing.T) {
	root := t.TempDir()
	parts := make([]string, 0, maxScanDepth+3)
	for i := 0; i < maxScanDepth+3; i++ {
		parts = append(parts, "d")
	}
	deep := filepath.Join(append([]string{root}, parts...)...)
	if err := os.MkdirAll(deep, 0o755); err != nil {
		t.Fatal(err)
	}

	index := indexWorkDirs([]string{root})
	if _, found := index[strings.ToLower(claudedir.Mangle(deep))]; found {
		t.Errorf("scanned past the depth limit of %d", maxScanDepth)
	}
}

// planTarget builds the inputs planFile needs.
func planTarget(t *testing.T, typ, scope string) (frontmatter.Memory, identity.Identity, string, string) {
	t.Helper()
	root := t.TempDir()
	work := filepath.Join(root, "repo")
	personal := filepath.Join(root, "personal")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}

	src := filepath.Join(root, "memory", "note.md")
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: note\ndescription: a note\n"
	if scope != "" {
		body += "scope: " + scope + "\n"
	}
	body += "metadata:\n  type: " + typ + "\n---\n\nbody\n"
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	m, err := frontmatter.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	id := identity.Identity{
		Slug:      "github.com__anton__portable",
		Canonical: "github.com/anton/portable",
		Source:    identity.SourceRemote,
		Root:      work,
	}
	return m, id, personal, work
}

func TestPlanFileRouting(t *testing.T) {
	tests := []struct {
		name  string
		typ   string
		scope string
		want  string
	}{
		{"project memory into the work tree", "project", "", filepath.Join("repo", ".claude", "memory", "note.md")},
		{"user memory is global", "user", "", filepath.Join("personal", "global", "note.md")},
		{"feedback is project-scoped", "feedback", "", filepath.Join("personal", "projects", "github.com__anton__portable", "note.md")},
		{"reference is project-scoped", "reference", "", filepath.Join("personal", "projects", "github.com__anton__portable", "note.md")},
		{"scope override shares a reference", "reference", "project", filepath.Join("repo", ".claude", "memory", "note.md")},
		{"scope override keeps a decision private", "project", "personal", filepath.Join("personal", "projects", "github.com__anton__portable", "note.md")},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, id, personal, _ := planTarget(t, tc.typ, tc.scope)
			a := planFile(m, id, personal)
			if a.Blocked != "" {
				t.Fatalf("blocked: %s", a.Blocked)
			}
			if !strings.HasSuffix(filepath.ToSlash(a.Dest), filepath.ToSlash(tc.want)) {
				t.Errorf("Dest = %q, want it to end with %q", a.Dest, tc.want)
			}
		})
	}
}

func TestPlanFileBlocksWithoutPersonalLayer(t *testing.T) {
	m, id, _, _ := planTarget(t, "feedback", "")
	a := planFile(m, id, "")
	if a.Blocked == "" {
		t.Fatal("a personal memory was planned with no personal layer available")
	}
	if a.Dest != "" {
		t.Errorf("Dest = %q, want empty for a blocked action", a.Dest)
	}
}

func TestPlanFileBlocksWithoutWorkTree(t *testing.T) {
	m, id, personal, _ := planTarget(t, "project", "")
	id.Root = ""
	a := planFile(m, id, personal)
	if a.Blocked == "" {
		t.Fatal("project memory was planned with no work tree")
	}
}

// TestPlanFileFlagsSecrets is why migrate has a review step at all.
func TestPlanFileFlagsSecrets(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "leak.md")
	// The prefix is split so that a scanner reading this file sees no token
	// shape; the compiler still hands the classifier the whole value.
	body := "---\nname: leak\ndescription: has a token\nmetadata:\n  type: project\n---\n\nglpat-" + "FAKEfake1234567890ab\n"
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := frontmatter.Parse(src)
	if err != nil {
		t.Fatal(err)
	}

	a := planFile(m, identity.Identity{Root: root, Slug: "s"}, root)
	if len(a.Secrets) == 0 {
		t.Error("the credential was not flagged")
	}
}

// TestApplySkipsSecretsByDefault is the guarantee behind the printed warning: a
// flagged file is not copied into a layer that gets committed.
func TestApplySkipsSecretsByDefault(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "leak.md")
	if err := os.WriteFile(src, []byte("token glpat-"+"FAKEfake1234567890ab\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The action is built through the real classifier so the Secrets field is
	// populated the same way production does it.
	m, err := frontmatter.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	action := planFile(m, identity.Identity{Root: filepath.Join(root, "repo"), Slug: "s"}, filepath.Join(root, "personal"))
	plan := Plan{Groups: []Group{{Resolved: true, Actions: []Action{action}}}}

	written, skipped, err := Apply(plan, false)
	if err != nil {
		t.Fatal(err)
	}
	if written != 0 || skipped != 1 {
		t.Errorf("written=%d skipped=%d, want 0 and 1", written, skipped)
	}
	if _, err := os.Stat(action.Dest); action.Dest != "" && !os.IsNotExist(err) {
		t.Error("a flagged file was written into a layer")
	}
}

// TestApplyNeverDeletesTheSource keeps the path-keyed directory as a backup, so a
// wrong classification costs a rerun instead of a lost memory.
func TestApplyNeverDeletesTheSource(t *testing.T) {
	root := t.TempDir()
	src := filepath.Join(root, "note.md")
	body := "---\nname: note\ndescription: a note\nmetadata:\n  type: project\n---\n\nbody\n"
	if err := os.WriteFile(src, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	m, err := frontmatter.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(root, "repo")
	action := planFile(m, identity.Identity{Root: work, Slug: "s"}, filepath.Join(root, "personal"))

	written, _, err := Apply(Plan{Groups: []Group{{Resolved: true, Actions: []Action{action}}}}, false)
	if err != nil {
		t.Fatal(err)
	}
	if written != 1 {
		t.Fatalf("written = %d, want 1", written)
	}
	if _, err := os.Stat(src); err != nil {
		t.Errorf("the original was removed: %v", err)
	}
	if _, err := os.Stat(action.Dest); err != nil {
		t.Errorf("the copy was not written: %v", err)
	}
}
