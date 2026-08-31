---
name: memory-manager-goal
description: "memory-manager is Anton's own project building cross-device memory sync for AI coding agents; root cause is Claude Code keying project memory by absolute path"
metadata: 
  node_type: memory
  type: project
  originSessionId: 4a902265-6735-48e3-8379-d9bfd8bb2e36
  modified: 2026-08-30T23:48:24.227Z
---

`memory-manager` is **Anton's own personal project**. It happens to live under his work directory for convenience only; his employer has nothing to do with it, and he stated that explicitly on 2026-08-27. Published publicly at `github.com/Arlezz/memory-manager` on 2026-08-30. Goal: a system to manage and sync agent/LLM memory **across devices**.

Concrete pain that started it: Anton works on a desktop PC, pushes code to GitHub, pulls on a second machine — but the per-project agent memory does not travel. Workaround before this project was zipping the memory folder and copying it by hand.

Root cause identified in session 2026-08-26: Claude Code stores project memory at `~/.claude/projects/<mangled-absolute-path>/memory/`. The key is the absolute path, so the same project checked out at a different path on another machine resolves to a *different* memory directory. Correct project identity is the **git remote URL**, not the filesystem path.

Note the tool is used *against* his work repos even though the tool itself is personal — that is why the project layer commits into shared repos, and why [[memory-manager-no-employer-names]] matters: memories written here can end up in a public tree. See [[memory-manager-scope-decisions]] and [[memory-manager-two-layer-design]].
