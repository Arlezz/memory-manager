package sync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Arlezz/memory-manager/internal/claudedir"
	"github.com/Arlezz/memory-manager/internal/layer"
	"github.com/Arlezz/memory-manager/internal/state"
)

// TestCountRemovalsHonoursBothLayers is A-13. The dry run is the preview of the
// guard that was added after a real loss of data, and it was previewing only
// half of it: a project memory whose layer had vanished was counted as a
// removal the real run would refuse to perform.
func TestCountRemovalsHonoursBothLayers(t *testing.T) {
	prev := state.Manifest{Entries: map[string]state.Entry{
		"team.md":     {Layer: string(layer.Project)},
		"personal.md": {Layer: string(layer.Personal)},
	}}
	// Nothing survives into this sync, so every entry is a removal candidate.
	chosen := map[string]candidate{}

	cases := []struct {
		name                                string
		personalAvailable, projectAvailable bool
		want                                int
	}{
		{"both layers present", true, true, 2},
		{"project layer gone", true, false, 1},
		{"personal layer gone", false, true, 1},
		{"both layers gone", false, false, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := countRemovals(prev, chosen, c.personalAvailable, c.projectAvailable); got != c.want {
				t.Errorf("countRemovals = %d, want %d", got, c.want)
			}
		})
	}
}

// TestLastHookFailureSurfacesAndClears is A-15. The launcher exits zero by
// design, so a broken or missing binary showed up only as one line of stderr
// that nobody reads — while the loss this tool exists to prevent is a memory
// that never arrives.
func TestLastHookFailureSurfacesAndClears(t *testing.T) {
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(t.TempDir(), "claude"))

	if got := lastHookFailure(); got != "" {
		t.Errorf("lastHookFailure = %q with no record, want empty", got)
	}

	root, err := claudedir.Root()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "memory-manager")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	record := "2026-09-17T23:00:00.000Z sync: sync exited with 1; the session continued on local memory"
	if err := os.WriteFile(filepath.Join(dir, ErrorLogFile), []byte(record+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	got := lastHookFailure()
	if !strings.Contains(got, "exited with 1") {
		t.Errorf("the warning does not carry what failed: %q", got)
	}
	if !strings.Contains(got, "may be out of date") {
		t.Errorf("the warning does not say what it means for the session: %q", got)
	}
	if !strings.Contains(got, ErrorLogFile) {
		t.Errorf("the warning does not name the file: %q", got)
	}

	// An empty file is not a failure: it must not produce a warning that never
	// goes away.
	if err := os.WriteFile(filepath.Join(dir, ErrorLogFile), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if got := lastHookFailure(); got != "" {
		t.Errorf("lastHookFailure = %q for an empty record, want empty", got)
	}
}
