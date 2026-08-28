---
description: Pull both memory layers and rebuild the index now
allowed-tools: Bash(memory-manager sync:*), Bash(memory-manager:*)
---

Run `memory-manager sync` for the current project.

This normally runs on its own at session start; run it by hand after pulling new
commits, or after a teammate pushed memory.

Report the counts per layer and any warning. If a warning says a local edit was
preserved, tell the user to run `/memory-push` so the edit reaches its layer.
