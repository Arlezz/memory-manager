# Architecture

## The problem, precisely

Claude Code stores per-project memory in `~/.claude/projects/<mangled-path>/memory/`, where
`<mangled-path>` is the project's absolute path with every non-alphanumeric character replaced by
a dash:

```
C:\Users\anton\Documents\projects\ORBIT-X_core  ->  C--Users-anton-Documents-projects-ORBIT-X-core
/home/anton/repos/orbit-x_core               ->  -home-anton-repos-orbit-x-core
```

Three consequences follow, and all three are load-bearing for this design:

1. **The key is not portable.** The same repository cloned to a different path — a second machine,
   a different folder name, another drive — resolves to a different memory directory. Memory does
   not travel with the code.
2. **The mapping is lossy.** Both `nova_core` and `nova-core` mangle to `nova-core`, so it cannot
   be inverted. Recovering the working directory for a given memory directory requires walking the
   disk and mangling forward, which is what `internal/migrate` does.
3. **The mapping is not even stable locally.** The drive letter's case follows the shell that
   launched the session, so one machine can hold both `c--Users-…` and `C--Users-…` for sibling
   projects.

The fix is to stop using the path as the identity.

## Identity

The identity of a project is its **normalized git remote**. Everything that can differ between two
clones of the same repository is discarded:

| Discarded | Why |
|---|---|
| credentials (`user:token@`) | a security requirement, not cosmetic — see [security.md](security.md) |
| scheme (`https`, `ssh`, `git`) | the same repo over SSH and HTTPS is one project |
| port | reaching a host on 22, 443 or 2222 does not make it a different repo |
| `.git` suffix, trailing slash | cosmetic variants of the same URL |
| letter case | fixes the drive-letter instability above |

```
https://user:TOKEN@github.com/ORBIT-DEV/ORBIT-X_core.git
git@github.com:ORBIT-DEV/ORBIT-X_core.git
                    │
                    ▼
       github.com/orbit-dev/orbit-x_core        (canonical)
       github.com__orbit-dev__orbit-x_core      (filesystem slug)
```

Path separators become `__` rather than a hash. A hash would be shorter, but being able to read
the store by hand is what makes a future migration to a server-backed index safe to attempt.

**Consequence, accepted deliberately:** two checkouts of one repository share one memory, backup
folders and worktrees included. A checkout that wants its own memory pins it in
`.claude/memory-id`, which also covers repositories with no remote and repositories moved between
organizations.

`.claude/memory-id` is searched upward from the working directory, stopping at the repository root
when there is one. Outside a repository it also *marks* the project root, so a directory that is
not a git repo can still hold a project layer.

## Two layers

| Layer | Holds | Lives in | Reviewed? |
|---|---|---|---|
| **Project** | architecture decisions, conventions, why an option was rejected | `.claude/memory/` committed in the project repo | yes, in the same PR as the code |
| **Personal** | preferences, working-style feedback, cross-project context | a private repo: `global/` and `projects/<slug>/` | no |

Routing is by memory `type`, with an explicit override:

```
type: project                      -> project layer
type: user                         -> personal layer, global/
type: feedback | reference         -> personal layer, projects/<slug>/
scope: project | personal          -> overrides all of the above
```

The default direction is the conservative one: only `project` is shared, so nothing leaks into a
team repository by accident. Sharing is one line of frontmatter away; unsharing something already
committed is not.

## The cycle

```
        SessionStart                  during the session               SessionEnd
        ┌──────────┐                  ┌──────────────┐                 ┌────────┐
        │   sync   │                  │ Claude writes│                 │  push  │
        └────┬─────┘                  │ into the     │                 └───┬────┘
             │                        │ native dir   │                     │
  ┌──────────┴──────────┐             └──────┬───────┘                     │
  │                     │                    │                             │
project layer      personal layer            │              ┌──────────────┴─────────────┐
(work tree,        (private repo,            │              │                            │
 already local)     git pull)                │        project layer               personal layer
  │                     │                    │        written to the              written, committed,
  └──────────┬──────────┘                    │        work tree,                  pushed with
             ▼                               │        NOT committed               rebase-and-retry
      merged directory  ────────────────────►│                     ▲
      + generated MEMORY.md                  │                     │
      + manifest (layer, origin, hash)  ─────┴──── diff ───────────┘
```

### `sync` (SessionStart)

1. Resolve identity. No identity is not an error: warn, use local memory, exit 0.
2. Read the project layer from the work tree — no network needed, it arrived with `git pull`.
3. Pull the personal clone. Failure is a warning, not an error.
4. Merge into the native directory. Later sources win: personal beats project (an explicit personal
   preference outranks a team default), and project-scoped personal memory beats global personal
   memory (the more specific statement wins).
5. Write the manifest: for every file, its layer, origin path and content hash.
6. Regenerate `MEMORY.md` from frontmatter.

### `push` (SessionEnd)

Diff the native directory against the manifest:

| State | Detected by | Destination |
|---|---|---|
| added | on disk, absent from the manifest | the layer its `type`/`scope` selects |
| updated | content hash differs from the manifest | its recorded origin, preserving global vs project-scoped placement |
| removed | in the manifest, absent from disk | deleted from its layer |
| moved | `type`/`scope` now selects the other layer | written to the new layer, deleted from the old |

The `moved` case is the one that leaves duplicates when missed: a memory reclassified from
`feedback` to `project` has to leave the personal repo, not merely appear in the project one.

Then: the personal layer is committed (one commit per session) and pushed. **The project layer is
written to the work tree and left uncommitted** — see the decision below.

## Design decisions and their reasons

### `MEMORY.md` is generated, never committed

Everyone who adds a memory touches the index, so a versioned index is the one guaranteed merge
conflict in the store — per contributor, per session. Deriving it from each file's frontmatter
removes the conflict class outright and makes the frontmatter the single source of truth.

It also leaves room to grow: a generated index can be grouped or trimmed when the store passes a
few hundred memories, which a hand-maintained file cannot.

Cost, accepted: a line Claude writes into `MEMORY.md` is overwritten at the next sync.

### The project layer is never auto-committed

It lives *inside* the user's repository. An automatic commit would land on whatever branch they are
on, mix into their pull request, and race their dirty tree. `push` writes the files and lists them;
the user commits them with their code.

This is not only about avoiding damage. It is what makes project memory pass code review the way
code does, which is the mitigation for the remaining leak risk in
[security.md](security.md).

### `git` is a subprocess, not a library

Invoking the `git` binary inherits the user's credential helpers, SSH agent and proxy
configuration. A Go git library would mean reimplementing authentication against a mix of GitHub
over SSH, GitHub over HTTPS and self-hosted GitLab — the largest available source of bugs, in the
component that must not fail quietly.

Every call runs with `GIT_TERMINAL_PROMPT=0` and a 30-second deadline, because a hook that blocks
on an invisible password prompt hangs the session start with no way to answer it.

### Zero third-party dependencies

The frontmatter parser covers only the subset of YAML the memory format uses — flat scalars plus a
one-level `metadata` map — and reports anything richer rather than guessing. The cost is a small
parser; the benefit is a static binary with no supply chain, for a tool that reads private notes
and holds repository credentials in flight.

## Failure behaviour

The rule is **degrade and warn, never block**. A hook that fails a session is worse than the manual
copying this tool replaces. Silence is the other failure mode, so every degradation prints a line.

| Situation | Behaviour |
|---|---|
| No identity | warn, use local memory, exit 0 |
| Personal remote unreachable | warn, use the local clone; if there is none, run project-layer only |
| Personal clone dirty | skip the pull, keep local work, warn |
| Corrupt manifest | report it, rebuild from empty |
| Unpushed local edit | **never overwritten**; hash compared against the manifest, warn and keep local |
| Layer unavailable at sync | its files are **kept and stay tracked** — not read as an upstream deletion |
| Every tracked file missing at push | **refuse to propagate**; that is a wiped directory, not a purge |
| Suspected credential | block that file from every layer, name it |
| Conflicting push | rebase once and retry; on real conflict abort, publish nothing, name the file |

Each row has a regression test. The interesting ones are in `internal/sync/sync_test.go` and
`internal/writeback/writeback_test.go`.

## Packages

```
cmd/memory-manager        CLI: identity, init, config, migrate, sync, status, push
internal/identity         remote -> canonical identity + slug; the override file
internal/claudedir        Claude Code's own layout, including the mangling
internal/frontmatter      the YAML subset the memory format uses
internal/layer            the two layers, and which one a memory belongs to
internal/index            MEMORY.md generation
internal/state            the manifest: layer, origin and hash per file
internal/secrets          credential scanning
internal/config           memory-manager's own settings
internal/gitx             the git subprocess wrapper
internal/personal         the private clone: open, pull, commit, push
internal/sync             SessionStart: pull, merge, index, manifest
internal/writeback        SessionEnd: diff, classify, route back
internal/migrate          adopting pre-existing path-keyed memory
plugin/                   the Claude Code plugin: hooks and slash commands
```

Dependencies point one way: `sync` and `writeback` sit on top of everything else and are the only
packages that orchestrate. `writeback` imports `sync` only in its tests, to build a realistic
starting state.

## What this is not

It does not back up transcripts, plans, tasks or history — only memory. It does not encrypt: the
store is plain markdown in git repositories whose access control is the repository's own. It has no
server; the long-term destination is a self-hosted index over these same files, and keeping the
store as plain markdown is what leaves that door open without a migration.

If you also want encrypted backup of everything else under `~/.claude`, see the compatibility note
in the [README](../README.md#compatibility-with-claude-sync).
