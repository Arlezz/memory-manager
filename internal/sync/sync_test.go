package sync

import (
	"encoding/json"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Arlezz/memory-manager/internal/claudedir"
	"github.com/Arlezz/memory-manager/internal/config"
	"github.com/Arlezz/memory-manager/internal/identity"
)

// lab is an isolated environment: its own CLAUDE_CONFIG_DIR, a project
// directory, and a directory standing in for the personal clone.
type lab struct {
	t           *testing.T
	claudeDir   string
	projectDir  string
	personalDir string
}

// newLab builds the environment. Identity comes from the override file rather
// than a git remote so the test needs no git binary and no network.
func newLab(t *testing.T) *lab {
	t.Helper()
	root := t.TempDir()

	l := &lab{
		t:          t,
		claudeDir:  filepath.Join(root, "claude"),
		projectDir: filepath.Join(root, "project"),
		// This must match what config.PersonalClonePath derives, since that is
		// the only clone location sync consults.
		personalDir: filepath.Join(root, "claude", "memory-manager", "personal"),
	}
	t.Setenv("CLAUDE_CONFIG_DIR", l.claudeDir)

	mustMkdir(t, filepath.Join(l.projectDir, ".claude", "memory"))
	mustWrite(t, filepath.Join(l.projectDir, filepath.FromSlash(identity.OverrideFile)), "test/project\n")

	return l
}

// enablePersonal makes the fake clone look like a git checkout so sync treats
// the personal layer as available.
func (l *lab) enablePersonal() {
	l.t.Helper()
	mustMkdir(l.t, filepath.Join(l.personalDir, ".git"))
	mustMkdir(l.t, filepath.Join(l.personalDir, "global"))
	mustMkdir(l.t, filepath.Join(l.personalDir, "projects", "test__project"))
	if err := config.Save(config.Config{PersonalRepo: "file://" + filepath.ToSlash(l.personalDir) + "-origin"}); err != nil {
		l.t.Fatal(err)
	}
}

// breakPersonal simulates an unreachable remote with no usable clone.
func (l *lab) breakPersonal() {
	l.t.Helper()
	if err := os.RemoveAll(filepath.Join(l.personalDir, ".git")); err != nil {
		l.t.Fatal(err)
	}
}

// memoryDir is where the merge lands.
func (l *lab) memoryDir() string {
	l.t.Helper()
	dir, err := claudedir.MemoryDir(l.projectDir)
	if err != nil {
		l.t.Fatal(err)
	}
	return dir
}

func (l *lab) writeProjectMemory(name, typ, desc string) {
	l.t.Helper()
	mustWrite(l.t, filepath.Join(l.projectDir, ".claude", "memory", name), memoryFile(name, typ, desc))
}

func (l *lab) writePersonalGlobal(name, typ, desc string) {
	l.t.Helper()
	mustWrite(l.t, filepath.Join(l.personalDir, "global", name), memoryFile(name, typ, desc))
}

func (l *lab) writePersonalProject(name, typ, desc string) {
	l.t.Helper()
	mustWrite(l.t, filepath.Join(l.personalDir, "projects", "test__project", name), memoryFile(name, typ, desc))
}

func (l *lab) run() Result {
	l.t.Helper()
	res, err := Run(Options{Dir: l.projectDir})
	if err != nil {
		l.t.Fatalf("sync failed: %v", err)
	}
	return res
}

func (l *lab) merged() []string {
	l.t.Helper()
	entries, err := os.ReadDir(l.memoryDir())
	if err != nil {
		l.t.Fatalf("read merged dir: %v", err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestRunMergesBothLayers(t *testing.T) {
	l := newLab(t)
	l.enablePersonal()
	l.writeProjectMemory("decision.md", "project", "why git over blob storage")
	l.writePersonalGlobal("spanish.md", "feedback", "always reply in Spanish")
	l.writePersonalProject("scratch.md", "feedback", "note scoped to this project")

	res := l.run()
	if res.FromProject != 1 || res.FromPersonalGlobal != 1 || res.FromPersonalProject != 1 {
		t.Errorf("counts = %d/%d/%d, want 1/1/1",
			res.FromProject, res.FromPersonalGlobal, res.FromPersonalProject)
	}
	assertHas(t, l.merged(), "decision.md", "spanish.md", "scratch.md", "MEMORY.md")
}

// TestRunKeepsPersonalWhenLayerUnavailable is the regression test for a bug
// found in manual testing: an unreachable personal remote looked identical to an
// upstream deletion, so a network blink stripped memory the agent depended on.
func TestRunKeepsPersonalWhenLayerUnavailable(t *testing.T) {
	l := newLab(t)
	l.enablePersonal()
	l.writeProjectMemory("decision.md", "project", "why git over blob storage")
	l.writePersonalGlobal("spanish.md", "feedback", "always reply in Spanish")

	l.run()
	assertHas(t, l.merged(), "spanish.md")

	l.breakPersonal()
	res := l.run()

	if res.Removed != 0 {
		t.Errorf("Removed = %d, want 0: an unavailable layer is not a deletion", res.Removed)
	}
	assertHas(t, l.merged(), "decision.md", "spanish.md")
	if !hasWarning(res, "not deleted while the personal layer is unavailable") {
		t.Errorf("no warning explained the retained files: %v", res.Warnings)
	}

	// The retained file must stay tracked, or it silently becomes unmanaged.
	if layerOf(t, l, "spanish.md") != "personal" {
		t.Error("retained file lost its manifest entry")
	}
}

// TestRunPreservesUnpushedLocalEdit is the regression test for silent data
// loss: Claude writes memory straight into the native directory during a
// session, so an unconditional copy from the layer at the next session start
// destroys everything written since the last push.
func TestRunPreservesUnpushedLocalEdit(t *testing.T) {
	l := newLab(t)
	l.enablePersonal()
	l.writePersonalGlobal("spanish.md", "feedback", "always reply in Spanish")
	l.run()

	// Stand in for Claude editing the memory mid-session.
	edited := memoryFile("spanish.md", "feedback", "always reply in Spanish, even in code comments")
	mustWrite(t, filepath.Join(l.memoryDir(), "spanish.md"), edited)

	res := l.run()

	if got := readFile(t, filepath.Join(l.memoryDir(), "spanish.md")); got != edited {
		t.Errorf("the local edit was overwritten:\n%s", got)
	}
	if res.Preserved != 1 {
		t.Errorf("Preserved = %d, want 1", res.Preserved)
	}
	if !hasWarning(res, "unpushed local changes") {
		t.Errorf("the user was not told: %v", res.Warnings)
	}
	// The index has to describe the file that is actually on disk.
	if body := readFile(t, filepath.Join(l.memoryDir(), "MEMORY.md")); !strings.Contains(body, "even in code comments") {
		t.Errorf("index describes the layer version, not the local one:\n%s", body)
	}
	// The baseline must stay at the last synced hash so push still sees an edit.
	if layerOf(t, l, "spanish.md") != "personal" {
		t.Error("preserved file lost its manifest entry")
	}
}

// TestRunPreservesDivergedFile covers both sides changing: the layer moved and
// the local file was edited. The local copy wins and the warning says so.
func TestRunPreservesDivergedFile(t *testing.T) {
	l := newLab(t)
	l.enablePersonal()
	l.writePersonalGlobal("spanish.md", "feedback", "always reply in Spanish")
	l.run()

	mustWrite(t, filepath.Join(l.memoryDir(), "spanish.md"),
		memoryFile("spanish.md", "feedback", "local change"))
	l.writePersonalGlobal("spanish.md", "feedback", "upstream change")

	res := l.run()
	if body := readFile(t, filepath.Join(l.memoryDir(), "spanish.md")); !strings.Contains(body, "local change") {
		t.Errorf("upstream overwrote the local edit:\n%s", body)
	}
	if !hasWarning(res, "both sides changed") {
		t.Errorf("divergence not reported: %v", res.Warnings)
	}
}

// TestRunOverwritesUnmodifiedFile is the other half: when the local file is
// untouched, an upstream change must still arrive. Preserving too eagerly would
// freeze memory forever.
func TestRunOverwritesUnmodifiedFile(t *testing.T) {
	l := newLab(t)
	l.enablePersonal()
	l.writePersonalGlobal("spanish.md", "feedback", "first version")
	l.run()

	l.writePersonalGlobal("spanish.md", "feedback", "second version")
	res := l.run()

	if body := readFile(t, filepath.Join(l.memoryDir(), "spanish.md")); !strings.Contains(body, "second version") {
		t.Errorf("upstream change did not arrive:\n%s", body)
	}
	if res.Preserved != 0 {
		t.Errorf("Preserved = %d, want 0 for an untouched file", res.Preserved)
	}
}

// TestRunPropagatesRealDeletion is the counterpart: when the layer *is*
// available and the file is gone upstream, it must disappear locally too.
func TestRunPropagatesRealDeletion(t *testing.T) {
	l := newLab(t)
	l.enablePersonal()
	l.writePersonalGlobal("spanish.md", "feedback", "always reply in Spanish")
	l.run()

	if err := os.Remove(filepath.Join(l.personalDir, "global", "spanish.md")); err != nil {
		t.Fatal(err)
	}
	res := l.run()

	if res.Removed != 1 {
		t.Errorf("Removed = %d, want 1", res.Removed)
	}
	for _, name := range l.merged() {
		if name == "spanish.md" {
			t.Error("a deleted memory survived the sync")
		}
	}
}

// TestRunLeavesUntrackedMemoryAlone protects memory written directly into the
// native directory: it was never managed here, so deleting it would be data loss.
func TestRunLeavesUntrackedMemoryAlone(t *testing.T) {
	l := newLab(t)
	l.enablePersonal()
	l.writeProjectMemory("decision.md", "project", "why git over blob storage")
	l.run()

	stray := filepath.Join(l.memoryDir(), "stray.md")
	mustWrite(t, stray, memoryFile("stray.md", "user", "written straight into the native dir"))

	l.run()
	if _, err := os.Stat(stray); err != nil {
		t.Errorf("untracked memory was removed: %v", err)
	}
	// It must also be indexed, or the index contradicts the directory beside it.
	body := readFile(t, filepath.Join(l.memoryDir(), "MEMORY.md"))
	if !strings.Contains(body, "stray.md") {
		t.Errorf("untracked memory missing from the index:\n%s", body)
	}
}

// TestRunPersonalWinsCollision pins the documented precedence and requires the
// user be told, since one of the two files is being shadowed.
func TestRunPersonalWinsCollision(t *testing.T) {
	l := newLab(t)
	l.enablePersonal()
	l.writeProjectMemory("shared.md", "project", "the team default")
	l.writePersonalGlobal("shared.md", "feedback", "my personal override")

	res := l.run()
	body := readFile(t, filepath.Join(l.memoryDir(), "shared.md"))
	if !strings.Contains(body, "my personal override") {
		t.Errorf("project layer won the collision:\n%s", body)
	}
	if !hasWarning(res, "exists in both") {
		t.Errorf("collision was not reported: %v", res.Warnings)
	}
}

// TestRunDegradesWithoutIdentity checks the tool stays out of the way when it
// cannot key the project: no merge, no error, and an actionable warning.
func TestRunDegradesWithoutIdentity(t *testing.T) {
	l := newLab(t)
	if err := os.Remove(filepath.Join(l.projectDir, filepath.FromSlash(identity.OverrideFile))); err != nil {
		t.Fatal(err)
	}

	res := l.run()
	if !res.Degraded {
		t.Error("Degraded = false, want true without an identity")
	}
	if !hasWarning(res, "memory-manager init") {
		t.Errorf("warning does not point at the fix: %v", res.Warnings)
	}
}

// TestRunDryRunWritesNothing guards the flag people reach for before trusting
// the tool with a real directory.
func TestRunDryRunWritesNothing(t *testing.T) {
	l := newLab(t)
	l.enablePersonal()
	l.writeProjectMemory("decision.md", "project", "why git over blob storage")

	res, err := Run(Options{Dir: l.projectDir, DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.FromProject != 1 {
		t.Errorf("FromProject = %d, want 1", res.FromProject)
	}
	if _, err := os.Stat(l.memoryDir()); !os.IsNotExist(err) {
		t.Errorf("dry run created the memory directory: %v", err)
	}
}

// TestRunReportsFormatProblems checks a malformed memory is surfaced and still
// merged, rather than dropped where it would look like it was never written.
func TestRunReportsFormatProblems(t *testing.T) {
	l := newLab(t)
	l.enablePersonal()
	mustWrite(t, filepath.Join(l.projectDir, ".claude", "memory", "broken.md"), "no frontmatter here\n")

	res := l.run()
	if !hasWarning(res, "missing frontmatter") {
		t.Errorf("format problem not reported: %v", res.Warnings)
	}
	assertHas(t, l.merged(), "broken.md")
}

func memoryFile(base, typ, desc string) string {
	name := strings.TrimSuffix(base, ".md")
	return "---\nname: " + name + "\ndescription: " + desc + "\nmetadata:\n  type: " + typ + "\n---\n\n" + desc + "\n"
}

// layerOf reads the recorded layer for a file from the manifest on disk.
func layerOf(t *testing.T, l *lab, name string) string {
	t.Helper()
	path := filepath.Join(l.claudeDir, "memory-manager", "state", "test__project.json")
	var m struct {
		Entries map[string]struct {
			Layer string `json:"layer"`
		} `json:"entries"`
	}
	if err := json.Unmarshal([]byte(readFile(t, path)), &m); err != nil {
		t.Fatalf("manifest %s: %v", path, err)
	}
	return m.Entries[name].Layer
}

func hasWarning(res Result, substr string) bool {
	for _, w := range res.Warnings {
		if strings.Contains(w, substr) {
			return true
		}
	}
	return false
}

func assertHas(t *testing.T, got []string, want ...string) {
	t.Helper()
	set := map[string]bool{}
	for _, g := range got {
		set[g] = true
	}
	for _, w := range want {
		if !set[w] {
			t.Errorf("merged dir %v is missing %q", got, w)
		}
	}
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

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

// TestRunKeepsProjectWhenLayerUnavailable is the regression test for a real
// loss: six project memories were deleted from a machine because the project
// layer directory was absent at the moment of a sync, and layer.Read reports a
// missing directory as an empty one. The personal layer had this guard; the
// project layer did not.
func TestRunKeepsProjectWhenLayerUnavailable(t *testing.T) {
	l := newLab(t)
	l.writeProjectMemory("decision.md", "project", "why git over blob storage")
	l.run()
	assertHas(t, l.merged(), "decision.md")

	// Not "every memory was deleted" — the directory itself is gone.
	if err := os.RemoveAll(filepath.Join(l.projectDir, ".claude", "memory")); err != nil {
		t.Fatal(err)
	}
	res := l.run()

	if res.Removed != 0 {
		t.Errorf("Removed = %d, want 0: an absent project layer is not a deletion", res.Removed)
	}
	assertHas(t, l.merged(), "decision.md")
	if !hasWarning(res, "not deleted while the project layer is unavailable") {
		t.Errorf("no warning explained the retained files: %v", res.Warnings)
	}
	// Still tracked, or it silently becomes an unmanaged file.
	if got := layerOf(t, l, "decision.md"); got != "project" {
		t.Errorf("layer = %q, want project", got)
	}
}

// TestRunArchivesBeforeRemoving pins that a removal is recoverable. Deletions
// are derived from a manifest and from what a layer reports, both of which have
// been wrong, and this runs unattended from a hook.
func TestRunArchivesBeforeRemoving(t *testing.T) {
	l := newLab(t)
	l.enablePersonal()
	l.writePersonalGlobal("spanish.md", "user", "respond in Spanish")
	l.run()
	assertHas(t, l.merged(), "spanish.md")

	// A genuine upstream deletion: the layer is present, the file is not.
	if err := os.Remove(filepath.Join(l.personalDir, "global", "spanish.md")); err != nil {
		t.Fatal(err)
	}
	if res := l.run(); res.Removed != 1 {
		t.Fatalf("Removed = %d, want 1", res.Removed)
	}

	dir, err := ArchiveDir("test__project")
	if err != nil {
		t.Fatal(err)
	}
	var found string
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && filepath.Base(path) == "spanish.md" {
			found = path
		}
		return nil
	})
	if found == "" {
		t.Fatalf("the removed memory was not archived under %s", dir)
	}
	if !strings.Contains(readFile(t, found), "respond in Spanish") {
		t.Error("the archived copy does not hold the memory's content")
	}
}
