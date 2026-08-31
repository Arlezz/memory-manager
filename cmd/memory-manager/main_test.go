package main

import (
	"bytes"
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
