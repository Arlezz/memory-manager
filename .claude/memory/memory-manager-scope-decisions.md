---
name: memory-manager-scope-decisions
description: memory-manager scope locked to team-shared memory, Claude Code only; transport choice still open
metadata:
  type: project
---

Scope decided by Anton on 2026-08-26 for [[memory-manager-goal]]:

- **Audience: team-shared**, not single-user. Several people on the same project read and contribute memory. Implies namespacing (personal vs project) and permissions.
- **Target: Claude Code only.** Native memory format (`.md` files + `MEMORY.md` index) and native hooks. No agent-agnostic adapter layer for now.
- **Transport: still undecided.** Options compared were dedicated private git repo, self-hosted server + MCP, memory committed inside the project repo, and blob storage (S3/R2 — ruled out: no merge, no history, silently overwrites a teammate's memory).

**Why:** team-shared plus Claude-Code-only is what rules the architecture — it forces a personal/project split and lets the implementation lean on native hooks instead of building an abstraction layer.

**How to apply:** do not design agent-agnostic adapters unless Anton reopens that decision. Do not propose blob storage. Any design must answer "how do two teammates avoid clobbering each other".

