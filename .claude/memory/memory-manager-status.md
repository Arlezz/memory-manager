---
name: memory-manager-status
description: "memory-manager as of 2026-09-17 - security audit remediated and merged, the cycle verified end to end, and v0.1.0 released from a pipeline that had never run before"
metadata: 
  node_type: memory
  type: project
  originSessionId: 4a902265-6735-48e3-8379-d9bfd8bb2e36
  modified: 2026-09-18T01:20:19.000Z
---

State of [[memory-manager-goal]]. Anton's personal open-source actions live separately in [[memory-manager-open-actions]], which is deliberately kept out of this public tree.

Both halves of the cycle are built: `sync` at SessionStart and `push` at SessionEnd, plus `identity`, `init`, `config`, `migrate`, `status`. Packaged as a Claude Code plugin and for npm. Docs are `docs/architecture.md`, `docs/how-it-works.md`, `docs/security.md`, and the architecture diagram at three size presets in `docs/diagrams/`. Design rationale lives in [[memory-manager-tech-decisions]]; the code and README record the rest.

**Published 2026-08-30** at `github.com/Arlezz/memory-manager`, a **public** repo — which is why [[memory-manager-no-employer-names]] exists.

## The security audit is remediated — 2026-09-17

A full security and quality audit ran on 2026-09-02 against `8300212` and found 17 confirmed findings: one critical, four high, seven medium, five low. The report itself is deliberately **not in the repo** — `/auditoria/` is gitignored, because it names the real personal repository and quotes real state paths. It carries its own remediation status table at the end.

Sixteen were closed across six PRs and are merged into `main`. Four points were deferred on purpose, as decisions rather than code: the index cap (A-07), a `purge` command for git history (A-06), Windows ACLs (A-11), and the coverage of `claudedir` and `layer` (A-16).

What the critical one was: `git clone` received the configured personal repo URL as a bare argv element with no `--` terminating git's options, so a value beginning with a dash was read as a flag — and `--upload-pack` names a command git runs. **Writing a config file was execution at the next session start.** Closed by terminating the options in both clones and refusing to record an option-shaped value at all.

The four declared no-gos are now covered: execution (A-01), losing a memory (A-04, A-09), leaking a secret (A-03, A-05, A-11), and a poisoned memory reaching the context (A-02, A-08).

Where the fixes departed from the report, and why, is recorded in the report's own remediation section. The pattern worth remembering: **twice the report recommended something broader than the problem**, and narrowing it was right both times — a blanket fail-closed would have made `config` unusable outside a git repo, and blocking on unterminated frontmatter would have fought an explicit design rule while the atomic write already removed the cause.

## Verified end to end — 2026-09-17

The whole cycle was exercised on a disposable bench with two devices, each with its own `CLAUDE_CONFIG_DIR` and **the same project checked out at two different paths**, which is the thing this tool exists to make work. Create, push, arrive on the second device, edit there, arrive back, delete, propagate, archive before deleting, and no resurrection on the next sync. Fifteen of the sixteen fixes were confirmed against the installed binary, not just in tests.

**Testing found a real defect in the remediation itself.** The A-15 fix recorded hook failures to `last-error.log` on the two spawn paths but not on the earliest exit — no binary found — which is the failure a new or broken install actually produces. Fixed in a follow-up PR. The lesson repeats one already in this file: the suite passing proves less than running the thing.

## Lessons that keep earning their place

**A green local suite proves nothing about CI.** It happened twice now. First on 2026-08-30: `Push` ran `pull --rebase` without the per-command git identity, so it failed on any machine with no global git identity — every runner — and the code called it a conflict that did not exist. Then again on 2026-09-17: a fix passed locally and failed on all three runners because `t.Chdir` needs Go 1.24 and CI pinned 1.23. **That second one was the audit's own CI finding demonstrating itself**, and it is why the toolchain PR was merged before the code PRs, against the planned order.

**GitHub push protection is active on this repo, and it blocks at push time.** It rejected the first push on 2026-08-30 over synthetic `glpat-`-shaped fixtures, which were split across a compile-time concatenation. It rejected another on 2026-09-17 over Slack-token and Stripe-key shapes in a new test corpus; those were rewritten as de-vendored synthetic values instead. **Either trick works — the point is that a token-shaped literal never reaches a commit.** The "allow secret" escape hatch was deliberately not used: marking fake secrets as allowed pollutes the repo's alerts permanently.

**The SessionEnd hook is cancelled as a matter of routine, on the network step.** Confirmed headless on 2026-08-30 and interactively on 2026-08-31: the binary copies the memory into the personal layer and commits it, then Claude Code prints `Hook cancelled` before `git push` runs. The clone is left ahead, `status` used to report nothing, and a second machine would see nothing. This is the dangerous shape for a sync tool: the local side looks finished.

It is not fixed, because it is Claude Code's, not this tool's — it is **survivable**. `74bb55a` detects the stranded commit from local refs and the next `push` publishes it even with an empty plan. As of 2026-09-17 the advice printed alongside that count also distinguishes "waiting to be sent" from "in conflict", which a push cannot resolve; the old message sent the user around a loop.

**The project layer had no unavailability guard, and six project memories were lost.** Recovered from Claude Code's `file-history/` and a session transcript. `layer.Read` reports a missing directory as an empty one, so an absent `.claude/memory` was indistinguishable from a deliberate wipe. Fixed in `483023b`: the same guard as the personal layer, a warning naming the missing directory, and an archive under `~/.claude/memory-manager/removed/<slug>/<date>/` written before any deletion — a file that cannot be archived is not deleted at all.

## v0.1.0 is released — 2026-09-17

The first tag ever, on `d7b40f5`. Until it existed there were no downloadable binaries and no npm package, so `install.ps1` / `install.sh` could not work for anyone and building from source was the only path.

`release.yml` had **never executed**, so the tag was also the only way to find out whether it worked. It did, on the first run: three-OS tests with `-race`, six cross-compiled targets, `SHA256SUMS`, and the release published. The npm job skipped itself cleanly because `NPM_TOKEN` is absent, which is the designed behaviour and not a failure.

Verified rather than assumed: the published Windows binary was downloaded back, its checksum matched `SHA256SUMS`, and it ran and reported `v0.1.0`.

The release notes are hand-written, not generated — with no previous tag the generated ones would have listed the entire history. They say what the tool does, what it needs, and the three limitations a user deserves up front: the cancelled SessionEnd hook, that deleting a memory leaves it in git history, and that the index grows without bound.

npm is deliberately not part of it. Both names — `memory-manager-cli` and the `@memory-manager` scope — **were confirmed free on 2026-09-17**, but publishing to a registry is irreversible in a way a tag is not, and one untested pipeline at a time is enough. Reserving the names is in [[memory-manager-open-actions]].

## The plugin and the binary update separately, and both were done

The binary at `~/.claude/memory-manager/bin/` was rebuilt from current `main` on 2026-09-17 with the release flags. The plugin is a separate thing: the hooks run `launch.js` out of the marketplace cache, which pins whatever commit it was installed from, so fifteen of the sixteen fixes were live through the binary while **A-15 sat in the launcher, unreachable**.

Both are current now — `claude plugin marketplace update memory-manager` followed by `claude plugin update memory-manager@memory-manager` moved the cache from `830021217fc6` to `d7b40f5f324a`. **The two halves drift independently and neither updates the other**, so after any change to `plugin/` the plugin needs its own update, and a change to `cmd/` or `internal/` needs the binary rebuilt. See [[plugin-cache-pins-a-commit]].

## One gap nothing covers

**CI does not check `launch.js` in any way**, not even `node --check`. There is no JS test infrastructure at all — `package.json` declares no test script. The audit read the launcher but never measured its coverage, so this is not one of its findings. It belongs with the deferred half of A-16.

## Still unverified

**Nobody has looked at the architecture diagram render.** Playwright is not installed. The geometry assertions in `docs/diagrams/generate.py` and the plugin's `self_check.py` cover the mechanical rules — mask overlaps, corridor fits, attach spacing, the accessible-SVG contract — but not whether it looks good. One change was made on suspicion rather than sight: the focal box's copy was split in two because 328px of height held only ~54px of centred text. `pip install playwright && playwright install chromium` (~150MB) unlocks the PNG exports and the ability to look at it.

## The open design question

A-07 gave the deferred index question a number: at 10,001 memories a sync takes 34s against a 60s hook timeout, and `MEMORY.md` reaches 717 KB — roughly 180,000 tokens of context before the conversation starts. Truncating each description (done, A-08) bounds the size per entry; it does not bound the number of entries. The candidates remain a trimming policy for `MEMORY.md` and a self-hosted server with semantic search over the same markdown files. See [[memory-manager-scope-decisions]].

Auto-commit granularity for the personal layer stays at one commit per session until there is real noise to measure.

**Why:** the audit is closed and the cycle is proven, so the next session should not start by re-verifying either. What is left is a release, a design call, and the deferred four.

**How to apply:** do not re-run the suite or re-read the architecture to get oriented — both are committed, and the audit report carries its own remediation table. Start from the release prerequisites above, or from [[memory-manager-open-actions]].
