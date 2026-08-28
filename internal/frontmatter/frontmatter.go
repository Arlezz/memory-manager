// Package frontmatter parses the YAML header of a Claude Code memory file.
//
// It implements only the subset of YAML the memory format actually uses —
// flat scalars plus a one-level "metadata" map — so the binary keeps zero
// third-party dependencies. Anything richer is reported as a problem instead of
// being guessed at.
package frontmatter

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Delimiter opens and closes the frontmatter block.
const Delimiter = "---"

// Memory is one parsed memory file: one fact, per the memory format.
type Memory struct {
	// Path is the absolute path the memory was read from.
	Path string
	// Base is the file name, which is the identity of the memory on disk.
	Base string
	// Name is the frontmatter slug used by [[wikilinks]].
	Name string
	// Description is the one-line summary used to build the index.
	Description string
	// Type is one of user, feedback, project, reference.
	Type string
	// Scope, when set, overrides the layer implied by Type.
	Scope string
	// Body is everything after the closing delimiter.
	Body string
	// Problems lists format defects that degrade the generated index. A memory
	// with problems is still usable; the caller surfaces them rather than
	// dropping the file silently, because a dropped memory looks identical to a
	// memory that was never written.
	Problems []string
	// Notes lists advisory findings worth fixing but not worth repeating at
	// every session start. Reported by migrate, not by sync.
	Notes []string
}

// Valid types, per the memory format.
var validTypes = map[string]bool{
	"user":      true,
	"feedback":  true,
	"project":   true,
	"reference": true,
}

// Valid explicit scopes.
var validScopes = map[string]bool{
	"project":  true,
	"personal": true,
}

// Parse reads a memory file.
//
// It returns a Memory even for a malformed file: the caller needs the path and
// the raw body to report and to copy the file regardless.
func Parse(path string) (Memory, error) {
	f, err := os.Open(path)
	if err != nil {
		return Memory{}, err
	}
	defer f.Close()

	m := Memory{Path: path, Base: filepath.Base(path)}

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)

	var (
		inHeader  bool
		headerEnd bool
		inMeta    bool
		body      strings.Builder
		firstLine = true
	)

	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r")

		if firstLine {
			firstLine = false
			if strings.TrimSpace(line) == Delimiter {
				inHeader = true
				continue
			}
			m.Problems = append(m.Problems, "missing frontmatter: file does not start with "+Delimiter)
			headerEnd = true
			body.WriteString(line + "\n")
			continue
		}

		if inHeader {
			if strings.TrimSpace(line) == Delimiter {
				inHeader, headerEnd = false, true
				continue
			}
			indented := strings.HasPrefix(line, " ") || strings.HasPrefix(line, "\t")
			key, val, ok := splitKeyValue(line)
			if !ok {
				continue
			}
			if !indented {
				inMeta = false
			}
			switch {
			case key == "metadata" && val == "":
				inMeta = true
			case inMeta && indented:
				m.assign(key, val)
			case !indented:
				m.assign(key, val)
			}
			continue
		}

		if headerEnd {
			body.WriteString(line + "\n")
		}
	}
	if err := sc.Err(); err != nil {
		return m, fmt.Errorf("read %s: %w", path, err)
	}

	if inHeader {
		m.Problems = append(m.Problems, "unterminated frontmatter: no closing "+Delimiter)
	}
	m.Body = strings.TrimSpace(body.String())
	m.validate()
	return m, nil
}

// assign stores a recognized key. Unknown keys are ignored: the format is
// allowed to grow without this parser rejecting newer files.
func (m *Memory) assign(key, val string) {
	switch key {
	case "name":
		m.Name = val
	case "description":
		m.Description = val
	case "type":
		m.Type = strings.ToLower(val)
	case "scope":
		m.Scope = strings.ToLower(val)
	}
}

func (m *Memory) validate() {
	if m.Name == "" {
		m.Problems = append(m.Problems, "missing 'name'")
	}
	if m.Description == "" {
		m.Problems = append(m.Problems, "missing 'description': the generated index has nothing to show")
	}
	switch {
	case m.Type == "":
		m.Problems = append(m.Problems, "missing 'metadata.type'")
	case !validTypes[m.Type]:
		m.Problems = append(m.Problems, fmt.Sprintf("unknown type %q", m.Type))
	}
	if m.Scope != "" && !validScopes[m.Scope] {
		m.Problems = append(m.Problems, fmt.Sprintf("unknown scope %q (want project or personal)", m.Scope))
	}
	// A name that disagrees with the filename breaks [[wikilinks]], which are
	// resolved by name while files are addressed by path. This is advisory: much
	// of the existing corpus has underscored filenames with hyphenated names, and
	// repeating that at every session start would bury the real warnings.
	if m.Name != "" {
		want := strings.TrimSuffix(m.Base, filepath.Ext(m.Base))
		if m.Name != want {
			m.Notes = append(m.Notes, fmt.Sprintf("name %q does not match filename %q; wikilinks will not resolve", m.Name, want))
		}
	}
}

// splitKeyValue parses "key: value", tolerating comments and quoted values.
func splitKeyValue(line string) (key, val string, ok bool) {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" || strings.HasPrefix(trimmed, "#") {
		return "", "", false
	}
	i := strings.Index(trimmed, ":")
	if i <= 0 {
		return "", "", false
	}
	key = strings.TrimSpace(trimmed[:i])
	val = strings.TrimSpace(trimmed[i+1:])
	if len(val) >= 2 {
		if (val[0] == '"' && val[len(val)-1] == '"') || (val[0] == '\'' && val[len(val)-1] == '\'') {
			val = val[1 : len(val)-1]
		}
	}
	return key, val, true
}
