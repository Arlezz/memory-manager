# Security

What this tool protects, what it does not, and the weaknesses it knows about.

Written to be useful rather than reassuring: the sections marked **Gap** are real and unfixed.

## What it handles

memory-manager reads private notes, writes into shared repositories, and holds repository
credentials in flight. Three properties follow from that, and each has tests.

### 1. Credentials never reach disk through an identity

A git remote can carry an inline credential:

```
https://user:glpat-xxxxxxxxxxxx@gitlab.example.com/team/service.git
```

That URL is the input to identity resolution, and the derived value ends up in file names, in the
manifest, in log lines and in terminal output. Normalization therefore strips credentials **before**
the value is used anywhere:

- `internal/identity/normalize.go` drops the userinfo component for both URL and scp-style remotes.
  The scp form is handled separately because a password containing a colon otherwise makes the
  username look like the host — which would put the token itself into the identity.
- Error messages pass through `redact()`, so a malformed remote can be reported without reproducing
  the secret.

Tested by `TestNormalizeStripsCredentials`, which asserts that neither the canonical identity nor
the slug contains any part of the credential, for four remote shapes. This was a real bug caught by
that test, not a hypothetical.

### 2. Suspected credentials block a write to any layer

The project layer is committed into a shared repository. A secret that reaches a shared history is
**not** undone by a revert — it has to be rotated. `internal/secrets` therefore scans every file
before it is written to a layer, and any finding blocks that file.

This matters most in `push`, which runs unattended from a `SessionEnd` hook: nobody is reading the
output at the moment it happens.

Detection has two tiers:

- **Named patterns** for the token shapes actually in use: credentials in a URL, `glpat-`,
  `GITLAB*-`, `gh[pousr]_`, `github_pat_`, `AKIA`/`ASIA`, `sk-ant-`, `sk-`/`sk-proj-`, `AIza`,
  `xox[baprs]-`, PEM private key blocks, and `password=` / `api_key=` / `access_token=`-style
  assignments. Prefix matches carry no false positives.
- **An entropy heuristic** for formats no prefix knows about: 32+ character runs with both digits
  and letters, above 4.2 bits per character, excluding hex strings, path-like strings, and anything
  that decomposes into words (camelCase, snake_case, kebab-case, a hex revision id followed by
  words).

Findings are masked (`glpa********abcd`) so the report itself does not become a second copy of the
secret, and a line matched by a named rule suppresses the heuristic on that line so nothing is
reported twice.

**The heuristic is deliberately tuned for precision over recall.** On the real corpus it went from
40+ findings to 3, of which 2 were genuine. A scan that cries wolf gets ignored, and an ignored scan
protects nothing. The trade-off is stated plainly below under Gaps.

### 3. No shell, no prompt, no hang

`internal/gitx` executes `git` through `exec.CommandContext` with an argument vector. No command
string is ever assembled, so a repository path, a branch name or a remote URL cannot inject a shell
command — including on Windows, where quoting rules differ.

Every call also runs with:

```
GIT_TERMINAL_PROMPT=0
GIT_ASKPASS=echo
GCM_INTERACTIVE=never
```

and a 30-second deadline. A hook that blocks on an invisible credential prompt hangs the session
start with no way to answer it, which is a denial of service on the user's own tool.

## What it does not do

**No encryption.** The store is plain markdown in git repositories. Confidentiality is entirely the
access control of those repositories: the project layer inherits the project repo's, the personal
layer inherits your private repo's. If you need encrypted-at-rest sync of your whole `~/.claude`,
that is a different tool — see the compatibility note in the [README](../README.md).

**No authentication of its own.** All network access is `git` inheriting your credential helpers,
SSH agent and proxy configuration. memory-manager never reads, stores or transmits a credential
itself, and never writes to your git config.

**No integrity verification of remote content.** A memory pulled from a layer is trusted as written.
Signed commits are the mechanism for that and are entirely up to your repository configuration.

## Trust boundaries

| Input | Trusted? | Why it matters |
|---|---|---|
| `git remote get-url` output | **no** | may contain a credential; normalized and redacted before use |
| memory file content | **no** | scanned for credentials before any layer write |
| memory file names | yes, structurally | come from `os.ReadDir`, so they cannot contain a path separator |
| `.claude/memory-id` | yes | run through `Slugify`, which strips everything outside `[a-z0-9._-]` and `/` |
| `~/.claude/memory-manager/state/*.json` | **yes — see Gap 1** | the manifest drives deletions |
| `~/.claude/memory-manager/config.json` | yes | your own configuration |

## Gaps

### Gap 1 — the manifest is trusted, and it drives deletions

`push` reads `origin` out of the manifest and deletes that path when a memory is removed or moved
between layers. The manifest is local state under `~/.claude`, written only by this tool, so in
normal operation the paths are ones it wrote itself.

But nothing validates them on read. A manifest that was tampered with, or synced in from another
machine by an unrelated backup tool, can name an arbitrary path for deletion.

Two mitigations are in place and neither is a fix: a wholesale disappearance of every tracked file
is refused rather than propagated, and the manifest is rebuilt from empty when it fails to parse.

A proper fix is to require that every deletion target sits inside a known layer root. Not
implemented.

### Gap 2 — the personal repository is not verified to be private

The tool asks for a repository URL and pushes personal memory to it. It does not and cannot check
that the repository is private; a public URL is accepted silently, and every preference and
working-style note goes to a public repo.

Check it yourself when you run `memory-manager config -personal-repo`.

### Gap 3 — `config.json` may hold a credential, with weak protection on Windows

If your personal repo URL embeds a token, that token is written to
`~/.claude/memory-manager/config.json`. The file is created with mode `0600`, which is meaningful on
Linux and macOS and effectively ignored on Windows, where no ACL is set.

Prefer SSH or a credential helper over a token in the URL. There is a test asserting `0600`, and it
skips on Windows precisely because the guarantee does not hold there.

### Gap 4 — the secret scanner is advisory

It is precision-tuned, so it misses things by design:

- a credential with no recognizable prefix and low entropy (a short password, a dictionary
  passphrase) passes
- a credential inside a string that decomposes into words passes
- one known false-positive class remains: generated CSS class names such as next/font's
  `inter_<hash>_variable` are reported. One in 103 files on the real corpus

It is a safety net for the unattended `push`, not a guarantee. The primary control for the project
layer is that **a human reviews the commit**, which is exactly why that layer is never committed
automatically.

`-allow-secrets` overrides a block. It exists for false positives, is never passed by the hooks, and
the plugin's slash commands are instructed never to pass it on their own initiative.

### Gap 5 — the project layer inherits the project repo's visibility

Project memory is committed into the work repository. If that repository is ever made public, or
its history is shared, the memory goes with it — including notes about clients, architecture and
internal systems.

The mitigation is structural rather than technical: routing defaults to the personal layer for
everything except `type: project`, sharing requires an explicit `scope: project`, and the files
land in the work tree uncommitted so they appear in a diff a human reads.

### Gap 6 — no size limits

Memory files are read whole into memory with `os.ReadFile`, and there is no cap on file size or file
count. The frontmatter scanner caps a single line at 4 MB and errors beyond that. A pathological
file in a memory directory can therefore consume memory proportional to its size.

Local files written by your own agent are not a realistic attack vector, but a shared project layer
is content from other people, so this is worth naming.

## Reviewing this yourself

The security-relevant code is small and worth reading directly:

```
internal/identity/normalize.go     credential stripping, redaction
internal/secrets/secrets.go        the scanner and its tuning
internal/gitx/gitx.go              subprocess execution, no shell, no prompts
internal/writeback/writeback.go    the block-on-finding decision, deletion targets
internal/personal/personal.go      what gets staged, committed and pushed
```

The properties above are pinned by tests. Run them with:

```sh
go test -race ./...
```

Tests never use a real credential. Synthetic stand-ins are defined at the top of
`internal/secrets/secrets_test.go` and `internal/identity/normalize_test.go`; a committed test file
containing a live token would be the exact leak this package exists to prevent.

## Reporting a problem

Open an issue at <https://github.com/Arlezz/memory-manager/issues> for anything non-sensitive. For
something that should not be public, describe the class of problem without a working exploit and ask
for a private channel first.
