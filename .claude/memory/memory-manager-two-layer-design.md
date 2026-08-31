---
name: memory-manager-two-layer-design
description: Proposed memory-manager architecture — project memory in-repo, personal memory in a private repo, merged at SessionStart; awaiting Anton's approval
metadata:
  type: project
---

Architecture proposed to Anton at the end of session 2026-08-26 for [[memory-manager-goal]]. **Not yet approved** — the session ended on the open question "two layers, or one layer to start simpler?".

Two memory layers, each with its own transport:

| Layer | Holds | Where | Rationale |
|---|---|---|---|
| Project | architecture decisions, conventions, "why we rejected X" | `.claude/memory/` committed in the project repo | travels with the code, reviewed in the same PR, free onboarding |
| Personal | preferences, working-style feedback, cross-project context | dedicated private repo | follows the person across projects and machines |

`memory-manager`'s job:
1. **SessionStart** — resolve project identity from git remote, pull both layers, merge into Claude Code's memory dir
2. **On write** — route each memory by its `type`: `project` → repo layer; `user`/`feedback` → personal layer
3. **SessionEnd** — commit and push per layer whatever changed

**Why:** keeping the store as plain markdown files means a self-hosted server with semantic search (the long-term destination, rejected now as too much infra to start) can later just index the same files. No migration break.

**How to apply:** confirm the one-vs-two-layer question with Anton before writing code. Known weak points to address: noisy auto-commits per session, `MEMORY.md` index not scaling past ~200 memories, and the risk of project memory leaking if the repo is ever opened up. Constraints in [[memory-manager-scope-decisions]].
