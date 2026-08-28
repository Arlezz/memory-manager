package writeback

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Arlezz/memory-manager/internal/claudedir"
	"github.com/Arlezz/memory-manager/internal/config"
	"github.com/Arlezz/memory-manager/internal/identity"
	"github.com/Arlezz/memory-manager/internal/layer"
	"github.com/Arlezz/memory-manager/internal/state"
	"github.com/Arlezz/memory-manager/internal/sync"
)

// lab is an isolated environment: its own CLAUDE_CONFIG_DIR, a project work
// tree, and a directory standing in for the personal clone.
//
// Identity comes from the override file, and the fake clone only needs a .git
// directory to be considered present, so nothing here needs git or a network.
type lab struct {
	t           *testing.T
	claudeDir   string
	projectDir  string
	personalDir string
}

const slug = "test__project"

func newLab(t *testing.T) *lab {
	t.Helper()
	root := t.TempDir()
	l := &lab{
		t:           t,
		claudeDir:   filepath.Join(root, "claude"),
		projectDir:  filepath.Join(root, "project"),
		personalDir: filepath.Join(root, "claude", "memory-manager", "personal"),
	}
	t.Setenv("CLAUDE_CONFIG_DIR", l.claudeDir)

	mustMkdir(t, filepath.Join(l.projectDir, ".claude", "memory"))
	mustWrite(t, filepath.Join(l.projectDir, filepath.FromSlash(identity.OverrideFile)), "test/project\n")

	mustMkdir(t, filepath.Join(l.personalDir, ".git"))
	mustMkdir(t, filepath.Join(l.personalDir, "global"))
	mustMkdir(t, filepath.Join(l.personalDir, "projects", slug))
	if err := config.Save(config.Config{PersonalRepo: "file:///stand-in"}); err != nil {
		t.Fatal(err)
	}
	return l
}

func (l *lab) memoryDir() string {
	l.t.Helper()
	dir, err := claudedir.MemoryDir(l.projectDir)
	if err != nil {
		l.t.Fatal(err)
	}
	return dir
}

// seed puts a memory in a layer and syncs, so the manifest has a baseline. This
// is the state every real push starts from.
func (l *lab) seed(name, typ, desc string, lyr layer.Layer) {
	l.t.Helper()
	var path string
	if lyr == layer.Project {
		path = filepath.Join(l.projectDir, ".claude", "memory", name)
	} else {
		path = filepath.Join(l.personalDir, "projects", slug, name)
	}
	mustWrite(l.t, path, memoryFile(name, typ, desc))
	if _, err := sync.Run(sync.Options{Dir: l.projectDir}); err != nil {
		l.t.Fatalf("seed sync: %v", err)
	}
}

// writeNative simulates Claude writing a memory during a session.
func (l *lab) writeNative(name, typ, desc string) {
	l.t.Helper()
	mustWrite(l.t, filepath.Join(l.memoryDir(), name), memoryFile(name, typ, desc))
}

func (l *lab) build() Plan {
	l.t.Helper()
	plan, err := Build(l.projectDir)
	if err != nil {
		l.t.Fatalf("Build: %v", err)
	}
	return plan
}

func (l *lab) apply(opts Options) Result {
	l.t.Helper()
	res, err := Apply(l.build(), opts)
	if err != nil {
		// A commit failure is expected here: the stand-in clone is not a real
		// repository. The file writes still have to have happened.
		if !strings.Contains(err.Error(), "git") && !strings.Contains(err.Error(), "not a git repository") {
			l.t.Fatalf("Apply: %v", err)
		}
	}
	return res
}

func TestBuildClassifiesNewMemory(t *testing.T) {
	l := newLab(t)
	mustMkdir(t, l.memoryDir())
	l.writeNative("new-note.md", "feedback", "written during the session")

	plan := l.build()
	a := findAction(t, plan, "new-note.md")

	if a.Change != Added {
		t.Errorf("Change = %q, want added", a.Change)
	}
	if a.Layer != layer.Personal {
		t.Errorf("Layer = %q, want personal", a.Layer)
	}
	// Feedback found inside a project stays scoped to that project.
	want := filepath.Join(l.personalDir, "projects", slug, "new-note.md")
	if a.Dest != want {
		t.Errorf("Dest = %q, want %q", a.Dest, want)
	}
}

// TestBuildRoutesUserMemoryGlobally pins the rule that identity applies
// everywhere, so it must not be filed under one project.
func TestBuildRoutesUserMemoryGlobally(t *testing.T) {
	l := newLab(t)
	mustMkdir(t, l.memoryDir())
	l.writeNative("who-i-am.md", "user", "who the user is")

	a := findAction(t, l.build(), "who-i-am.md")
	want := filepath.Join(l.personalDir, "global", "who-i-am.md")
	if a.Dest != want {
		t.Errorf("Dest = %q, want %q", a.Dest, want)
	}
}

func TestBuildRoutesProjectMemoryToWorkTree(t *testing.T) {
	l := newLab(t)
	mustMkdir(t, l.memoryDir())
	l.writeNative("decision.md", "project", "why git over blob storage")

	a := findAction(t, l.build(), "decision.md")
	if a.Layer != layer.Project {
		t.Errorf("Layer = %q, want project", a.Layer)
	}
	want := filepath.Join(l.projectDir, ".claude", "memory", "decision.md")
	if a.Dest != want {
		t.Errorf("Dest = %q, want %q", a.Dest, want)
	}
}

func TestBuildIgnoresUnchangedMemory(t *testing.T) {
	l := newLab(t)
	l.seed("note.md", "feedback", "unchanged", layer.Personal)

	if plan := l.build(); !plan.Empty() {
		t.Errorf("plan has %d action(s) for an unchanged store: %+v", len(plan.Actions), plan.Actions)
	}
}

func TestBuildClassifiesEdit(t *testing.T) {
	l := newLab(t)
	l.seed("note.md", "feedback", "first version", layer.Personal)
	l.writeNative("note.md", "feedback", "edited during the session")

	a := findAction(t, l.build(), "note.md")
	if a.Change != Updated {
		t.Errorf("Change = %q, want updated", a.Change)
	}
	// An edit goes back where it came from, which preserves global vs
	// project-scoped placement inside the personal repo.
	want := filepath.Join(l.personalDir, "projects", slug, "note.md")
	if a.Dest != want {
		t.Errorf("Dest = %q, want %q", a.Dest, want)
	}
}

func TestBuildClassifiesDeletion(t *testing.T) {
	l := newLab(t)
	l.seed("gone.md", "feedback", "will be deleted", layer.Personal)
	l.seed("stays.md", "feedback", "stays", layer.Personal)

	if err := os.Remove(filepath.Join(l.memoryDir(), "gone.md")); err != nil {
		t.Fatal(err)
	}

	a := findAction(t, l.build(), "gone.md")
	if a.Change != Removed {
		t.Errorf("Change = %q, want removed", a.Change)
	}
	if a.DeleteFrom == "" {
		t.Error("DeleteFrom is empty; the layer copy would survive")
	}
}

// TestBuildMovesMemoryBetweenLayers covers the case that leaves duplicates if it
// is missed: an edit that changes where the memory belongs.
func TestBuildMovesMemoryBetweenLayers(t *testing.T) {
	l := newLab(t)
	l.seed("note.md", "feedback", "started as personal", layer.Personal)
	oldPath := filepath.Join(l.personalDir, "projects", slug, "note.md")

	// The same memory, now declared as project knowledge.
	l.writeNative("note.md", "project", "turned out to be a team decision")

	a := findAction(t, l.build(), "note.md")
	if a.Change != Moved {
		t.Fatalf("Change = %q, want moved", a.Change)
	}
	if a.FromLayer != layer.Personal || a.Layer != layer.Project {
		t.Errorf("move is %q -> %q, want personal -> project", a.FromLayer, a.Layer)
	}
	if a.DeleteFrom != oldPath {
		t.Errorf("DeleteFrom = %q, want %q", a.DeleteFrom, oldPath)
	}

	l.apply(Options{})
	if _, err := os.Stat(oldPath); !os.IsNotExist(err) {
		t.Error("the memory still exists in the personal layer; it is now duplicated")
	}
	if _, err := os.Stat(filepath.Join(l.projectDir, ".claude", "memory", "note.md")); err != nil {
		t.Errorf("the memory did not arrive in the project layer: %v", err)
	}
}

// TestBuildScopeOverrideMoves checks the explicit frontmatter override drives a
// move the same way the type does.
func TestBuildScopeOverrideMoves(t *testing.T) {
	l := newLab(t)
	l.seed("dash.md", "reference", "an ops dashboard", layer.Personal)

	mustWrite(t, filepath.Join(l.memoryDir(), "dash.md"),
		"---\nname: dash\ndescription: an ops dashboard the team needs\nscope: project\nmetadata:\n  type: reference\n---\n\nshared\n")

	a := findAction(t, l.build(), "dash.md")
	if a.Change != Moved || a.Layer != layer.Project {
		t.Errorf("scope override gave %q to %q, want moved to project", a.Change, a.Layer)
	}
}

// TestBuildBlocksSecrets is the guard that matters most here: push runs
// unattended from a SessionEnd hook, so nobody reads the warning in time.
func TestBuildBlocksSecrets(t *testing.T) {
	l := newLab(t)
	mustMkdir(t, l.memoryDir())
	mustWrite(t, filepath.Join(l.memoryDir(), "leak.md"),
		"---\nname: leak\ndescription: has a token\nmetadata:\n  type: project\n---\n\nexport GITLAB_TOKEN=glpat-" + "FAKEfake1234567890ab\n")

	a := findAction(t, l.build(), "leak.md")
	if a.Blocked == "" {
		t.Fatal("a file with a credential was not blocked")
	}
	if len(a.Secrets) == 0 {
		t.Error("no finding was attached to explain the block")
	}

	res := l.apply(Options{})
	if res.Blocked != 1 {
		t.Errorf("Blocked = %d, want 1", res.Blocked)
	}
	if _, err := os.Stat(filepath.Join(l.projectDir, ".claude", "memory", "leak.md")); !os.IsNotExist(err) {
		t.Error("the credential was written into the project layer anyway")
	}
}

// TestBuildRefusesWholesaleDeletion is a safety valve: a wiped native directory
// looks exactly like deleting every memory on purpose.
func TestBuildRefusesWholesaleDeletion(t *testing.T) {
	l := newLab(t)
	l.seed("a.md", "feedback", "first", layer.Personal)
	l.seed("b.md", "feedback", "second", layer.Personal)

	if err := os.RemoveAll(l.memoryDir()); err != nil {
		t.Fatal(err)
	}

	plan := l.build()
	for _, a := range plan.Actions {
		if a.Change == Removed {
			t.Errorf("propagated a wholesale deletion: %s", a.Base)
		}
	}
	if !hasWarning(plan.Warnings, "refusing to propagate") {
		t.Errorf("the refusal was not explained: %v", plan.Warnings)
	}
}

func TestApplyWritesAndRecords(t *testing.T) {
	l := newLab(t)
	mustMkdir(t, l.memoryDir())
	l.writeNative("decision.md", "project", "why git over blob storage")

	res := l.apply(Options{})
	if res.ProjectWritten != 1 {
		t.Errorf("ProjectWritten = %d, want 1", res.ProjectWritten)
	}
	if len(res.ProjectFiles) != 1 {
		t.Errorf("ProjectFiles = %v, want one entry so the user knows what to commit", res.ProjectFiles)
	}

	dest := filepath.Join(l.projectDir, ".claude", "memory", "decision.md")
	if _, err := os.Stat(dest); err != nil {
		t.Fatalf("file not written: %v", err)
	}

	// The manifest must now point at the layer copy, or the next push would
	// classify the same file as new all over again.
	m, err := state.Load(slug)
	if err != nil {
		t.Fatal(err)
	}
	entry, ok := m.Entries["decision.md"]
	if !ok {
		t.Fatal("no manifest entry was recorded")
	}
	if entry.Origin != dest || entry.Layer != string(layer.Project) {
		t.Errorf("entry = %+v, want origin %q in the project layer", entry, dest)
	}

	if plan := l.build(); !plan.Empty() {
		t.Errorf("a second push still sees work: %+v", plan.Actions)
	}
}

func TestApplyDryRunWritesNothing(t *testing.T) {
	l := newLab(t)
	mustMkdir(t, l.memoryDir())
	l.writeNative("decision.md", "project", "why git over blob storage")

	res, err := Apply(l.build(), Options{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.ProjectWritten != 1 {
		t.Errorf("ProjectWritten = %d, want 1 counted", res.ProjectWritten)
	}
	if _, err := os.Stat(filepath.Join(l.projectDir, ".claude", "memory", "decision.md")); !os.IsNotExist(err) {
		t.Error("dry run wrote to the work tree")
	}
}

// TestApplyDeletesFromLayer completes the round trip for a deletion.
func TestApplyDeletesFromLayer(t *testing.T) {
	l := newLab(t)
	l.seed("gone.md", "project", "will be deleted", layer.Project)
	l.seed("stays.md", "project", "stays", layer.Project)
	origin := filepath.Join(l.projectDir, ".claude", "memory", "gone.md")

	if err := os.Remove(filepath.Join(l.memoryDir(), "gone.md")); err != nil {
		t.Fatal(err)
	}
	l.apply(Options{})

	if _, err := os.Stat(origin); !os.IsNotExist(err) {
		t.Error("the deleted memory survived in the layer")
	}
	m, _ := state.Load(slug)
	if _, ok := m.Entries["gone.md"]; ok {
		t.Error("the manifest still tracks the deleted memory")
	}
}

// TestBuildWithoutIdentityFails checks push refuses to guess where memory goes.
func TestBuildWithoutIdentityFails(t *testing.T) {
	l := newLab(t)
	if err := os.Remove(filepath.Join(l.projectDir, filepath.FromSlash(identity.OverrideFile))); err != nil {
		t.Fatal(err)
	}
	_, err := Build(l.projectDir)
	if err == nil {
		t.Fatal("Build succeeded without an identity")
	}
	if !strings.Contains(err.Error(), "memory-manager init") {
		t.Errorf("error does not point at the fix: %v", err)
	}
}

func memoryFile(base, typ, desc string) string {
	name := strings.TrimSuffix(base, ".md")
	return "---\nname: " + name + "\ndescription: " + desc + "\nmetadata:\n  type: " + typ + "\n---\n\n" + desc + "\n"
}

func findAction(t *testing.T, plan Plan, base string) Action {
	t.Helper()
	for _, a := range plan.Actions {
		if a.Base == base {
			return a
		}
	}
	t.Fatalf("no action for %q; plan has %+v", base, plan.Actions)
	return Action{}
}

func hasWarning(warnings []string, substr string) bool {
	for _, w := range warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}
