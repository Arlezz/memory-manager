---
description: Check the memory-manager setup and explain what is missing
allowed-tools: Bash(memory-manager identity:*), Bash(memory-manager config:*), Bash(memory-manager version:*), Bash(memory-manager:*)
---

Check that memory sync is set up for this project, then explain the result.

Run these and interpret them together:

- `memory-manager version`
- `memory-manager config` — is a personal repo configured?
- `memory-manager identity` — does this project resolve to a stable identity?

Then tell the user exactly which of these applies:

- **No identity**: this directory has no git remote and no `.claude/memory-id`.
  Offer `memory-manager init` and explain that a folder-name identity does not
  travel to another machine.
- **No personal repo**: personal memory has nowhere to go. Explain they need a
  private repo of their own and the command
  `memory-manager config -personal-repo <url>`.
- **All set**: say so, and mention `/memory-status` for day-to-day use.
