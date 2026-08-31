// Package writeback sends memory written during a session back to its layer.
//
// Claude Code writes memory into its own path-keyed directory. Without this
// package the tool only reads: everything written during a session stays on one
// machine, which is the problem memory-manager exists to solve.
//
// The manifest from internal/state is what makes the diff possible. It records
// the layer, origin path and content hash of every file the last sync wrote, so
// a file on disk can be classified as new, edited, deleted, or moved between
// layers.
package writeback

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/Arlezz/memory-manager/internal/claudedir"
	"github.com/Arlezz/memory-manager/internal/config"
	"github.com/Arlezz/memory-manager/internal/frontmatter"
	"github.com/Arlezz/memory-manager/internal/identity"
	"github.com/Arlezz/memory-manager/internal/layer"
	"github.com/Arlezz/memory-manager/internal/personal"
	"github.com/Arlezz/memory-manager/internal/secrets"
	"github.com/Arlezz/memory-manager/internal/state"
)

// Change is what happened to a memory since the last sync.
type Change string

const (
	// Added is a memory that no layer has yet.
	Added Change = "added"
	// Updated is an edit to a memory that came from a layer.
	Updated Change = "updated"
	// Removed is a memory deleted from the native directory.
	Removed Change = "removed"
	// Moved is a memory whose type or scope now routes it to the other layer.
	Moved Change = "moved"
)

// Action is one planned write.
type Action struct {
	// Base is the memory file name.
	Base string
	// Change is what happened to it.
	Change Change
	// Layer is the destination layer. Meaningless for Removed.
	Layer layer.Layer
	// FromLayer is the previous layer, set only for Moved.
	FromLayer layer.Layer
	// Source is the file in the native directory, empty for Removed.
	Source string
	// Dest is where the content goes, empty for Removed and for blocked actions.
	Dest string
	// DeleteFrom is a layer path to delete, set for Removed and Moved.
	DeleteFrom string
	// Type is the declared memory type.
	Type string
	// Blocked explains why this action will not be applied, when set.
	Blocked string
	// Problems are frontmatter defects that degrade the index.
	Problems []string
	// Secrets are suspected credentials. Any finding blocks the action.
	Secrets []secrets.Finding
}

// Plan is everything that needs to go back to a layer.
type Plan struct {
	// Identity is the resolved project identity.
	Identity identity.Identity
	// MemoryDir is the native directory the plan was built from.
	MemoryDir string
	// PersonalRoot is the personal clone, empty when that layer is unavailable.
	PersonalRoot string
	// ProjectRoot is the work tree holding the project layer, empty without one.
	ProjectRoot string
	// Actions is the work to do, sorted by file name.
	Actions []Action
	// PersonalUnpushed counts commits already in the personal clone that the
	// remote has not seen. They are not actions — there is nothing left to
	// write — but they are memory that has not travelled yet.
	PersonalUnpushed int
	// Warnings are plan-level problems worth showing the user.
	Warnings []string
}

// Empty reports whether there is no file to write back.
func (p Plan) Empty() bool { return len(p.Actions) == 0 }

// Settled reports whether nothing at all is waiting: no file to write back and
// no commit stranded in the personal clone.
//
// Empty is not enough on its own. A run that committed and then died before the
// push leaves an empty plan behind, and treating that as finished is how memory
// silently stops reaching the other machine.
func (p Plan) Settled() bool { return p.Empty() && p.PersonalUnpushed == 0 }

// Counts returns how many actions of each change kind the plan holds.
func (p Plan) Counts() map[Change]int {
	out := map[Change]int{}
	for _, a := range p.Actions {
		out[a.Change]++
	}
	return out
}

// Build diffs the native memory directory against the manifest.
//
// It reads and classifies only; nothing is written.
func Build(dir string) (Plan, error) {
	var plan Plan

	abs, err := filepath.Abs(dir)
	if err != nil {
		return plan, err
	}

	id, err := identity.Resolve(abs)
	if err != nil {
		return plan, fmt.Errorf("%w; run \"memory-manager init\" to pin an identity", err)
	}
	plan.Identity = id
	plan.ProjectRoot = id.Root

	target, err := claudedir.MemoryDir(abs)
	if err != nil {
		return plan, err
	}
	plan.MemoryDir = target

	manifest, err := state.Load(id.Slug)
	if err != nil {
		// A rebuilt manifest makes every file look new. Say so loudly: applying
		// that plan would duplicate memory into the wrong layers.
		plan.Warnings = append(plan.Warnings, err.Error()+
			"; every file will look new until the next sync rebuilds it")
	}

	if cfg, cfgErr := config.Load(); cfgErr != nil {
		plan.Warnings = append(plan.Warnings, cfgErr.Error())
	} else {
		if fresh := manifest.ForPersonalRepo(cfg.PersonalRepo); len(fresh.Entries) != len(manifest.Entries) {
			plan.Warnings = append(plan.Warnings,
				"the personal repository changed since the last sync; every memory is treated as new so nothing is left behind in the old clone")
			manifest = fresh
		}
		repo, warn, err := personal.Open(cfg)
		if err != nil {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("personal layer: %v", err))
		}
		if warn != "" {
			plan.Warnings = append(plan.Warnings, warn)
		}
		if repo.Present {
			plan.PersonalRoot = repo.Path
			if n, aheadErr := repo.Unpushed(); aheadErr != nil {
				plan.Warnings = append(plan.Warnings,
					fmt.Sprintf("personal layer: cannot tell whether it is pushed: %v", aheadErr))
			} else {
				plan.PersonalUnpushed = n
			}
		}
	}

	onDisk, err := layer.Read(target)
	if err != nil {
		return plan, fmt.Errorf("read %s: %w", target, err)
	}

	seen := map[string]bool{}
	for _, m := range onDisk {
		seen[m.Base] = true
		if a, ok := classify(m, manifest, id, plan.PersonalRoot, plan.ProjectRoot); ok {
			plan.Actions = append(plan.Actions, a)
		}
	}

	plan.Actions = append(plan.Actions, deletions(manifest, seen, &plan)...)

	sort.Slice(plan.Actions, func(i, j int) bool { return plan.Actions[i].Base < plan.Actions[j].Base })
	return plan, nil
}

// classify decides what happened to one on-disk memory. The bool is false when
// nothing changed and there is no work to do.
func classify(m frontmatter.Memory, manifest state.Manifest, id identity.Identity, personalRoot, projectRoot string) (Action, bool) {
	content, err := os.ReadFile(m.Path)
	if err != nil {
		return Action{Base: m.Base, Blocked: err.Error()}, true
	}
	digest := state.Digest(content)

	dest := layer.For(m)
	entry, tracked := manifest.Entries[m.Base]

	a := Action{
		Base:     m.Base,
		Layer:    dest,
		Source:   m.Path,
		Type:     m.Type,
		Problems: m.Problems,
		Secrets:  secrets.Scan(string(content)),
	}

	switch {
	case !tracked:
		a.Change = Added
	case digest == entry.SHA256:
		// Byte-identical to what the layer holds, so nothing to send.
		return Action{}, false
	case string(dest) != entry.Layer:
		// The type or scope changed, so the memory belongs to the other layer now.
		// Without the delete it would exist in both.
		a.Change = Moved
		a.FromLayer = layer.Layer(entry.Layer)
		a.DeleteFrom = entry.Origin
	default:
		a.Change = Updated
		// Write back exactly where it came from, which preserves whether it was
		// global or project-scoped inside the personal repo.
		a.Dest = entry.Origin
	}

	if a.Dest == "" {
		a.Dest = destination(m, dest, id, personalRoot, projectRoot)
	}
	if a.Dest == "" {
		a.Blocked = blockReason(dest)
	}
	if len(a.Secrets) > 0 {
		// The project layer is committed to a shared repo and the personal repo is
		// pushed automatically, so neither is a safe place for a credential. This
		// runs unattended from a hook, where nobody is reading the output.
		a.Blocked = "suspected credential in the file; not written to any layer"
	}
	return a, true
}

// deletions turns manifest entries with no file on disk into Removed actions.
func deletions(manifest state.Manifest, seen map[string]bool, plan *Plan) []Action {
	var missing []string
	for _, name := range manifest.Names() {
		if !seen[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) == 0 {
		return nil
	}

	// Every tracked file gone at once is far more likely to be a wiped directory
	// than a deliberate purge, and propagating it would delete the layers too.
	if len(missing) == len(manifest.Entries) && len(missing) > 1 {
		plan.Warnings = append(plan.Warnings, fmt.Sprintf(
			"all %d tracked memories are missing from %s; refusing to propagate that as a deletion. Delete them from the layer by hand if it was intentional.",
			len(missing), plan.MemoryDir))
		return nil
	}

	out := make([]Action, 0, len(missing))
	for _, name := range missing {
		entry := manifest.Entries[name]
		out = append(out, Action{
			Base:       name,
			Change:     Removed,
			Layer:      layer.Layer(entry.Layer),
			DeleteFrom: entry.Origin,
		})
	}
	return out
}

// destination returns the layer path a memory should be written to.
func destination(m frontmatter.Memory, dest layer.Layer, id identity.Identity, personalRoot, projectRoot string) string {
	switch dest {
	case layer.Project:
		if projectRoot == "" {
			return ""
		}
		return filepath.Join(projectRoot, filepath.FromSlash(layer.ProjectDir), m.Base)
	default:
		if personalRoot == "" {
			return ""
		}
		return filepath.Join(layer.PersonalPath(personalRoot, layer.PersonalScope(m, id.Slug)), m.Base)
	}
}

func blockReason(dest layer.Layer) string {
	if dest == layer.Project {
		return "project memory needs a git work tree and none was found"
	}
	return "personal layer is not available; run \"memory-manager config -personal-repo <url>\""
}

// Result reports what Apply did.
type Result struct {
	// PersonalWritten and PersonalRemoved count changes to the personal clone.
	PersonalWritten int
	PersonalRemoved int
	// ProjectWritten and ProjectRemoved count changes to the work tree.
	ProjectWritten int
	ProjectRemoved int
	// Blocked counts actions that were skipped.
	Blocked int
	// ProjectFiles are the work-tree paths that changed. They are deliberately
	// left uncommitted, so the user needs the list.
	ProjectFiles []string
	// Committed and Pushed report what happened in the personal repo.
	Committed bool
	Pushed    bool
	// Warnings are non-fatal problems.
	Warnings []string
}

// Options configures Apply.
type Options struct {
	// DryRun classifies and reports without writing.
	DryRun bool
	// NoPush writes and commits the personal layer but does not push it.
	NoPush bool
	// AllowSecrets applies actions that were blocked for a suspected credential.
	AllowSecrets bool
}

// Apply executes the plan.
//
// The project layer is written into the work tree and never committed: those
// files live inside the user's repository, so an automatic commit would land on
// whatever branch they are on and leak into their pull request. Committing them
// by hand also means project memory passes code review like code does.
func Apply(plan Plan, opts Options) (Result, error) {
	var res Result

	if opts.DryRun {
		for _, a := range plan.Actions {
			if a.blocked(opts) {
				res.Blocked++
				continue
			}
			res.tally(a)
		}
		return res, nil
	}

	manifest, _ := state.Load(plan.Identity.Slug)
	if cfg, cfgErr := config.Load(); cfgErr == nil {
		manifest = manifest.ForPersonalRepo(cfg.PersonalRepo)
		// Record which repository these entries describe, so a later repoint
		// invalidates them instead of silently reporting nothing to push.
		manifest.PersonalRepo = cfg.PersonalRepo
	}
	if manifest.Entries == nil {
		manifest.Entries = map[string]state.Entry{}
	}

	var personalPaths []string
	for _, a := range plan.Actions {
		if a.blocked(opts) {
			res.Blocked++
			continue
		}

		if err := a.execute(); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %v", a.Base, err))
			continue
		}

		res.tally(a)
		if a.Layer == layer.Personal || a.FromLayer == layer.Personal {
			personalPaths = append(personalPaths, a.personalRelPaths(plan.PersonalRoot)...)
		}
		if a.Layer == layer.Project && a.Change != Removed {
			res.ProjectFiles = append(res.ProjectFiles, a.Dest)
		}
		a.record(&manifest, plan)
	}

	if err := state.Save(manifest); err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("manifest not saved: %v", err))
	}

	switch {
	case len(personalPaths) > 0:
		if err := commitAndPush(plan, personalPaths, opts, &res); err != nil {
			return res, err
		}
	case plan.PersonalUnpushed > 0 && !opts.NoPush:
		// Nothing to write, but an earlier run committed and then lost the
		// network step. Finishing it here is what makes the failure survivable:
		// otherwise the plan stays empty forever and the commit never leaves.
		if err := pushOnly(&res); err != nil {
			return res, err
		}
	}
	return res, nil
}

// pushOnly publishes commits an earlier run left behind, without writing or
// committing anything first.
func pushOnly(res *Result) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	repo, _, err := personal.Open(cfg)
	if err != nil {
		return err
	}
	if !repo.Present {
		return errors.New("personal layer is not available; nothing was pushed")
	}
	if err := repo.Push(); err != nil {
		return err
	}
	res.Pushed = true
	return nil
}

// commitAndPush finishes the personal layer. A failure here is returned rather
// than warned about: the files are on disk but not published, and silently
// leaving them behind is how a user ends up weeks out of sync.
func commitAndPush(plan Plan, paths []string, opts Options, res *Result) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	repo, _, err := personal.Open(cfg)
	if err != nil {
		return err
	}
	if !repo.Present {
		return errors.New("personal layer is not available; nothing was committed")
	}

	message := commitMessage(plan, *res)
	if err := repo.Commit(paths, message); err != nil {
		if errors.Is(err, personal.ErrNothingToCommit) {
			return nil
		}
		return err
	}
	res.Committed = true

	if opts.NoPush {
		res.Warnings = append(res.Warnings, "committed but not pushed (-no-push)")
		return nil
	}
	if err := repo.Push(); err != nil {
		return err
	}
	res.Pushed = true
	return nil
}

// commitMessage summarizes the change set in the subject and lists the files in
// the body, so the history of the personal repo is readable a month later.
func commitMessage(plan Plan, counts Result) string {
	var parts []string
	for _, c := range []struct {
		n     int
		label string
	}{
		{counts.PersonalWritten, "written"},
		{counts.PersonalRemoved, "removed"},
	} {
		if c.n > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", c.n, c.label))
		}
	}
	subject := fmt.Sprintf("memory: %s (%s)", strings.Join(parts, ", "), plan.Identity.Canonical)

	var body strings.Builder
	for _, a := range plan.Actions {
		if a.Layer != layer.Personal && a.FromLayer != layer.Personal {
			continue
		}
		fmt.Fprintf(&body, "\n%s %s", a.Change, a.Base)
	}
	return subject + "\n" + body.String() + "\n"
}

// blocked reports whether an action should be skipped.
//
// Only the credential block is overridable, and only when a destination exists:
// a missing layer is not something a flag can fix.
func (a Action) blocked(opts Options) bool {
	if a.Blocked == "" {
		return false
	}
	if opts.AllowSecrets && len(a.Secrets) > 0 && a.Dest != "" {
		return false
	}
	return true
}

// execute performs one action's file operations.
func (a Action) execute() error {
	if a.DeleteFrom != "" {
		if err := os.Remove(a.DeleteFrom); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	if a.Change == Removed || a.Dest == "" {
		return nil
	}
	content, err := os.ReadFile(a.Source)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.Dest), 0o755); err != nil {
		return err
	}
	return os.WriteFile(a.Dest, content, 0o644)
}

// record updates the manifest so the next sync and the next push agree with what
// is now on disk.
func (a Action) record(manifest *state.Manifest, plan Plan) {
	if a.Change == Removed {
		delete(manifest.Entries, a.Base)
		return
	}
	content, err := os.ReadFile(a.Dest)
	if err != nil {
		return
	}
	manifest.Entries[a.Base] = state.Entry{
		Layer:  string(a.Layer),
		Origin: a.Dest,
		SHA256: state.Digest(content),
	}
	manifest.Slug = plan.Identity.Slug
	manifest.Canonical = plan.Identity.Canonical
	manifest.MemoryDir = plan.MemoryDir
}

// personalRelPaths returns the repo-relative paths this action touched, which is
// what git needs to stage exactly those files and nothing else.
func (a Action) personalRelPaths(root string) []string {
	var out []string
	for _, p := range []string{a.Dest, a.DeleteFrom} {
		if p == "" {
			continue
		}
		rel, err := filepath.Rel(root, p)
		if err != nil || strings.HasPrefix(rel, "..") {
			continue
		}
		out = append(out, filepath.ToSlash(rel))
	}
	return out
}

func (r *Result) tally(a Action) {
	switch {
	case a.Change == Removed && a.Layer == layer.Personal:
		r.PersonalRemoved++
	case a.Change == Removed:
		r.ProjectRemoved++
	case a.Layer == layer.Personal:
		r.PersonalWritten++
		if a.FromLayer == layer.Project {
			r.ProjectRemoved++
		}
	default:
		r.ProjectWritten++
		if a.FromLayer == layer.Personal {
			r.PersonalRemoved++
		}
	}
}
