// Command memory-manager syncs Claude Code memory across machines.
//
// Claude Code keys project memory by absolute filesystem path, so the same
// repository checked out at a different path resolves to a different memory
// directory and memory does not travel. This tool keys memory by the normalized
// git remote instead, and merges two layers — project memory committed in the
// repo, personal memory in a private repo — into the directory Claude Code reads.
//
// This build is the read-only half: it never writes to a project repository
// outside an explicit "migrate --apply".
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/Arlezz/memory-manager/internal/claudedir"
	"github.com/Arlezz/memory-manager/internal/config"
	"github.com/Arlezz/memory-manager/internal/identity"
	"github.com/Arlezz/memory-manager/internal/migrate"
	"github.com/Arlezz/memory-manager/internal/sync"
	"github.com/Arlezz/memory-manager/internal/writeback"
)

// version is stamped at build time via -ldflags.
var version = "dev"

const usage = `memory-manager — cross-machine memory for Claude Code

Usage:
  memory-manager identity [dir]        Show the resolved project identity
  memory-manager init [dir]            Pin an identity in .claude/memory-id
  memory-manager config                Show or set configuration
  memory-manager migrate               Plan the migration of path-keyed memory
  memory-manager sync [dir]            Merge both layers into Claude Code's memory dir
  memory-manager status [dir]          Show memory waiting to go back to its layer
  memory-manager push [dir]            Send changed memory back; commits and pushes the personal layer
  memory-manager version

Run a command with -h for its flags.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}

	var err error
	switch os.Args[1] {
	case "identity":
		err = cmdIdentity(os.Args[2:])
	case "init":
		err = cmdInit(os.Args[2:])
	case "config":
		err = cmdConfig(os.Args[2:])
	case "migrate":
		err = cmdMigrate(os.Args[2:])
	case "sync":
		err = cmdSync(os.Args[2:])
	case "status":
		err = cmdStatus(os.Args[2:])
	case "push":
		err = cmdPush(os.Args[2:])
	case "version", "--version", "-v":
		fmt.Println("memory-manager", version)
	case "help", "-h", "--help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n%s", os.Args[1], usage)
		os.Exit(2)
	}

	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

// argDir returns the directory argument or the working directory.
func argDir(args []string) (string, error) {
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		return filepath.Abs(args[0])
	}
	return os.Getwd()
}

func cmdIdentity(args []string) error {
	dir, err := argDir(args)
	if err != nil {
		return err
	}

	id, err := identity.Resolve(dir)
	if err != nil {
		// Not an error to the caller: 40% of the repos here have no remote, and
		// the useful output is the explanation plus the fix.
		fmt.Printf("dir:       %s\nidentity:  (none)\nreason:    %v\nfix:       memory-manager init %s\n", dir, err, dir)
		return nil
	}

	memDir, err := claudedir.MemoryDir(dir)
	if err != nil {
		return err
	}
	fmt.Printf("dir:       %s\nrepo root: %s\nidentity:  %s\nslug:      %s\nsource:    %s\nmemory:    %s\n",
		dir, orNone(id.Root), id.Canonical, id.Slug, id.Source, memDir)
	return nil
}

func cmdInit(args []string) error {
	fs := flag.NewFlagSet("init", flag.ExitOnError)
	slug := fs.String("slug", "", "identity to pin; defaults to the resolved remote or the folder name")
	force := fs.Bool("force", false, "overwrite an existing "+identity.OverrideFile)
	var rest []string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		rest = args[1:]
	} else {
		rest = args
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}

	dir, err := argDir(args)
	if err != nil {
		return err
	}

	want := strings.TrimSpace(*slug)
	if want == "" {
		if id, err := identity.Resolve(dir); err == nil {
			want = id.Canonical
		} else {
			// No remote to derive from. A folder-name identity is explicitly not
			// portable, so it is labelled as local to make that visible.
			want = "local/" + strings.ToLower(filepath.Base(dir))
			fmt.Fprintf(os.Stderr, "warning: no git remote; pinning %q, which does not identify this project on another machine\n", want)
		}
	}

	path := filepath.Join(dir, filepath.FromSlash(identity.OverrideFile))
	if _, err := os.Stat(path); err == nil && !*force {
		return fmt.Errorf("%s already exists; pass -force to replace it", path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body := fmt.Sprintf("# Project identity for memory-manager. Keep this value stable:\n"+
		"# changing it points the project at a different memory store.\n%s\n", identity.Slugify(want))
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		return err
	}
	fmt.Printf("wrote %s\nidentity: %s\n", path, identity.Slugify(want))
	return nil
}

func cmdConfig(args []string) error {
	fs := flag.NewFlagSet("config", flag.ExitOnError)
	repo := fs.String("personal-repo", "", "git URL of the private personal memory repository")
	branch := fs.String("personal-branch", "", "branch to track in the personal repository")
	if err := fs.Parse(args); err != nil {
		return err
	}

	path, err := config.File()
	if err != nil {
		return err
	}

	if *repo == "" && *branch == "" {
		cfg, err := config.Load()
		if err != nil {
			fmt.Printf("config:          %s (absent)\npersonal repo:   (unset)\n", path)
			return nil
		}
		clone, _ := config.PersonalClonePath()
		fmt.Printf("config:          %s\npersonal repo:   %s\npersonal branch: %s\nlocal clone:     %s\n",
			path, orNone(cfg.PersonalRepo), orNone(cfg.PersonalBranch), clone)
		return nil
	}

	cfg, err := config.Load()
	if err != nil {
		cfg = config.Config{}
	}
	if *repo != "" {
		cfg.PersonalRepo = *repo
	}
	if *branch != "" {
		cfg.PersonalBranch = *branch
	}
	if err := config.Save(cfg); err != nil {
		return err
	}
	fmt.Printf("wrote %s\npersonal repo: %s\n", path, cfg.PersonalRepo)
	return nil
}

func cmdMigrate(args []string) error {
	fs := flag.NewFlagSet("migrate", flag.ExitOnError)
	apply := fs.Bool("apply", false, "write the plan; without this flag nothing is modified")
	allowSecrets := fs.Bool("allow-secrets", false, "migrate files with suspected credentials anyway")
	only := fs.String("slug", "", "limit the plan to one identity slug")
	var roots multiFlag
	fs.Var(&roots, "search-root", "directory to scan for working copies (repeatable; default: home)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	plan, err := migrate.Build(migrate.Options{SearchRoots: roots, OnlySlug: *only})
	if err != nil {
		return err
	}
	printPlan(plan)

	if !*apply {
		fmt.Println("\nNothing was written. Re-run with -apply once the plan above looks right.")
		return nil
	}

	written, skipped, err := migrate.Apply(plan, *allowSecrets)
	if err != nil {
		return err
	}
	fmt.Printf("\nWrote %d file(s), skipped %d. Originals were left in place as a backup.\n", written, skipped)
	if skipped > 0 && !*allowSecrets {
		fmt.Println("Files with suspected credentials were skipped. Clean them, or re-run with -allow-secrets.")
	}
	return nil
}

// printPlan renders the migration plan for human review.
func printPlan(plan migrate.Plan) {
	for _, note := range plan.Notes {
		fmt.Println("note:", note)
	}
	if plan.PersonalRoot != "" {
		fmt.Println("personal clone:", plan.PersonalRoot)
	}

	var files, blocked, flagged int
	for _, g := range plan.Groups {
		fmt.Printf("\n%s\n", g.MangledDir)
		if !g.Resolved {
			fmt.Printf("  unresolved: %s\n", g.Reason)
			continue
		}
		fmt.Printf("  work dir: %s\n  identity: %s (%s)\n", g.WorkDir, g.Identity.Canonical, g.Identity.Source)
		for _, a := range g.Actions {
			files++
			if a.Blocked != "" {
				blocked++
				fmt.Printf("  - %-42s BLOCKED  %s\n", a.Base, a.Blocked)
				continue
			}
			marker := ""
			if a.Overwrites {
				marker = "  [overwrites existing]"
			}
			fmt.Printf("  - %-42s %-8s -> %s%s\n", a.Base, a.Type, shorten(a.Dest), marker)
			for _, p := range a.Problems {
				fmt.Printf("      format: %s\n", p)
			}
			for _, n := range a.Notes {
				fmt.Printf("      note:   %s\n", n)
			}
			for _, s := range a.Secrets {
				flagged++
				fmt.Printf("      SECRET: %s\n", s)
			}
		}
	}
	fmt.Printf("\n%d file(s) planned, %d blocked, %d secret finding(s).\n", files, blocked, flagged)
	if flagged > 0 {
		fmt.Println("Files with secret findings are skipped by -apply: the project layer is committed to shared repos.")
	}
}

func cmdSync(args []string) error {
	fs := flag.NewFlagSet("sync", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "report what would change without writing")
	quiet := fs.Bool("quiet", false, "print only warnings; for use as a hook")
	var rest []string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		rest = args[1:]
	} else {
		rest = args
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}

	dir, err := argDir(args)
	if err != nil {
		return err
	}

	res, err := sync.Run(sync.Options{Dir: dir, DryRun: *dryRun})
	if err != nil {
		return err
	}

	// The summary goes to stdout and the warnings to stderr, so a hook can stay
	// quiet on success while a real problem is still visible.
	if !*quiet && !res.Degraded {
		prefix := ""
		if *dryRun {
			prefix = "[dry-run] "
		}
		fmt.Printf("%smemory: %s — %d project, %d personal/global, %d personal/project",
			prefix, res.Identity.Canonical, res.FromProject, res.FromPersonalGlobal, res.FromPersonalProject)
		if res.Removed > 0 {
			fmt.Printf(", %d removed", res.Removed)
		}
		if res.Preserved > 0 {
			fmt.Printf(", %d local edit(s) preserved", res.Preserved)
		}
		fmt.Printf("\n%s\n", res.MemoryDir)
	}
	for _, w := range res.Warnings {
		fmt.Fprintln(os.Stderr, "memory-manager:", w)
	}
	return nil
}

func cmdStatus(args []string) error {
	dir, err := argDir(args)
	if err != nil {
		return err
	}
	plan, err := writeback.Build(dir)
	if err != nil {
		return err
	}
	printWriteback(plan)
	if plan.Empty() {
		return nil
	}
	fmt.Println("\nRun \"memory-manager push\" to send these to their layers.")
	return nil
}

func cmdPush(args []string) error {
	fs := flag.NewFlagSet("push", flag.ExitOnError)
	dryRun := fs.Bool("dry-run", false, "report what would be written without writing")
	noPush := fs.Bool("no-push", false, "write and commit the personal layer but do not push")
	allowSecrets := fs.Bool("allow-secrets", false, "write files with suspected credentials anyway")
	quiet := fs.Bool("quiet", false, "print only the summary and warnings; for use as a hook")
	var rest []string
	if len(args) > 0 && !strings.HasPrefix(args[0], "-") {
		rest = args[1:]
	} else {
		rest = args
	}
	if err := fs.Parse(rest); err != nil {
		return err
	}

	dir, err := argDir(args)
	if err != nil {
		return err
	}

	plan, err := writeback.Build(dir)
	if err != nil {
		return err
	}
	if plan.Empty() {
		if !*quiet {
			for _, w := range plan.Warnings {
				fmt.Fprintln(os.Stderr, "memory-manager:", w)
			}
			fmt.Println("memory: nothing to push")
		}
		return nil
	}
	if !*quiet {
		printWriteback(plan)
	}

	res, applyErr := writeback.Apply(plan, writeback.Options{
		DryRun:       *dryRun,
		NoPush:       *noPush,
		AllowSecrets: *allowSecrets,
	})

	prefix := ""
	if *dryRun {
		prefix = "[dry-run] "
	}
	fmt.Printf("%smemory: personal %d written, %d removed; project %d written, %d removed",
		prefix, res.PersonalWritten, res.PersonalRemoved, res.ProjectWritten, res.ProjectRemoved)
	if res.Blocked > 0 {
		fmt.Printf("; %d blocked", res.Blocked)
	}
	switch {
	case res.Pushed:
		fmt.Print("; pushed")
	case res.Committed:
		fmt.Print("; committed, not pushed")
	}
	fmt.Println()

	// The project layer is deliberately left uncommitted, so the user has to be
	// told which files are now waiting in their work tree.
	if len(res.ProjectFiles) > 0 && !*dryRun {
		fmt.Println("\nProject memory written to your work tree, not committed. Commit these with your code:")
		for _, f := range res.ProjectFiles {
			fmt.Println("  ", f)
		}
	}
	for _, w := range res.Warnings {
		fmt.Fprintln(os.Stderr, "memory-manager:", w)
	}
	// Reported after the summary so the user can see what did succeed.
	return applyErr
}

// printWriteback renders a write-back plan, matching the migrate plan's shape.
func printWriteback(plan writeback.Plan) {
	for _, w := range plan.Warnings {
		fmt.Fprintln(os.Stderr, "memory-manager:", w)
	}
	if plan.Empty() {
		return
	}

	fmt.Printf("identity: %s\nmemory:   %s\n\n", plan.Identity.Canonical, plan.MemoryDir)
	for _, a := range plan.Actions {
		if a.Blocked != "" {
			fmt.Printf("  %-8s %-42s BLOCKED  %s\n", a.Change, a.Base, a.Blocked)
		} else if a.Change == writeback.Moved {
			fmt.Printf("  %-8s %-42s %s -> %s\n", a.Change, a.Base, a.FromLayer, shorten(a.Dest))
		} else if a.Change == writeback.Removed {
			fmt.Printf("  %-8s %-42s %-8s from %s\n", a.Change, a.Base, a.Layer, shorten(a.DeleteFrom))
		} else {
			fmt.Printf("  %-8s %-42s %-8s -> %s\n", a.Change, a.Base, a.Layer, shorten(a.Dest))
		}
		for _, p := range a.Problems {
			fmt.Printf("      format: %s\n", p)
		}
		for _, s := range a.Secrets {
			fmt.Printf("      SECRET: %s\n", s)
		}
	}
}

// multiFlag collects a repeatable string flag.
type multiFlag []string

func (m *multiFlag) String() string { return strings.Join(*m, ", ") }

func (m *multiFlag) Set(v string) error {
	*m = append(*m, v)
	return nil
}

// shorten trims a destination to its meaningful tail so the plan stays readable
// in a terminal.
func shorten(path string) string {
	parts := strings.Split(filepath.ToSlash(path), "/")
	if len(parts) <= 4 {
		return path
	}
	return ".../" + strings.Join(parts[len(parts)-4:], "/")
}

func orNone(s string) string {
	if strings.TrimSpace(s) == "" {
		return "(none)"
	}
	return s
}
