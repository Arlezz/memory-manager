// Package secrets scans memory text for credentials.
//
// It exists because the project layer is committed into shared repositories,
// and a secret that reaches a shared history is not removed by a revert. The
// scan runs before anything is written, and it is advisory: it reports, the
// human decides.
package secrets

import (
	"fmt"
	"math"
	"regexp"
	"strings"
)

// Finding is one suspected credential.
type Finding struct {
	// Rule names the pattern that matched.
	Rule string
	// Line is the 1-indexed line number.
	Line int
	// Excerpt is the matched text, already masked.
	Excerpt string
}

func (f Finding) String() string {
	return fmt.Sprintf("line %d: %s (%s)", f.Line, f.Rule, f.Excerpt)
}

// rule is a named pattern.
type rule struct {
	name string
	re   *regexp.Regexp
}

// rules covers the token shapes in use across this user's remotes plus the
// common cloud provider formats. Provider prefixes are matched rather than
// guessed at by entropy alone, because prefix matches carry no false positives.
var rules = []rule{
	{"credential in URL", regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^/\s:@]+:[^/\s@]+@`)},
	{"GitLab personal access token", regexp.MustCompile(`glpat-[A-Za-z0-9_\-]{16,}`)},
	{"GitLab CI/deploy token", regexp.MustCompile(`GITLAB[A-Z0-9]*-[A-Za-z0-9_\-]{16,}`)},
	{"GitHub token", regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{16,}`)},
	{"GitHub fine-grained PAT", regexp.MustCompile(`github_pat_[A-Za-z0-9_]{20,}`)},
	{"AWS access key id", regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)},
	{"Anthropic API key", regexp.MustCompile(`sk-ant-[A-Za-z0-9_\-]{20,}`)},
	{"OpenAI API key", regexp.MustCompile(`\bsk-(?:proj-)?[A-Za-z0-9_\-]{32,}`)},
	{"Google API key", regexp.MustCompile(`\bAIza[0-9A-Za-z_\-]{35}\b`)},
	{"Slack token", regexp.MustCompile(`xox[baprs]-[A-Za-z0-9\-]{10,}`)},
	{"private key block", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{"assigned secret", regexp.MustCompile(`(?i)\b(?:password|passwd|secret|api[_-]?key|access[_-]?token|auth[_-]?token|client[_-]?secret)\b\s*[:=]\s*["']?[^\s"',;]{8,}`)},
}

// codeFence matches a fence marker line. Only the marker is skipped, never the
// code inside it: a fenced example is exactly where a real token gets pasted by
// mistake.
var codeFence = regexp.MustCompile("^[ \t]*```")

// Scan reports suspected credentials in text.
func Scan(text string) []Finding {
	var out []Finding
	for i, line := range strings.Split(text, "\n") {
		if codeFence.MatchString(line) {
			continue
		}
		for _, r := range rules {
			if m := r.re.FindString(line); m != "" {
				out = append(out, Finding{Rule: r.name, Line: i + 1, Excerpt: Mask(m)})
			}
		}
		for _, f := range scanEntropy(line, i+1) {
			out = append(out, f)
		}
	}
	return dedupe(out)
}

// entropyCandidate matches long unbroken tokens worth an entropy check.
//
// Slashes are excluded deliberately: branch names and file paths are the most
// common long strings in a memory file, and including them made the entropy rule
// fire on almost every project note. A scan that cries wolf gets ignored, which
// is worse than not scanning.
var entropyCandidate = regexp.MustCompile(`[A-Za-z0-9+_\-=]{32,}`)

// entropyThreshold is bits per character. Base64-ish secrets sit near 5.0 to
// 6.0; prose, slugs and identifiers sit below 4.2.
const entropyThreshold = 4.2

// minAssignedValue is the shortest right-hand side of an assignment still worth
// an entropy check.
//
// It is below the 32 the regexp demands of a bare token because the name has
// already told us this is a value: "TOKEN=" is context a loose string does not
// carry. Nothing shorter than this reads as a credential.
const minAssignedValue = 20

// scanEntropy catches token formats no prefix rule knows about.
//
// The filters below are all there to hold the false-positive rate near zero on
// real memory text; a missed exotic token is caught by review, a noisy report is
// not caught by anything.
func scanEntropy(line string, lineNo int) []Finding {
	var out []Finding
	for _, cand := range entropyCandidate.FindAllString(line, -1) {
		val := assignedValue(cand)
		if isHex(val) || looksLikeIdentifier(val) || !hasDigitAndLetter(val) {
			continue
		}
		if shannon(val) < entropyThreshold {
			continue
		}
		out = append(out, Finding{
			Rule:    "high-entropy string",
			Line:    lineNo,
			Excerpt: Mask(val),
		})
	}
	return out
}

// assignedValue reduces "NAME=value" to the value, and returns anything else
// unchanged.
//
// The name is not part of the secret, so it must not lend the candidate its
// length or its entropy. It was doing both: "UV_INDEX_NOVAIA_USERNAME=oauth2
// accesstoken" assigns a public constant, but scored as one 42-character token
// it cleared both bars on the strength of the variable name alone. Four of the
// corpus's 117 files were blocked from migrating that way, all false.
//
// Trailing "=" is stripped before looking for the separator, because base64
// padding is not an assignment; the padding is kept on the value itself, which
// is what the entropy check wants.
func assignedValue(s string) string {
	i := strings.Index(strings.TrimRight(s, "="), "=")
	if i < 0 {
		return s
	}
	value := s[i+1:]
	if len(value) < minAssignedValue {
		// Too short to be a credential. Returning the name would put its
		// entropy back in play, so return nothing and let the filters drop it.
		return ""
	}
	return value
}

// looksLikeIdentifier reports whether s decomposes into words, which makes it a
// name rather than a secret.
func looksLikeIdentifier(s string) bool {
	// Insert a break at each case transition so camelCase splits into words.
	var b strings.Builder
	runes := []rune(s)
	for i, r := range runes {
		if i > 0 && isUpper(r) && !isUpper(runes[i-1]) {
			b.WriteByte('_')
		}
		b.WriteRune(r)
	}
	parts := strings.FieldsFunc(b.String(), func(r rune) bool {
		return strings.ContainsRune("_-.=+", r)
	})
	if len(parts) < 2 {
		return false
	}
	for _, p := range parts {
		if isAlpha(p) || isShortNumber(p) || isRevision(p) {
			continue
		}
		return false
	}
	return true
}

// isRevision accepts a hex segment such as a git SHA or an Alembic revision id.
//
// A migration filename like "f7b2c0a1d3e4_add_client_catalog.py" is one hex
// segment followed by words, which is not a credential shape and appeared in the
// real corpus.
func isRevision(s string) bool {
	return len(s) >= 8 && isHex(s)
}

// hasDigitAndLetter requires the mix that generated credentials have and that
// prose does not.
func hasDigitAndLetter(s string) bool {
	var digit, letter bool
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
			digit = true
		case (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z'):
			letter = true
		}
	}
	return digit && letter
}

// isHex skips git hashes and checksums, which are not secrets.
func isHex(s string) bool {
	for _, r := range s {
		if !strings.ContainsRune("0123456789abcdefABCDEF", r) {
			return false
		}
	}
	return true
}

func isAlpha(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')) {
			return false
		}
	}
	return true
}

// isShortNumber accepts version and date fragments such as "1", "2026" or "07".
func isShortNumber(s string) bool {
	if s == "" || len(s) > 4 {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func isUpper(r rune) bool { return r >= 'A' && r <= 'Z' }

// shannon returns the Shannon entropy of s in bits per character.
func shannon(s string) float64 {
	if s == "" {
		return 0
	}
	counts := map[rune]int{}
	for _, r := range s {
		counts[r]++
	}
	n := float64(len(s))
	var h float64
	for _, c := range counts {
		p := float64(c) / n
		h -= p * math.Log2(p)
	}
	return h
}

// Mask reduces a match to a shape that identifies it without reproducing it.
//
// Findings are printed to a terminal and may be pasted into a chat or a ticket,
// so the report itself must not become a second copy of the secret.
func Mask(s string) string {
	const keep = 4
	if len(s) <= keep*2 {
		return strings.Repeat("*", len(s))
	}
	return s[:keep] + strings.Repeat("*", 8) + s[len(s)-keep:]
}

// entropyRule is the name used by the fallback heuristic.
const entropyRule = "high-entropy string"

// dedupe removes repeats and suppresses the entropy heuristic on lines a named
// rule already matched.
//
// A GitLab token matches both the prefix rule and the entropy rule, and printing
// the same secret twice makes the report look unreliable.
func dedupe(in []Finding) []Finding {
	named := map[int]bool{}
	for _, f := range in {
		if f.Rule != entropyRule {
			named[f.Line] = true
		}
	}

	seen := map[string]bool{}
	var out []Finding
	for _, f := range in {
		if f.Rule == entropyRule && named[f.Line] {
			continue
		}
		key := fmt.Sprintf("%d|%s|%s", f.Line, f.Rule, f.Excerpt)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, f)
	}
	return out
}
