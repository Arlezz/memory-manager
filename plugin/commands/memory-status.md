---
description: Show which memories are waiting to go back to their layer
allowed-tools: Bash(memory-manager status:*), Bash(memory-manager:*)
---

Run `memory-manager status` in the current project and report the result.

Explain, in one short block:

- which memories are pending, and which layer each one is headed to
- anything BLOCKED, and why (a missing layer, or a suspected credential)
- anything with a `format:` note that would degrade the generated index
- a `personal layer: N commit(s) committed but not pushed` line, if present — that memory exists on
  this machine only, because an earlier run was cut off before its push. Say so plainly; it is easy
  to miss next to a list of files.

If nothing is pending, say so in one line. Do not run `push` unless the user asks.
