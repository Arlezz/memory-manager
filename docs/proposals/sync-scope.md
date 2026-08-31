# Proposal: what else `~/.claude` should sync

Status: **proposed**, not decided. Written 2026-08-30.

Today this tool syncs exactly one thing: curated memory. The neighbouring tool
[claude-sync](https://github.com/tawanorg/claude-sync) syncs the whole `~/.claude` directory to
object storage with `age` encryption. The question this proposal answers is which parts of that
scope are worth adopting over a git transport, and which are not.

## The measurement that decides it

From a real developer machine with 40 sessions of history:

| | size | share |
|---|---|---|
| `~/.claude` total | 128 MB | |
| session transcripts (40 `.jsonl`) | 78 MB | 61% |
| `plugins/` | 35 MB | 27% |
| `file-history/` | 11 MB | 9% |
| **curated memory (124 files)** | **572 KB** | **0.45%** |

Memory is half a percent of the directory. Everything else is bulk, and the bulk is not uniform:
some of it is small structured text that git merges well, and some of it is opaque and large.

## Why git, and what it is actually good at

The advantage over object storage is not price — Cloudflare R2 has a free tier and WebDAV can be
self-hosted. It is that **a private git repository needs no new account, no bucket, no access key
and no passphrase**. Every developer already has a git host and working credentials. That is the
property worth protecting, and it survives only as long as the repository stays small enough to
clone quickly on the machine that needs it most: a new one.

Git is good at small structured text that changes in pieces. Two machines that each add a skill
merge without help, the same way two machines that each write a memory do. Git is bad at large
opaque files that are rewritten wholesale, because the cost lands on every future clone.

## Tiers

**Tier 1 — adopt.** `skills/`, `agents/`, `rules/`, `plans/`, `tasks/`, `settings.json`,
`CLAUDE.md`, `history.jsonl`.

Under 5 MB of text combined on the measured machine. Structured, mergeable, and genuinely portable
between machines. This is roughly half the practical value of claude-sync for a small amount of
work, and it reuses the existing personal layer unchanged: same repo, same commit-per-session, same
conflict handling.

`settings.json` needs care — it holds machine-local values — so it likely syncs as a merged subset
rather than wholesale. That detail is unresolved.

**Tier 2 — never.** `plugins/`, `file-history/`, `cache/`, `paste-cache/`, `shell-snapshots/`,
`daemon/`, `session-env/`.

62 MB of the 128 MB, and none of it should travel. Plugins reinstall from their marketplace.
`file-history` is local undo state. The rest is cache. Syncing any of it costs clone time and buys
nothing.

**Tier 3 — only behind encryption, and that is a different project.** Session transcripts.

Two independent problems. The size is the smaller one: 78 MB growing daily, and although git delta
compresses append-only JSON better than expected, the cost is paid on every clone.

The real blocker is confidentiality. Transcripts contain everything typed and pasted in a session,
including credentials. The secret scanner in `internal/secrets` works because curated memories are
small, structured markdown; it cannot usefully gate tens of megabytes of `.jsonl`. This tool's
transport is plaintext by design (see [security.md](../security.md)). claude-sync encrypts with
`age` precisely because it carries this class of data — that is not an incidental feature of theirs,
it is the entry price for the scope.

Adopting Tier 3 therefore means adopting encryption first, which changes the threat model, the key
management story, and the "no new account, no passphrase" advantage above. It should be decided on
its own merits, not smuggled in as an extension of memory sync.

## What this does not change

The two-layer split stays. Tier 1 items are personal by nature: they describe how one developer
works, not what a team decided. They belong in the private personal repo, next to `global/`, and
never in a project work tree.

## Open questions

- Does `settings.json` sync wholesale, as a subset, or not at all?
- Do `plans/` and `tasks/` belong to a project rather than to the person? They are keyed by project
  today, which argues for project scope, but they are unfinished personal work, which argues
  against.
- Is `history.jsonl` append-only enough to merge by concatenation and sort, or does it need the same
  conflict path as memory?
