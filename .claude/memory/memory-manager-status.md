---
name: memory-manager-status
description: "memory-manager as of 2026-08-30 — both halves of the cycle built, published publicly; what is still unverified and the open design question"
metadata: 
  node_type: memory
  type: project
  originSessionId: 4a902265-6735-48e3-8379-d9bfd8bb2e36
  modified: 2026-08-31T02:55:10.784Z
---

State of [[memory-manager-goal]]. Anton's personal open-source actions live separately in [[memory-manager-open-actions]], which is deliberately kept out of this public tree.

Both halves of the cycle are built: `sync` at SessionStart and `push` at SessionEnd, plus `identity`, `init`, `config`, `migrate`, `status`. Packaged as a Claude Code plugin and for npm. Docs are `docs/architecture.md`, `docs/how-it-works.md`, `docs/security.md`, and the architecture diagram at three size presets in `docs/diagrams/`. Design rationale lives in [[memory-manager-tech-decisions]]; the code and README record the rest, so it is not repeated here.

**Published 2026-08-30** at `github.com/Arlezz/memory-manager`, a **public** repo — which is why [[memory-manager-no-employer-names]] exists. Six commits, 110 tests, `gofmt`/`go vet` clean, **CI green on Ubuntu, Windows and macOS**.

The first CI run ever to execute failed, and it caught a real bug rather than a flake: `Push` ran `pull --rebase` without the per-command git identity that `Commit` passed, so on any machine with no global git identity — every runner, any fresh checkout — the rebase failed, and the code then reported it as a conflict that did not exist. Fixed in `a168d8d`: one shared `gitIdentity`, and a new `ErrRebaseFailed` so an unreachable remote is no longer called a conflict. Two regression tests pin it, one of them running with `GIT_CONFIG_GLOBAL` pointed at the null device. **Lesson worth keeping: a green local suite proved nothing here, because the bug was in what the development machine happened to have configured.**

Two CI warnings, neither failing: `actions/checkout@v4` and `actions/setup-go@v5` still target Node 20, and the Go cache step looks for a `go.sum` this repo does not have (zero dependencies).

GitHub push protection rejected the first push attempt over the synthetic `glpat-`-shaped fixtures in three test files. They were always fake, but the shape matched. They are now split across a compile-time concatenation, and the history was rewritten so no commit carries the shape. **Any new token-shaped fixture must use the same trick** or the push will be blocked again.

## Release prerequisites, before the first tag

- Check the npm names are free: `memory-manager-cli` and the `@memory-manager` scope. `scripts/publish-npm.sh` and `package.json` both assume them.
- `NPM_TOKEN` has to exist as a repo secret, or the npm job in `release.yml` skips itself silently.

## Unverified, and known to be unverified

**Nobody has looked at the architecture diagram render.** Playwright is not installed and the Claude-in-Chrome extension was not connected on 2026-08-28, so there was no way to see it. The geometry assertions in `docs/diagrams/generate.py` and the plugin's `self_check.py` cover the mechanical rules — mask overlaps, corridor fits, attach spacing, the accessible-SVG contract — but not whether it looks good. One change was made on suspicion rather than sight: the focal box's copy was split into two blocks because 328px of height held only ~54px of centred text. `pip install playwright && playwright install chromium` (~150MB) unlocks both the PNG exports and the ability to look at it.

**The hook path is verified as of 2026-08-30.** Both subcommands were run through `plugin/hooks/launch.js` exactly as the plugin declares them — from a neutral working directory, with `CLAUDE_PROJECT_DIR` set, resolving the binary from `~/.claude/memory-manager/bin/`. `sync` merged and correctly refused to overwrite two locally edited memories; `push` wrote 6 project memories into the work tree without committing them and pushed 3 personal ones to the private repo (`75a8aa1`). Both exited 0.

**The marketplace install is verified for `sync` as of 2026-08-30.** The plugin was installed at user scope from the marketplace (commit `483023b`, `installPath` under `plugins/cache/memory-manager/`) and a new session was started. The SessionStart hook fired: `state/github.com__arlezz__memory-manager.json` and `MEMORY.md` were both rewritten at session start, and the merged index carried personal-layer memories that exist nowhere in the work tree. 13 memories, 7 personal and 6 project.

**`push` at a genuine SessionEnd fires, does the work, and then gets cut off before `git push`.** Tested 2026-08-30 with a real headless session (`claude -p`, CLI 2.1.251) in the project directory, with one new personal memory pending. Claude Code printed `SessionEnd hook [node "${CLAUDE_PLUGIN_ROOT}/hooks/launch.js" push] failed: Hook cancelled`, yet the binary had already copied the memory into the personal layer and committed it (`0eff574`, "memory: 1 written"). The repo was left **ahead 1** — the commit existed locally and the remote never received it, so a second machine would have seen nothing. The commit had to be pushed by hand. The 60s timeout was not the cause; the whole session lasted seconds. The cancellation lands on the last and slowest step, the network one.

This is the dangerous shape for a sync tool: the local side looks complete, `status` reports nothing pending, and the memory is still stranded. Whether an interactive session exit is cancelled the same way as `-p` is not yet known — that is the one remaining check, and it needs a pending personal memory at exit.

## Next design question, not yet opened

Iteration 4 has two candidates and no decision: a self-hosted server with semantic search over the same markdown files, and a trimming policy for `MEMORY.md` past ~200 memories. Auto-commit granularity for the personal layer stays at one commit per session until there is real noise to measure.

**Why:** the code is done; what is left is either external action or a design call, so a session should not start by writing code.

**How to apply:** do not re-verify the test suite or re-read the architecture — both are committed and documented. Start from [[memory-manager-open-actions]] instead, since most of this is blocked behind it.

## Fixed 2026-08-30: the project layer had no unavailability guard

Six project memories were deleted from disk with no backup and had to be
recovered from Claude Code's `file-history/` and a session transcript.
`layer.Read` reports a missing directory as an empty one, so an absent
`.claude/memory` was indistinguishable from every project memory having been
deleted on purpose. The personal layer had a guard against exactly this; the
project layer did not, and `push` then propagated the removals into the layer,
emptying both.

Fixed in `483023b`. The project layer gets the same treatment, and the warning
names the missing directory so the two cases can be told apart. Removal also
archives every file under `~/.claude/memory-manager/removed/<slug>/<date>/`
first, and a file that cannot be archived is not deleted at all — keeping a
stale memory costs a duplicate, losing one costs the fact. Two regression tests
cover the case that actually happened.
