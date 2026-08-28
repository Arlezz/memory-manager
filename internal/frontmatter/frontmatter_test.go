package frontmatter

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// write creates a memory file and returns its path.
func write(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestParseRealShape(t *testing.T) {
	// This is the exact shape of the memory files already on disk.
	path := write(t, "respond-in-spanish.md", `---
name: respond-in-spanish
description: Golden rule — always reply in Spanish
metadata:
  type: feedback
---

Anton set a standing rule. Links to [[memory-manager-goal]].
`)

	m, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "respond-in-spanish" {
		t.Errorf("Name = %q", m.Name)
	}
	if m.Description != "Golden rule — always reply in Spanish" {
		t.Errorf("Description = %q", m.Description)
	}
	if m.Type != "feedback" {
		t.Errorf("Type = %q", m.Type)
	}
	if m.Scope != "" {
		t.Errorf("Scope = %q, want empty", m.Scope)
	}
	if len(m.Problems) != 0 {
		t.Errorf("Problems = %v, want none", m.Problems)
	}
	if !strings.Contains(m.Body, "standing rule") {
		t.Errorf("Body = %q", m.Body)
	}
}

func TestParseScopeOverride(t *testing.T) {
	path := write(t, "dash.md", `---
name: dash
description: A shared dashboard
scope: project
metadata:
  type: reference
---
body
`)
	m, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.Scope != "project" {
		t.Errorf("Scope = %q, want project", m.Scope)
	}
	if m.Type != "reference" {
		t.Errorf("Type = %q, want reference", m.Type)
	}
}

// TestParseNestedMetadataOnly checks that a "type" inside metadata is picked up
// even though the parser also accepts it at the top level.
func TestParseTopLevelType(t *testing.T) {
	path := write(t, "x.md", `---
name: x
description: d
type: project
---
body
`)
	m, err := Parse(path)
	if err != nil {
		t.Fatal(err)
	}
	if m.Type != "project" {
		t.Errorf("Type = %q", m.Type)
	}
}

func TestParseReportsDefects(t *testing.T) {
	tests := []struct {
		name    string
		file    string
		content string
		want    string
	}{
		{"no frontmatter", "a.md", "just a body\n", "missing frontmatter"},
		{"unterminated", "b.md", "---\nname: b\n", "unterminated frontmatter"},
		{"missing description", "c.md", "---\nname: c\nmetadata:\n  type: user\n---\n", "missing 'description'"},
		{"unknown type", "d.md", "---\nname: d\ndescription: x\nmetadata:\n  type: notes\n---\n", "unknown type"},
		{"unknown scope", "e.md", "---\nname: e\ndescription: x\nscope: team\nmetadata:\n  type: user\n---\n", "unknown scope"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m, err := Parse(write(t, tc.file, tc.content))
			if err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(m.Problems, " | ")
			if !strings.Contains(joined, tc.want) {
				t.Errorf("Problems = %q, want one containing %q", joined, tc.want)
			}
		})
	}
}

// TestParseNameMismatchIsAdvisory keeps the filename check out of Problems: a
// large part of the existing corpus trips it, and repeating it at every session
// start would bury the warnings that matter.
func TestParseNameMismatchIsAdvisory(t *testing.T) {
	m, err := Parse(write(t, "f.md", `---
name: other
description: x
metadata:
  type: user
---
`))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Problems) != 0 {
		t.Errorf("Problems = %v, want the mismatch reported as a note", m.Problems)
	}
	if !strings.Contains(strings.Join(m.Notes, " "), "does not match filename") {
		t.Errorf("Notes = %v, want the filename mismatch", m.Notes)
	}
}

// TestParseKeepsBodyOnDefect is the guarantee that a malformed file is never
// dropped: a lost memory is indistinguishable from one that was never written.
func TestParseKeepsBodyOnDefect(t *testing.T) {
	m, err := Parse(write(t, "g.md", "no header at all\nsecond line\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(m.Body, "no header at all") || !strings.Contains(m.Body, "second line") {
		t.Errorf("Body lost content: %q", m.Body)
	}
}

func TestParseQuotedAndCommented(t *testing.T) {
	m, err := Parse(write(t, "h.md", `---
# a comment
name: "h"
description: 'quoted: with a colon'
metadata:
  type: user
---
body
`))
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "h" {
		t.Errorf("Name = %q", m.Name)
	}
	if m.Description != "quoted: with a colon" {
		t.Errorf("Description = %q", m.Description)
	}
}

func TestParseCRLF(t *testing.T) {
	m, err := Parse(write(t, "i.md", "---\r\nname: i\r\ndescription: d\r\nmetadata:\r\n  type: user\r\n---\r\nbody\r\n"))
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Problems) != 0 {
		t.Errorf("Problems = %v, want none for CRLF input", m.Problems)
	}
}
