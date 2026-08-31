# memory-manager (Claude Code plugin)

Cross-machine memory for Claude Code. Project memory travels in the repo; personal memory follows
you in a private repo.

Claude Code keys project memory by absolute filesystem path, so the same repository checked out at a
different path — a second machine, a different folder name — resolves to a *different* memory
directory. This keys it by the normalized git remote instead.

## What the plugin does

Two hooks, declared by the plugin itself. **Your `settings.json` is never modified.**

| Hook | Runs | Effect |
|---|---|---|
| `SessionStart` | `memory-manager sync` | pulls both layers, merges them into the directory Claude Code reads, regenerates `MEMORY.md` |
| `SessionEnd` | `memory-manager push` | routes what the session wrote back to its layer; commits and pushes the personal one |

Both are wrapped so they **never block a session**: any failure prints one line to stderr and the
session continues on local memory.

## Requires the binary

The plugin is distributed as source, so it cannot carry a per-platform binary. Install one:

```sh
go install github.com/Arlezz/memory-manager/cmd/memory-manager@latest
```

Needs Go 1.23+. Until the first release is tagged this is the only install that works — the npm
package (`memory-manager-cli`) is not published yet, and the install scripts download from GitHub
releases that do not exist. Both arrive with the first tag.

The hook launcher looks for it in this order: `$MEMORY_MANAGER_BIN`,
`~/.claude/memory-manager/bin/`, then `PATH`. If it finds none, it says so and gets out of the way.

## First run

```
/memory-setup
```

That checks the binary, the personal repo and this project's identity, and tells you exactly what is
missing.

## Commands

| Command | Does |
|---|---|
| `/memory-setup` | check the setup and explain what is missing |
| `/memory-status` | what is waiting to go back to a layer |
| `/memory-sync` | pull both layers and rebuild the index now |
| `/memory-push` | send this session's memory back |
| `/memory-migrate` | adopt memory still living in the path-keyed directories |

## Documentation

Full docs in the repository: [how it works](../docs/how-it-works.md),
[architecture](../docs/architecture.md), [security](../docs/security.md).
