---
description: Send memory written this session back to its layer
allowed-tools: Bash(memory-manager status:*), Bash(memory-manager push:*), Bash(memory-manager:*)
---

Send this session's memory back to its layers.

1. Run `memory-manager status` first and show what will happen.
2. If anything is BLOCKED by a suspected credential, stop and report it. Do not
   pass `-allow-secrets` unless the user explicitly says the finding is a false
   positive.
3. Otherwise run `memory-manager push`.
4. Report what was pushed. **If project memory was written to the work tree,
   list those files and remind the user they are deliberately left uncommitted**
   so they go through review with the code.
