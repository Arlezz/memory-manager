---
name: memory-manager-tech-decisions
description: "memory-manager implementation choices — Go binary, remote-pure identity, generated MEMORY.md, degrade-and-warn, and project memory never auto-committed"
metadata: 
  node_type: memory
  type: project
  originSessionId: e832f050-5cdb-4ddf-981c-c30c9b723159
  modified: 2026-08-28T04:15:13.638Z
---

Locked by Anton for [[memory-manager-two-layer-design]]. Decided 2026-08-27, extended the same day when the write half was built.

**Identity and format**

- **Go**, static binary per OS. Targets **Windows, macOS, Linux**.
- **Identity = normalized git remote, pure.** Same remote means same memory even across different folders — backups and worktrees included, accepted deliberately. Override/fallback: `.claude/memory-id` in the repo root, which always wins, is searched upward from the working directory, and **also marks the project root when there is no git repo** so the ~40% of his directories that are not repos can still hold project memory.
- Normalization must **strip credentials from the URL** before the slug touches any file. Hard security requirement — see [[git-remote-credential-leak]].
- **`MEMORY.md` is generated** from each file's frontmatter on every sync, never versioned. Kills the merge-conflict hotspot instead of managing it; frontmatter is the source of truth.
- `git` is invoked as a **subprocess, not a library** — inherits credential helpers, SSH agent and proxy config, which are heterogeneous across his GitHub and GitLab remotes.
- Zero third-party dependencies, including a hand-written parser for the YAML subset the memory format uses.

**Routing and transport**

- Personal layer: a **new private repo on Anton's personal GitHub**, laid out as `global/` + `projects/<slug>/`. `user` memories go global; `feedback` and `reference` stay project-scoped.
- Write routing: `project` → project layer; `user`/`feedback`/`reference` → personal. An optional `scope: project|personal` frontmatter field overrides the default. A change to `type` or `scope` **moves** the memory between layers, writing to the new one and deleting from the old.
- **The project layer is never auto-committed.** It lives inside the work repo, so an automatic commit would land on whatever branch Anton is on and leak into his PR. `push` writes the files and lists them; he commits them with his code, which also makes project memory pass code review.
- **The personal layer commits once per session and pushes**, with `pull --rebase` and one retry. A real conflict aborts the rebase, publishes nothing, and names the file.
- Hooks: `SessionStart -> sync -quiet`, `SessionEnd -> push -quiet`.

**Packaging — decided 2026-08-28 after looking at [[claude-sync-reference]]**

- **A Claude Code plugin is the preferred install**, not editing `settings.json`. The plugin declares its own hooks in `plugin/.claude-plugin/plugin.json`, so the user's settings file is never touched. `.claude-plugin/marketplace.json` at the repo root points at `./plugin`. The install scripts stay as the fallback.
- The plugin cannot ship a per-platform binary (the marketplace distributes source), so `plugin/hooks/launch.js` resolves one in this order: `MEMORY_MANAGER_BIN`, `~/.claude/memory-manager/bin/`, then PATH. It **always exits 0** and prints the reason to stderr — the never-block rule enforced at the hook boundary.
- **npm is the primary way to get the binary**: `memory-manager-cli` is a launcher with six per-platform packages as optional dependencies. `npm i -g` beats curl-to-shell on all three OS.
- Slash commands live in `plugin/commands/*.md` and are instructed never to pass `-allow-secrets` on their own initiative.

**Failure behaviour — the rule is degrade and warn, never block**

- No network, no clone, dirty personal clone, corrupt manifest: warn, continue on local memory, exit 0.
- An **unpushed local edit is never overwritten** by the layer copy (hash compared against the manifest).
- An **unavailable layer is not a deletion**; its files are kept and stay tracked.
- A **wholesale disappearance of every tracked file is not propagated** — that is a wiped directory, not a purge.
- A **suspected credential blocks the write to any layer**, because `push` runs unattended from a hook.

**Why:** these are the answers that shape the code; re-deciding them would mean rewriting the identity, index and routing layers.

**How to apply:** both halves of the cycle are built, tested and committed as of 2026-08-27. What remains is not implementation of this design: a server-backed semantic search over the same markdown files, and a trimming policy for `MEMORY.md` past ~200 memories. Auto-commit granularity for the personal layer stays at one commit per session until real noise justifies changing it.
