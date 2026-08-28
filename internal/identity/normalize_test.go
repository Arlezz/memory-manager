package identity

import (
	"errors"
	"strings"
	"testing"
)

// fakeToken stands in for the real GitLab PAT found in one of the configured
// remotes. Never put a live credential in a test file.
const fakeToken = "GITLAB-FAKE-TOKEN-abc123"

func TestNormalize(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		// Remotes taken from the machine survey, with the credential replaced.
		{"github https", "https://github.com/ACME-DEV/route_optimizer.git", "github.com/acme-dev/route_optimizer"},
		{"github https no suffix", "https://github.com/ORBIT-DEV/ORBIT-X_core", "github.com/orbit-dev/orbit-x_core"},
		{"github ssh scp form", "git@github.com:ORBIT-DEV/ORBIT-X_core.git", "github.com/orbit-dev/orbit-x_core"},
		{"github ssh url form", "ssh://git@github.com/ORBIT-DEV/ORBIT-X_core.git", "github.com/orbit-dev/orbit-x_core"},
		{"gitlab with credentials", "https://user:" + fakeToken + "@gitlab.example.com/prd/repositorios/internal/x/admin-frontend.git", "gitlab.example.com/prd/repositorios/internal/x/admin-frontend"},
		{"nested group path", "https://github.com/contoso/poc-control-room-3.git", "github.com/contoso/poc-control-room-3"},
		{"trailing slash", "https://github.com/Arlezz/codex-review.git/", "github.com/arlezz/codex-review"},
		{"uppercase host", "https://GitHub.com/Arlezz/vendor_experiments.git", "github.com/arlezz/vendor_experiments"},
		{"non default ssh port", "ssh://git@gitlab.example.com:2222/prd/x.git", "gitlab.example.com/prd/x"},
		{"git protocol", "git://github.com/ACME-DEV/atlas.git", "github.com/acme-dev/atlas"},
		{"whitespace padded", "  https://github.com/ACME-DEV/MVP-DEMO.git\n", "github.com/acme-dev/mvp-demo"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Normalize(tc.raw)
			if err != nil {
				t.Fatalf("Normalize(%q) returned error: %v", redact(tc.raw), err)
			}
			if got != tc.want {
				t.Errorf("Normalize() = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestNormalizeStripsCredentials is the security assertion: no part of an
// inline credential may survive into the identity or its slug, because both get
// written to disk and into log lines.
func TestNormalizeStripsCredentials(t *testing.T) {
	raws := []string{
		"https://antony:" + fakeToken + "@gitlab.example.com/prd/x/admin-frontend.git",
		"https://" + fakeToken + "@github.com/Arlezz/codex-review.git",
		"ssh://antony:" + fakeToken + "@gitlab.example.com:2222/prd/x.git",
		"antony:" + fakeToken + "@gitlab.example.com:prd/x.git",
	}

	for _, raw := range raws {
		canonical, err := Normalize(raw)
		if err != nil {
			t.Fatalf("Normalize(%q) returned error: %v", redact(raw), err)
		}
		slug := Slugify(canonical)
		for _, leaked := range []string{fakeToken, "antony", "@"} {
			if strings.Contains(canonical, leaked) {
				t.Errorf("canonical %q leaked %q", canonical, leaked)
			}
			if strings.Contains(slug, leaked) {
				t.Errorf("slug %q leaked %q", slug, leaked)
			}
		}
	}
}

// TestNormalizeSharedRemote pins the deliberate decision that two checkouts of
// the same repository share one identity, backup folders included.
func TestNormalizeSharedRemote(t *testing.T) {
	a, err := Normalize("https://github.com/ORBIT-DEV/ORBIT-X_core.git")
	if err != nil {
		t.Fatal(err)
	}
	// This is the remote of ORBIT-X_core_backup_20260728, reached over SSH.
	b, err := Normalize("git@github.com:ORBIT-DEV/ORBIT-X_core.git")
	if err != nil {
		t.Fatal(err)
	}
	if a != b {
		t.Errorf("same repo resolved to different identities: %q vs %q", a, b)
	}
}

// TestNormalizeOrgRenameDiverges documents a known limitation: a repo moved
// between orgs gets a new identity, which is why the override file exists.
func TestNormalizeOrgRenameDiverges(t *testing.T) {
	old, _ := Normalize("https://github.com/ACME-DEV/ORBIT-X_billing.git")
	new_, _ := Normalize("https://github.com/ORBIT-DEV/ORBIT-X_billing.git")
	if old == new_ {
		t.Fatal("expected an org rename to change identity; if this ever passes, the override file is no longer needed")
	}
}

func TestNormalizeRejectsLocal(t *testing.T) {
	raws := []string{
		`C:\Users\Anton\Documents\projects\ORBIT-X_core`,
		"C:/Users/Anton/Documents/projects/ORBIT-X_core",
		"/home/anton/repos/core",
		"../sibling-repo",
		"file:///c/repos/core",
		`\share\repos\core`,
	}
	for _, raw := range raws {
		if _, err := Normalize(raw); !errors.Is(err, ErrLocalRemote) {
			t.Errorf("Normalize(%q) error = %v, want ErrLocalRemote", raw, err)
		}
	}
}

func TestNormalizeRejectsEmptyAndPathless(t *testing.T) {
	for _, raw := range []string{"", "   ", "https://github.com", "https://github.com/"} {
		if _, err := Normalize(raw); err == nil {
			t.Errorf("Normalize(%q) succeeded, want error", raw)
		}
	}
}

func TestSlugify(t *testing.T) {
	tests := []struct{ in, want string }{
		{"github.com/orbit-dev/orbit-x_core", "github.com__orbit-dev__orbit-x_core"},
		{"gitlab.example.com/prd/repositorios/internal/x/admin-frontend", "gitlab.example.com__prd__repositorios__internal__x__admin-frontend"},
		{"GitHub.com/Arlezz/Codex Review", "github.com__arlezz__codex-review"},
		{"host//double///slash", "host__double__slash"},
		{`host\backslash\path`, "host__backslash__path"},
	}
	for _, tc := range tests {
		if got := Slugify(tc.in); got != tc.want {
			t.Errorf("Slugify(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestSlugifyIsStableAcrossCase covers the observed bug that Claude Code's own
// project directories differ only by the case of the drive letter.
func TestSlugifyIsStableAcrossCase(t *testing.T) {
	if Slugify("GitHub.com/ORBIT-DEV/Core") != Slugify("github.com/orbit-dev/core") {
		t.Error("slug is case sensitive; identity would split across machines")
	}
}
