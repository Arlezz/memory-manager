package secrets

import (
	"strings"
	"testing"
)

// Synthetic values only. A test file is committed, so a real credential here
// would be the exact leak this package exists to prevent.
//
// The prefixes are split across a concatenation on purpose: a scanner reading
// the file sees no token shape, while the compiler hands the detector under
// test the whole value. Keep any new fixture in the same form.
const (
	fakeGitLabPAT = "glpat-" + "FAKEfake1234567890ab"
	fakeGitLabTok = "GITLAB-FAKEfake1234567890"
	fakeGitHubPAT = "ghp_" + "FAKEfake1234567890abcdefghijklmn"
)

func TestScanFindsCredentials(t *testing.T) {
	tests := []struct {
		name string
		text string
		want string
	}{
		{
			"credential in URL",
			"remote is https://antony:" + fakeGitLabTok + "@gitlab.example.com/prd/x.git",
			"credential in URL",
		},
		{"gitlab pat", "token: " + fakeGitLabPAT, "GitLab personal access token"},
		{"github token", "use " + fakeGitHubPAT + " to push", "GitHub token"},
		{"aws key", "AKIAIOSFODNN7EXAMPLE is the key id", "AWS access key id"},
		{"private key", "-----BEGIN OPENSSH PRIVATE KEY-----", "private key block"},
		{"assigned secret", `password = "hunter2hunter2"`, "assigned secret"},
		{"api key assignment", "api_key: abcdef1234567890", "assigned secret"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			found := Scan(tc.text)
			if len(found) == 0 {
				t.Fatalf("Scan(%q) found nothing, want %q", tc.text, tc.want)
			}
			var rules []string
			for _, f := range found {
				rules = append(rules, f.Rule)
			}
			if !contains(rules, tc.want) {
				t.Errorf("rules = %v, want one to be %q", rules, tc.want)
			}
		})
	}
}

// TestScanNeverEchoesTheSecret is the property that makes the report safe to
// paste into a ticket or a chat.
func TestScanNeverEchoesTheSecret(t *testing.T) {
	text := "token: " + fakeGitLabPAT
	for _, f := range Scan(text) {
		if strings.Contains(f.Excerpt, fakeGitLabPAT) {
			t.Errorf("finding echoed the full secret: %q", f.Excerpt)
		}
		if !strings.Contains(f.Excerpt, "*") {
			t.Errorf("finding %q is not masked", f.Excerpt)
		}
	}
}

// TestScanQuietOnRealMemoryText guards against the scan becoming noise. These
// lines are taken from the memory files already on disk; a false positive here
// trains the user to ignore the report.
func TestScanQuietOnRealMemoryText(t *testing.T) {
	clean := []string{
		"Claude Code stores project memory at ~/.claude/projects/<mangled-absolute-path>/memory/",
		"See [[memory-manager-scope-decisions]] and [[memory-manager-two-layer-design]].",
		"`C:\\Users\\Anton\\Documents\\projects\\memory-manager` is a greenfield repo",
		"identity slug is github.com__orbit-dev__orbit-x_core and it stays stable",
		"- **Transport: resolved on 2026-08-27** — git for both layers, no blob storage",
		"commit 4f0a1c2d9b8e7a6f5c4d3b2a1908f7e6d5c4b3a2 fixed the merge",
		"description: memory-manager scope locked to team-shared memory, Claude Code only",
	}
	for _, line := range clean {
		if found := Scan(line); len(found) > 0 {
			t.Errorf("false positive on %q: %v", line, found)
		}
	}
}

// TestScanQuietOnObservedFalsePositives locks in the fixes for every string the
// entropy rule wrongly flagged on the first real run over the existing memory
// corpus. These are branch names, camelCase identifiers and doc paths.
func TestScanQuietOnObservedFalsePositives(t *testing.T) {
	clean := []string{
		"branch is fix/source-item-height-and-size",
		"12/source-item-height-branches-wip-frontend-",
		"see fix/add-source-race-condition-handler",
		"migration_changelog_backend_1_2_0_catalog",
		"chore/bump-datatable-package-version-table",
		"examples/nova-playground/src/app/frontend",
		"useCallbackWithDependenciesAndMoreOnes",
		"NotificationLabelsIconStyleAndMoreLabels",
		"docs/notifications-migration-2026-07-27",
		"reactQueryGatingArchitectureRewriter",
		"fastify_task_mise_run_test_details_fails",
		"project_client_migration_backend_1_2_0",
		"github.com/ORBIT-DEV/ORBIT-X_core/pull/comments",
		// An Alembic revision filename: a hex revision id followed by words.
		"migrations/versions/f7b2c0a1d3e4_add_client_catalog_table.py",
		"revision f7b2c0a1d3e4_add_client_catalog_table is the head",
	}
	for _, line := range clean {
		if found := Scan(line); len(found) > 0 {
			t.Errorf("false positive on %q: %v", line, found)
		}
	}
}

// TestScanStillCatchesOpaqueTokens is the other half of the tuning: tightening
// the entropy rule must not blind it to a token with no known prefix.
func TestScanStillCatchesOpaqueTokens(t *testing.T) {
	opaque := []string{
		"token: 7Kq2xZ9vB4nM1pL8sT3wY6rE5uI0oA7dF2gH4jK9",
		"bearer c3RhY2tvZmZha2VieXRlczEyMzQ1Njc4OTBhYmNk",
		// The shape actually found in the corpus: an env var whose name ends in
		// TOKEN, assigned an opaque value.
		"export UV_INDEX_PRIVATE_TOKEN=7Kq2xZ9vB4nM1pL8sT3wY6rE5uI0oA",
	}
	for _, line := range opaque {
		if found := Scan(line); len(found) == 0 {
			t.Errorf("missed an opaque token in %q", line)
		}
	}
}

// TestScanReportsEachSecretOnce checks the entropy heuristic stays quiet where a
// named rule already fired: the same token reported twice reads as a broken tool.
func TestScanReportsEachSecretOnce(t *testing.T) {
	found := Scan("Run: export GITLAB_TOKEN=" + fakeGitLabPAT)
	if len(found) != 1 {
		t.Errorf("Scan reported %d findings for one token: %v", len(found), found)
	}
	if len(found) > 0 && found[0].Rule == "high-entropy string" {
		t.Errorf("the named rule lost to the heuristic: %v", found[0])
	}
}

func TestMask(t *testing.T) {
	if got := Mask("abcd"); got != "****" {
		t.Errorf("Mask(short) = %q", got)
	}
	got := Mask("abcdefghijklmnop")
	if strings.Contains(got, "efghijkl") {
		t.Errorf("Mask left the middle visible: %q", got)
	}
	if !strings.HasPrefix(got, "abcd") || !strings.HasSuffix(got, "mnop") {
		t.Errorf("Mask lost the identifying edges: %q", got)
	}
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
