package layer

import (
	"path/filepath"
	"testing"

	"github.com/Arlezz/memory-manager/internal/frontmatter"
)

func TestForDefaults(t *testing.T) {
	tests := []struct {
		typ  string
		want Layer
	}{
		{"project", Project},
		{"user", Personal},
		{"feedback", Personal},
		{"reference", Personal},
		{"", Personal},
		{"nonsense", Personal},
	}
	for _, tc := range tests {
		got := For(frontmatter.Memory{Type: tc.typ})
		if got != tc.want {
			t.Errorf("For(type=%q) = %q, want %q", tc.typ, got, tc.want)
		}
	}
}

// TestForScopeWins pins that the explicit frontmatter scope overrides the
// type-derived default in both directions.
func TestForScopeWins(t *testing.T) {
	if got := For(frontmatter.Memory{Type: "reference", Scope: "project"}); got != Project {
		t.Errorf("scope=project on a reference gave %q, want project", got)
	}
	if got := For(frontmatter.Memory{Type: "project", Scope: "personal"}); got != Personal {
		t.Errorf("scope=personal on a project memory gave %q, want personal", got)
	}
}

// TestForUnknownTypeStaysPersonal is the safety property: an unrecognized type
// must never default into the shared repository.
func TestForUnknownTypeStaysPersonal(t *testing.T) {
	for _, typ := range []string{"", "notes", "todo", "PROJECT "} {
		if got := For(frontmatter.Memory{Type: typ}); got != Personal {
			t.Errorf("For(type=%q) = %q, want personal", typ, got)
		}
	}
}

func TestPersonalPath(t *testing.T) {
	root := filepath.FromSlash("/clone")
	if got, want := PersonalPath(root, ""), filepath.Join(root, "global"); got != want {
		t.Errorf("PersonalPath(global) = %q, want %q", got, want)
	}
	slug := "github.com__orbit-dev__orbit-x_core"
	if got, want := PersonalPath(root, slug), filepath.Join(root, "projects", slug); got != want {
		t.Errorf("PersonalPath(project) = %q, want %q", got, want)
	}
}
