package gitx

import (
	"strings"
	"testing"
)

// TestRunRedactsCredentialsInItsError covers the widest of the three leak paths
// of A-03: this error travels from personal.Open up to main and out on stderr,
// which launch.js inherits and Claude Code captures. A failing clone is exactly
// when it fires, and a failing clone is exactly when the URL is wrong — which is
// when a user is most likely to have pasted one carrying a token.
func TestRunRedactsCredentialsInItsError(t *testing.T) {
	requireGit(t)

	const url = "https://arlezz:github_pat_11ABCDEFG0aBcDeFgHiJkL@github.invalid/acme-dev/nope.git"
	_, err := Run("", "clone", "--quiet", "--", url, t.TempDir()+"/clone")
	if err == nil {
		t.Fatal("cloning an invalid host succeeded, so there is no error to inspect")
	}

	msg := err.Error()
	if strings.Contains(msg, "github_pat_") {
		t.Errorf("the token is in the error message:\n%s", msg)
	}
	// The rest of the command line has to survive, or the error stops being
	// useful for the reason it exists.
	if !strings.Contains(msg, "***@github.invalid/acme-dev/nope.git") {
		t.Errorf("the redacted URL is missing from the error:\n%s", msg)
	}
}

// TestRedactArgsLeavesNonURLArgumentsAlone keeps the redaction from damaging the
// error it is meant to make safe. Only a scheme-bearing argument can carry a
// credential; a commit message that happens to contain "@" must stay readable.
func TestRedactArgsLeavesNonURLArgumentsAlone(t *testing.T) {
	args := []string{"commit", "-m", "thanks @someone for the report", "--", "file.md"}
	got := redactArgs(args)
	for i := range args {
		if got[i] != args[i] {
			t.Errorf("arg %d = %q, want it untouched (%q)", i, got[i], args[i])
		}
	}

	withURL := []string{"clone", "--", "https://user:tok@example.com/r.git", "dst"}
	if redactArgs(withURL)[2] != "https://***@example.com/r.git" {
		t.Errorf("the URL argument was not redacted: %q", redactArgs(withURL)[2])
	}
}
