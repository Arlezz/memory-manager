# How it works

A walkthrough of what actually happens on disk. For the reasoning behind these choices, see
[architecture.md](architecture.md).

## Setup, once per machine

```sh
go install github.com/Arlezz/memory-manager/cmd/memory-manager@latest
memory-manager config -personal-repo git@github.com:you/claude-memory.git
```

`go install` needs Go 1.23+ and your Go bin directory on `PATH`. It is the only install that works
until the first release is tagged; `npm install -g memory-manager-cli` and the install scripts both
arrive with that tag.

The personal repo can be empty — a repository you just created on GitHub has no branches at all,
and the first run handles that.

Then add the plugin so the hooks are wired without touching your `settings.json`:

```
/plugin marketplace add Arlezz/memory-manager
/plugin install memory-manager
```

Check it:

```sh
memory-manager version
memory-manager config
memory-manager identity
```

Or from inside a session: `/memory-setup`.

## Adopting memory you already have

If you have been using Claude Code, memory already exists under `~/.claude/projects/`. Adopt it:

```sh
memory-manager migrate                      # prints a plan, writes nothing
memory-manager migrate -apply               # after you have read the plan
```

The plan names, per file: the resolved identity, the destination layer, any frontmatter defect, and
**any suspected credential**. Files with a credential finding are skipped by `-apply`. Originals
are never deleted, so a wrong classification costs a rerun rather than a lost memory.

## A session, step by step

### 1. Session start

The plugin's `SessionStart` hook runs `sync`.

```
memory: github.com/orbit-dev/orbit-x_core — 12 project, 3 personal/global, 5 personal/project
C:\Users\anton\.claude\projects\C--Users-anton-Documents-projects-ORBIT-X-core\memory
```

On disk, before:

```
ORBIT-X_core/.claude/memory/          <- project layer, arrived with git pull
  gating-architecture.md
  http-package.md

~/.claude/memory-manager/personal/    <- private clone, just pulled
  global/
    respond-in-spanish.md
  projects/github.com__orbit-dev__orbit-x_core/
    my-scratch-branches.md
```

After:

```
~/.claude/projects/C--Users-anton-…-ORBIT-X-core/memory/
  gating-architecture.md      (project)
  http-package.md             (project)
  respond-in-spanish.md       (personal/global)
  my-scratch-branches.md      (personal/project)
  MEMORY.md                   (generated)

~/.claude/memory-manager/state/github.com__orbit-dev__orbit-x_core.json
```

The manifest records where each file came from and its hash:

```json
{
  "version": 1,
  "slug": "github.com__orbit-dev__orbit-x_core",
  "canonical": "github.com/orbit-dev/orbit-x_core",
  "entries": {
    "respond-in-spanish.md": {
      "layer": "personal",
      "origin": "C:\\Users\\anton\\.claude\\memory-manager\\personal\\global\\respond-in-spanish.md",
      "sha256": "9f2c…"
    }
  }
}
```

That hash is what makes everything else possible: it distinguishes "unchanged" from "edited during
the session" from "changed upstream".

### 2. During the session

Claude writes memory straight into the native directory, as it always does. Nothing intercepts
that. You can also edit those files by hand.

Check what is pending at any point:

```sh
memory-manager status
```

```
identity: github.com/orbit-dev/orbit-x_core
memory:   C:\Users\anton\.claude\projects\C--Users-anton-…-ORBIT-X-core\memory

  added    sonar-coverage-gate.md        project  -> .../ORBIT-X_core/.claude/memory/sonar-coverage-gate.md
  updated  respond-in-spanish.md         personal -> .../personal/global/respond-in-spanish.md
  moved    my-scratch-branches.md        personal -> .../ORBIT-X_core/.claude/memory/my-scratch-branches.md
  removed  http-package.md               project     from .../ORBIT-X_core/.claude/memory/http-package.md
```

When nothing is waiting it says so, in one line:

```
memory: nothing waiting
```

That line matters. Silence would read the same as a `status` that never ran.

Or `/memory-status` inside the session.

### 3. Session end

The `SessionEnd` hook runs `push`.

```
memory: personal 1 written, 1 removed; project 2 written, 1 removed; pushed

Project memory written to your work tree, not committed. Commit these with your code:
   C:\Users\anton\Documents\projects\ORBIT-X_core\.claude\memory\sonar-coverage-gate.md
   C:\Users\anton\Documents\projects\ORBIT-X_core\.claude\memory\my-scratch-branches.md
```

The personal layer is committed and pushed. The project layer is waiting in your work tree:

```sh
git -C ORBIT-X_core status --short
?? .claude/memory/sonar-coverage-gate.md
```

Commit it with the code it describes. That is deliberate — project memory goes through review the
same way code does.

### 4. The other machine

```sh
git pull                      # brings the project layer with the code
# session start -> sync pulls the personal layer and merges both
```

The project resolves to the same identity even though the path is different, so the same memory
arrives. That is the whole point.

## Reading the classifications

```
added     no manifest entry                        -> routed by type/scope
updated   hash differs from the manifest           -> back to its recorded origin
removed   manifest entry, no file on disk          -> deleted from its layer
moved     type/scope now selects the other layer   -> written to the new, deleted from the old
BLOCKED   no destination, or a credential found    -> nothing written, reason printed
```

`updated` goes back to *where it came from*, which preserves whether a personal memory was filed
globally or under one project. Only `moved` relocates a file.

## Cases you will actually hit

### "unpushed local changes"

```
memory-manager: respond-in-spanish.md has unpushed local changes: kept the local version.
Run "memory-manager push" to send it to its layer.
```

A previous session ended without a push. The layer copy is **not** written over your edit. Run
`push` (or `/memory-push`) and the warning goes away.

### "both sides changed"

Your local edit and the layer both moved. The local copy wins and the warning says so. If you want
the layer's version instead, delete the local file and run `sync` again.

### "personal layer unavailable"

No network, or the remote is wrong. The session runs on the project layer plus whatever the local
clone already had. Personal memories already merged are **kept, not deleted** — an unreachable
remote is not an upstream deletion.

### A conflicting push

Two machines edited the same memory. `push` rebases once and retries; one fact per file means this
usually just works. On a real conflict:

```
memory-manager: personal memory conflicts with the remote in global/respond-in-spanish.md;
resolve it by hand in the personal clone
```

Nothing was published and no rebase is left in progress. Fix it in
`~/.claude/memory-manager/personal`, commit, and push again.

### A write-back that was cut off before its push

`push` commits the personal layer and then pushes it. Claude Code can cancel the `SessionEnd` hook
between those two steps, and the cancellation lands on the network call because it is the slowest
one. What is left behind is the worst shape a sync tool has: the files are in the clone, the
manifest agrees with the disk, nothing is pending — and no other machine has the memory.

The commit count against the remote is the only thing that still knows, so it is reported wherever
you are already looking:

```
memory: github.com/orbit-dev/orbit-x_core — 6 project, 0 personal/global, 8 personal/project
personal layer: 1 commit committed but not pushed; run "memory-manager push"
```

That line survives `-quiet`, so it reaches you at session start. `status` says the same thing, and
the next `push` publishes the commit even though it has nothing new to write — the stranded state
heals itself on the next run rather than waiting for you to notice.

### A credential finding

```
  added    deploy-notes.md      BLOCKED  suspected credential in the file; not written to any layer
      SECRET: line 7: GitLab personal access token (glpa********abcd)
```

Remove the credential from the memory. If it is genuinely a false positive, `-allow-secrets`
overrides — but read [security.md](security.md) first, because the project layer is committed to a
shared repository.

### A project with no git remote

```sh
memory-manager init          # writes .claude/memory-id
```

Without a remote there is nothing portable to derive an identity from, so `init` pins one. It warns
when it falls back to a folder-name identity, because that does not identify the project on another
machine.

## Where things live

| Path | What |
|---|---|
| `~/.claude/projects/<mangled>/memory/` | the merged directory Claude Code reads |
| `~/.claude/memory-manager/config.json` | your settings (personal repo URL) |
| `~/.claude/memory-manager/personal/` | the private clone |
| `~/.claude/memory-manager/state/<slug>.json` | the manifest, per project |
| `<repo>/.claude/memory/` | the project layer, in the work tree |
| `<repo>/.claude/memory-id` | an identity override, when present |

`CLAUDE_CONFIG_DIR` relocates everything under `~/.claude`, which is how the test suite runs in
isolation.

## Command reference

```sh
memory-manager identity [dir]     # what this directory resolves to, and why
memory-manager init [dir]         # pin an identity in .claude/memory-id
memory-manager config             # show or set the personal repo
memory-manager migrate            # plan adopting path-keyed memory (-apply to write)
memory-manager sync [dir]         # merge both layers now (-dry-run)
memory-manager status [dir]       # what is waiting to go back
memory-manager push [dir]         # send it back (-dry-run, -no-push, -allow-secrets)
memory-manager version
```

Slash commands from the plugin: `/memory-setup`, `/memory-status`, `/memory-sync`, `/memory-push`,
`/memory-migrate`.
