---
description: Adopt memory that still lives in Claude Code's path-keyed directories
allowed-tools: Bash(memory-manager migrate:*), Bash(memory-manager:*)
---

Adopt pre-existing memory into the two layers.

1. Run `memory-manager migrate` with no flags. It only prints a plan.
2. Walk the user through it: how many files, which identity each resolved to,
   which layer each is headed to.
3. **Call out every SECRET finding explicitly.** The project layer gets committed
   to a shared repository, and a secret in a shared history is not undone by a
   revert. These files are skipped unless the user overrides.
4. Only run `memory-manager migrate -apply` after the user approves the plan.

Never pass `-allow-secrets` on your own initiative.
