package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Arlezz/memory-manager/internal/identity"
	"github.com/Arlezz/memory-manager/internal/sync"
)

func syncResult() sync.Result {
	return sync.Result{
		Identity:            identity.Identity{Canonical: "github.com/acme-dev/orbit-x"},
		MemoryDir:           "/home/dev/.claude/projects/orbit-x/memory",
		FromProject:         6,
		FromPersonalGlobal:  2,
		FromPersonalProject: 5,
	}
}

// TestSyncSummarySurvivesQuiet pins the reason -quiet exists at all. The hook
// runs with it, so if it suppressed the summary too, a successful sync would
// look exactly like a hook that never fired — the failure this tool is meant to
// prevent.
func TestSyncSummarySurvivesQuiet(t *testing.T) {
	var out bytes.Buffer
	printSyncSummary(&out, syncResult(), false, true)

	got := out.String()
	if !strings.Contains(got, "6 project, 2 personal/global, 5 personal/project") {
		t.Errorf("quiet sync did not report its counts:\n%s", got)
	}
	if strings.Contains(got, "/home/dev") {
		t.Errorf("quiet sync printed the memory directory detail:\n%s", got)
	}
	if lines := strings.Count(got, "\n"); lines != 1 {
		t.Errorf("quiet sync printed %d lines, want 1:\n%s", lines, got)
	}
}

func TestSyncSummaryPrintsDirWhenNotQuiet(t *testing.T) {
	var out bytes.Buffer
	printSyncSummary(&out, syncResult(), false, false)

	if !strings.Contains(out.String(), "/home/dev/.claude/projects/orbit-x/memory") {
		t.Errorf("interactive sync did not print the memory directory:\n%s", out.String())
	}
}

// TestSyncSummarySilentWhenDegraded covers the one case that must stay silent:
// a degraded run merged nothing, so its counts would read as a complete sync.
// Its warnings carry the message instead, and they go to stderr regardless.
func TestSyncSummarySilentWhenDegraded(t *testing.T) {
	res := syncResult()
	res.Degraded = true

	for _, quiet := range []bool{true, false} {
		var out bytes.Buffer
		printSyncSummary(&out, res, false, quiet)
		if out.Len() != 0 {
			t.Errorf("degraded sync (quiet=%v) printed a summary:\n%s", quiet, out.String())
		}
	}
}

// TestSyncSummaryReportsUnpushedPersonalMemory covers the failure that hides
// best: the write-back hook committed and was cancelled before its push, so
// every local check looks finished while the memory has reached no other
// machine. Session start is the only place the user will see it, and the hook
// runs quiet, so the line has to survive -quiet like the summary does.
func TestSyncSummaryReportsUnpushedPersonalMemory(t *testing.T) {
	res := syncResult()
	res.PersonalUnpushed = 1

	for _, quiet := range []bool{true, false} {
		var out bytes.Buffer
		printSyncSummary(&out, res, false, quiet)

		got := out.String()
		if !strings.Contains(got, "1 commit committed but not pushed") {
			t.Errorf("sync (quiet=%v) did not report the unpushed commit:\n%s", quiet, got)
		}
		if !strings.Contains(got, "memory-manager push") {
			t.Errorf("sync (quiet=%v) did not say how to fix it:\n%s", quiet, got)
		}
	}
}

// TestSyncSummarySilentWhenNothingUnpushed keeps the normal case to one line.
func TestSyncSummarySilentWhenNothingUnpushed(t *testing.T) {
	var out bytes.Buffer
	printSyncSummary(&out, syncResult(), false, true)

	if strings.Contains(out.String(), "not pushed") {
		t.Errorf("a fully pushed layer was reported as unpushed:\n%s", out.String())
	}
}

func TestSyncSummaryReportsRemovalsAndPreservedEdits(t *testing.T) {
	res := syncResult()
	res.Removed = 2
	res.Preserved = 1

	var out bytes.Buffer
	printSyncSummary(&out, res, true, true)

	got := out.String()
	for _, want := range []string{"[dry-run] ", "2 removed", "1 local edit(s) preserved"} {
		if !strings.Contains(got, want) {
			t.Errorf("summary missing %q:\n%s", want, got)
		}
	}
}

// TestRefuseOptionLikeRepo covers the config half of the argument injection.
// git reads any argv element beginning with "-" as an option, and --upload-pack
// names a command it runs, so an option-shaped personal_repo turned writing a
// config file into running a command. The clones terminate their options with
// "--", which is what closes the hole; this refuses to record the value at all,
// while the error can still name the field the user got wrong.
func TestRefuseOptionLikeRepo(t *testing.T) {
	refused := []string{
		"--upload-pack=touch PWNED.txt",
		"-u",
		"--config=core.sshCommand=touch x",
		// config.Load trims before use, so a leading space must not smuggle the
		// same value past this check.
		"  --upload-pack=touch PWNED.txt",
	}
	for _, repo := range refused {
		if err := refuseOptionLikeRepo(repo); err == nil {
			t.Errorf("refuseOptionLikeRepo(%q) = nil, want an error", repo)
		}
	}

	accepted := []string{
		"https://github.com/acme-dev/orbit-x-memory.git",
		"git@github.com:acme-dev/orbit-x-memory.git",
		"ssh://git@gitlab.example.com:2222/acme-dev/orbit-x-memory.git",
		"gitlab@gitlab.example.com:acme-dev/orbit-x-memory.git",
	}
	for _, repo := range accepted {
		if err := refuseOptionLikeRepo(repo); err != nil {
			t.Errorf("refuseOptionLikeRepo(%q) = %v, want nil", repo, err)
		}
	}
}

// TestRefuseUnusableRepo is the fail-closed half of A-10. The old guard threw
// away identity.Normalize's error and carried on, which is how the audit's bench
// accepted a bare local path as a personal repository.
func TestRefuseUnusableRepo(t *testing.T) {
	refused := []string{
		// The collateral finding: a local path cannot identify a repository on
		// another machine, which is the whole problem this tool exists to fix.
		"/home/anton/notes",
		"C:" + string(filepath.Separator) + "repos" + string(filepath.Separator) + "notes",
		"./relative/path",
		"file:///srv/git/memory.git",
		"",
		"   ",
		"not a url at all",
	}
	for _, repo := range refused {
		if err := refuseUnusableRepo(repo); err == nil {
			t.Errorf("refuseUnusableRepo(%q) = nil, want an error", repo)
		}
	}

	accepted := []string{
		"https://github.com/acme-dev/orbit-x-memory.git",
		"git@github.com:acme-dev/orbit-x-memory.git",
		"ssh://git@gitlab.example.com:2222/acme-dev/orbit-x-memory.git",
		"https://arlezz:github_pat_11ABCDEFG0aBcDeFgHiJkL@github.com/acme-dev/orbit-x-memory.git",
	}
	for _, repo := range accepted {
		if err := refuseUnusableRepo(repo); err != nil {
			t.Errorf("refuseUnusableRepo(%q) = %v, want nil", repo, err)
		}
	}
}

// TestRefuseUnusableRepoDoesNotLeakTheToken keeps the new error from undoing
// A-03: it quotes the value the user passed, and that value may be the one form
// that carries a credential.
func TestRefuseUnusableRepoDoesNotLeakTheToken(t *testing.T) {
	err := refuseUnusableRepo("file://arlezz:github_pat_11ABCDEFG0aBcDeFgHiJkL@localhost/x.git")
	if err == nil {
		t.Fatal("a file:// remote was accepted")
	}
	if strings.Contains(err.Error(), "github_pat_") {
		t.Errorf("the token is in the error message: %v", err)
	}
}
