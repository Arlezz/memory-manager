// Package sync merges the two memory layers into the directory Claude Code reads.
//
// This is the read half of the tool and the whole of the first deliverable:
// nothing here writes to a project repository. It pulls, merges, regenerates the
// index and records a manifest.
package sync

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/Arlezz/memory-manager/internal/claudedir"
	"github.com/Arlezz/memory-manager/internal/config"
	"github.com/Arlezz/memory-manager/internal/frontmatter"
	"github.com/Arlezz/memory-manager/internal/identity"
	"github.com/Arlezz/memory-manager/internal/index"
	"github.com/Arlezz/memory-manager/internal/layer"
	"github.com/Arlezz/memory-manager/internal/personal"
	"github.com/Arlezz/memory-manager/internal/state"
)

// Result summarizes one sync, for the single line printed at session start.
type Result struct {
	// Identity is the resolved project identity, zero when none was found.
	Identity identity.Identity
	// MemoryDir is where the merge was written.
	MemoryDir string
	// FromProject, FromPersonalGlobal and FromPersonalProject count merged files.
	FromProject         int
	FromPersonalGlobal  int
	FromPersonalProject int
	// Removed counts files dropped because they left both layers.
	Removed int
	// Preserved counts local edits that were not overwritten because they have
	// not been pushed back to their layer yet.
	Preserved int
	// Warnings are non-fatal problems worth showing the user. A silent failure
	// here means working for weeks against stale memory without knowing.
	Warnings []string
	// Degraded reports that the merge did not happen and local memory is in use.
	Degraded bool
}

// Options configures a sync.
type Options struct {
	// Dir is the project directory to sync. Defaults to the working directory.
	Dir string
	// DryRun reports what would change without touching the memory directory.
	DryRun bool
}

// candidate is one memory file competing for a slot in the merged directory.
type candidate struct {
	memory frontmatter.Memory
	layer  layer.Layer
	// label describes the source, for warnings and the manifest.
	label string
}

// Run performs the sync.
//
// An unresolvable identity is not an error: the session continues on whatever
// memory is already in the native directory, with a warning.
func Run(opts Options) (Result, error) {
	dir := opts.Dir
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return Result{}, err
		}
		dir = wd
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return Result{}, err
	}

	var res Result

	id, err := identity.Resolve(abs)
	if err != nil {
		res.Degraded = true
		res.Warnings = append(res.Warnings,
			fmt.Sprintf("%v; using local memory only. Run \"memory-manager init\" to pin an identity.", err))
		return res, nil
	}
	res.Identity = id

	target, err := claudedir.MemoryDir(abs)
	if err != nil {
		return res, err
	}
	res.MemoryDir = target

	// Project layer: already in the work tree, so it needs no network.
	projectRoot := id.Root
	if projectRoot == "" {
		projectRoot = abs
	}
	projectMemories, err := layer.Read(filepath.Join(projectRoot, filepath.FromSlash(layer.ProjectDir)))
	if err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("project layer unreadable: %v", err))
	}

	// Personal layer: needs the private clone, which may be unavailable.
	// personalAvailable gates deletion propagation below: an unreachable remote
	// must never be read as "these memories were deleted".
	var (
		globalMemories, projectScoped []frontmatter.Memory
		personalAvailable             bool
	)
	cfg, cfgErr := config.Load()
	if cfgErr != nil {
		res.Warnings = append(res.Warnings, cfgErr.Error())
	} else {
		repo, warn, err := personal.Open(cfg)
		if err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("personal layer: %v", err))
		}
		if warn != "" {
			res.Warnings = append(res.Warnings, warn)
		}
		if pullWarn, _ := repo.Pull(); pullWarn != "" {
			res.Warnings = append(res.Warnings, pullWarn)
		}
		if repo.Present {
			personalAvailable = true
			if globalMemories, err = layer.Read(layer.PersonalPath(repo.Path, "")); err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("personal global layer unreadable: %v", err))
			}
			if projectScoped, err = layer.Read(layer.PersonalPath(repo.Path, id.Slug)); err != nil {
				res.Warnings = append(res.Warnings, fmt.Sprintf("personal project layer unreadable: %v", err))
			}
		}
	}

	// Later sources win. Personal beats project because an explicit personal
	// preference outranks a team default, and project-scoped personal memory
	// beats global personal memory because it is the more specific statement.
	chosen := map[string]candidate{}
	add := func(list []frontmatter.Memory, lyr layer.Layer, label string) {
		for _, m := range list {
			if prev, ok := chosen[m.Base]; ok && prev.label != label {
				res.Warnings = append(res.Warnings,
					fmt.Sprintf("%s exists in both %s and %s; kept %s", m.Base, prev.label, label, label))
			}
			chosen[m.Base] = candidate{memory: m, layer: lyr, label: label}
		}
	}
	add(projectMemories, layer.Project, "project")
	add(globalMemories, layer.Personal, "personal/global")
	add(projectScoped, layer.Personal, "personal/"+id.Slug)

	// Format defects are reported, never a reason to drop a fact.
	for _, name := range sortedKeys(chosen) {
		for _, p := range chosen[name].memory.Problems {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %s", name, p))
		}
	}

	prev, manifestErr := state.Load(id.Slug)
	if manifestErr != nil {
		res.Warnings = append(res.Warnings, manifestErr.Error())
	}
	if fresh := prev.ForPersonalRepo(cfg.PersonalRepo); len(fresh.Entries) != len(prev.Entries) {
		res.Warnings = append(res.Warnings,
			"the personal repository changed since the last sync; the manifest was discarded and every memory is treated as new")
		prev = fresh
	}

	if opts.DryRun {
		res.tally(chosen)
		res.Removed = countRemovals(prev, chosen, personalAvailable)
		return res, nil
	}

	if err := os.MkdirAll(target, 0o755); err != nil {
		return res, err
	}

	next := state.Manifest{
		Version:      state.Version,
		Slug:         id.Slug,
		Canonical:    id.Canonical,
		MemoryDir:    target,
		PersonalRepo: cfg.PersonalRepo,
		Entries:      map[string]state.Entry{},
	}

	// indexed holds the memory that will describe each name in the generated
	// index. It normally comes from the layer, but a preserved local edit must
	// describe itself or the index would contradict the file next to it.
	indexed := map[string]frontmatter.Memory{}

	for _, name := range sortedKeys(chosen) {
		c := chosen[name]
		indexed[name] = c.memory

		content, err := os.ReadFile(c.memory.Path)
		if err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		dst := filepath.Join(target, name)
		layerDigest := state.Digest(content)

		// Never overwrite a local edit that has not been pushed back yet. Claude
		// writes memory straight into this directory during a session, so an
		// unconditional copy here loses whatever it wrote since the last push.
		if onDisk, readErr := os.ReadFile(dst); readErr == nil {
			diskDigest := state.Digest(onDisk)
			tracked, wasTracked := prev.Entries[name]

			switch {
			case diskDigest == layerDigest:
				// Already identical. Record it and skip the write.
				next.Entries[name] = state.Entry{
					Layer:  string(c.layer),
					Origin: c.memory.Path,
					SHA256: layerDigest,
				}
				continue

			case wasTracked && diskDigest != tracked.SHA256:
				// Edited locally since the last sync. Keep the local file and keep
				// the old baseline, so "push" still sees it as an edit.
				res.Preserved++
				next.Entries[name] = tracked
				if local, err := frontmatter.Parse(dst); err == nil {
					indexed[name] = local
				}
				detail := "kept the local version"
				if tracked.SHA256 != layerDigest {
					detail = "both sides changed; kept the local version"
				}
				res.Warnings = append(res.Warnings, fmt.Sprintf(
					"%s has unpushed local changes: %s. Run \"memory-manager push\" to send it to its layer.", name, detail))
				continue

			case !wasTracked:
				// A local file this tool never wrote, colliding with a layer file.
				// It cannot be reconciled automatically, so the local one stands.
				res.Preserved++
				if local, err := frontmatter.Parse(dst); err == nil {
					indexed[name] = local
				}
				res.Warnings = append(res.Warnings, fmt.Sprintf(
					"%s exists locally and in the %s layer but was never synced; kept the local version", name, c.label))
				continue
			}
		}

		if err := os.WriteFile(dst, content, 0o644); err != nil {
			res.Warnings = append(res.Warnings, fmt.Sprintf("%s: %v", name, err))
			continue
		}
		next.Entries[name] = state.Entry{
			Layer:  string(c.layer),
			Origin: c.memory.Path,
			SHA256: layerDigest,
		}
	}

	// Propagate deletions, but only for files this tool put there. Anything not
	// in the previous manifest is untracked local memory and is left alone.
	//
	// A file is only deleted when its own layer was readable this run. An
	// unreachable personal remote otherwise looks exactly like an upstream
	// deletion, and the tool would strip memory because the network blinked.
	var retained int
	for _, name := range prev.Names() {
		if _, still := chosen[name]; still {
			continue
		}
		entry := prev.Entries[name]
		if entry.Layer == string(layer.Personal) && !personalAvailable {
			// Keep the file and keep tracking it, so it is still a managed file
			// once the layer comes back.
			next.Entries[name] = entry
			retained++
			continue
		}
		if err := os.Remove(filepath.Join(target, name)); err == nil {
			res.Removed++
		}
	}
	if retained > 0 {
		res.Warnings = append(res.Warnings,
			fmt.Sprintf("kept %d personal memory file(s) that could not be refreshed; they are not deleted while the personal layer is unavailable", retained))
	}

	merged := make([]frontmatter.Memory, 0, len(indexed))
	for _, name := range sortedKeys(chosen) {
		merged = append(merged, indexed[name])
	}
	// Untracked local memories still belong in the index, or Claude Code loads
	// an index that contradicts the directory beside it.
	untracked, err := untrackedMemories(target, chosen)
	if err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("scan of %s: %v", target, err))
	}
	merged = append(merged, untracked...)

	if err := index.Write(target, merged); err != nil {
		return res, err
	}
	if err := state.Save(next); err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("manifest not saved: %v", err))
	}

	res.tally(chosen)
	return res, nil
}

// tally fills the per-source counters.
func (r *Result) tally(chosen map[string]candidate) {
	for _, c := range chosen {
		switch c.label {
		case "project":
			r.FromProject++
		case "personal/global":
			r.FromPersonalGlobal++
		default:
			r.FromPersonalProject++
		}
	}
}

// countRemovals reports how many tracked files would be dropped, applying the
// same rule as the real run: a personal memory is never dropped while its layer
// is unavailable.
func countRemovals(prev state.Manifest, chosen map[string]candidate, personalAvailable bool) int {
	n := 0
	for _, name := range prev.Names() {
		if _, still := chosen[name]; still {
			continue
		}
		if prev.Entries[name].Layer == string(layer.Personal) && !personalAvailable {
			continue
		}
		n++
	}
	return n
}

// untrackedMemories returns memories present in the target directory that no
// layer provided, so they can still be indexed.
func untrackedMemories(target string, chosen map[string]candidate) ([]frontmatter.Memory, error) {
	all, err := layer.Read(target)
	if err != nil {
		return nil, err
	}
	var out []frontmatter.Memory
	for _, m := range all {
		if _, managed := chosen[m.Base]; managed {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

func sortedKeys(m map[string]candidate) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
