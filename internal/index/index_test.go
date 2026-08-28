package index

import (
	"strings"
	"testing"

	"github.com/Arlezz/memory-manager/internal/frontmatter"
)

func sample() []frontmatter.Memory {
	return []frontmatter.Memory{
		{Base: "respond-in-spanish.md", Name: "respond-in-spanish", Description: "Always reply in Spanish", Type: "feedback"},
		{Base: "memory-manager-goal.md", Name: "memory-manager-goal", Description: "Cross-device memory sync", Type: "project"},
		{Base: "anton.md", Name: "anton", Description: "Who the user is", Type: "user"},
		{Base: "dash.md", Name: "dash", Description: "Ops dashboard", Type: "reference"},
	}
}

func TestRenderOrdersSections(t *testing.T) {
	out := Render(sample())
	want := []string{"Who the user is", "How to work", "Projects and constraints", "References"}
	last := -1
	for _, section := range want {
		i := strings.Index(out, section)
		if i == -1 {
			t.Fatalf("section %q missing from:\n%s", section, out)
		}
		if i < last {
			t.Errorf("section %q appears out of order", section)
		}
		last = i
	}
}

func TestRenderLinksAndDescriptions(t *testing.T) {
	out := Render(sample())
	if !strings.Contains(out, "- [respond-in-spanish](respond-in-spanish.md) — Always reply in Spanish") {
		t.Errorf("entry not rendered as expected:\n%s", out)
	}
	if !strings.HasPrefix(out, Banner) {
		t.Errorf("index does not start with the generated banner")
	}
}

// TestRenderKeepsUnknownTypes is the counterpart of the parser's tolerance: a
// memory with a bad type still has to appear, or the index silently hides a fact.
func TestRenderKeepsUnknownTypes(t *testing.T) {
	out := Render([]frontmatter.Memory{
		{Base: "weird.md", Name: "weird", Description: "no valid type", Type: "notes"},
	})
	if !strings.Contains(out, "## Other") {
		t.Errorf("unknown type did not land in an Other section:\n%s", out)
	}
	if !strings.Contains(out, "weird.md") {
		t.Errorf("unknown-type memory is missing from the index:\n%s", out)
	}
}

func TestRenderMissingDescription(t *testing.T) {
	out := Render([]frontmatter.Memory{{Base: "x.md", Name: "x", Type: "user"}})
	if !strings.Contains(out, "(no description in frontmatter)") {
		t.Errorf("missing description not surfaced:\n%s", out)
	}
}

// TestRenderEmpty checks the index of an empty store is just the banner: no
// empty headings for Claude Code to read as meaningful.
func TestRenderEmpty(t *testing.T) {
	out := Render(nil)
	if strings.Contains(out, "##") {
		t.Errorf("empty store rendered headings:\n%s", out)
	}
}

// TestRenderIsDeterministic matters because the index is rewritten on every
// sync; unstable output would churn the file for no reason.
func TestRenderIsDeterministic(t *testing.T) {
	a := Render(sample())
	shuffled := sample()
	shuffled[0], shuffled[3] = shuffled[3], shuffled[0]
	if b := Render(shuffled); a != b {
		t.Errorf("render depends on input order:\n%s\n---\n%s", a, b)
	}
}
