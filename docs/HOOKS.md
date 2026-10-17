# Gas Town Hooks Management

Centralized hook management for Gas Town workspaces.

## Overview

Gas Town manages context injection for all supported agents. The mechanism varies by agent:

| Agent | Hook mechanism | Managed file |
|-------|---------------|-------------|
| Claude Code, Gemini | `settings.json` lifecycle hooks | `<role>/.claude/settings.json` |
| OpenCode | JS plugin | `workDir/.opencode/plugins/gastown.js` |
| GitHub Copilot | JSON lifecycle hooks | `workDir/.github/hooks/gastown.json` |
| Codex, others | Startup nudge fallback | *(no file — nudge only)* |

> **GitHub Copilot note**: Copilot CLI supports full executable lifecycle hooks
> (`sessionStart`, `userPromptSubmitted`, `preToolUse`, `sessionEnd`) via
> `.github/hooks/gastown.json`. This is the same lifecycle coverage as Claude Code,
> delivered in Copilot's JSON format rather than Claude's `settings.json` format.
> The `gt hooks` commands below apply to Claude Code (and Gemini) only.

Gas Town manages `.claude/settings.json` files in gastown-managed parent directories
and passes them to Claude Code via the `--settings` flag. This keeps customer repos
clean while providing role-specific hook configuration. The hooks system provides
a single source of truth with a base config and per-role/per-rig overrides.

## Architecture

```
~/.gt/hooks-base.json              ← Shared base config (all agents)
~/.gt/hooks-overrides/
  ├── crew.json                    ← Override for all crew workers
  ├── witness.json                 ← Override for all witnesses
  ├── gastown__crew.json           ← Override for gastown crew specifically
  └── ...
```

**Merge strategy:** `base → role → rig+role` (more specific wins)

For a target like `gastown/crew`:
1. Start with base config
2. Apply `crew` override (if exists)
3. Apply `gastown/crew` override (if exists)

## Generated targets

Each rig generates settings in shared parent directories (not per-worktree):

| Target | Path | Override Key |
|--------|------|--------------|
| Crew (shared) | `<rig>/crew/.claude/settings.json` | `<rig>/crew` |
| Witness | `<rig>/witness/.claude/settings.json` | `<rig>/witness` |
| Refinery | `<rig>/refinery/.claude/settings.json` | `<rig>/refinery` |
| Polecats (shared) | `<rig>/polecats/.claude/settings.json` | `<rig>/polecats` |

Town-level targets:
- `mayor/.claude/settings.json` (key: `mayor`)
- `deacon/.claude/settings.json` (key: `deacon`)

Settings are passed to Claude Code via `--settings <path>`, which loads them as
a separate priority tier that merges additively with project settings.

## Commands

### `gt hooks sync`

Regenerate all `.claude/settings.json` files from base + overrides.
Preserves non-hooks fields (editorMode, enabledPlugins, etc.).

```bash
gt hooks sync             # Write all settings files
gt hooks sync --dry-run   # Preview changes without writing
```

### `gt hooks diff`

Show what `sync` would change, without writing anything.

```bash
gt hooks diff             # Show differences
gt hooks diff --no-color  # Plain output
```

### `gt hooks base`

Edit the shared base config in `$EDITOR`.

```bash
gt hooks base             # Open in editor
gt hooks base --show      # Print current base config
```

### `gt hooks override <target>`

Edit overrides for a specific role or rig+role.

```bash
gt hooks override crew              # Edit crew override
gt hooks override gastown/witness   # Edit gastown witness override
gt hooks override crew --show       # Print current override
```

### `gt hooks list`

Show all managed settings.local.json locations and their sync status.

```bash
gt hooks list             # Show all targets
gt hooks list --json      # Machine-readable output
```

### `gt hooks scan`

Scan the workspace for existing hooks (reads current settings files).

```bash
gt hooks scan             # List all hooks
gt hooks scan --verbose   # Show hook commands
gt hooks scan --json      # JSON output
```

### `gt hooks init`

Bootstrap base config from existing settings.local.json files. Analyzes all
current settings, extracts common hooks as the base, and creates overrides
for per-target differences.

```bash
gt hooks init             # Bootstrap base and overrides
gt hooks init --dry-run   # Preview what would be created
```

Only works when no base config exists yet. Use `gt hooks base` to edit
an existing base config.

### `gt hooks registry` / `gt hooks install`

Browse and install hooks from the registry.

```bash
gt hooks registry                  # List available hooks
gt hooks install <hook-id>         # Install a hook to base config
```

## Current Registry Hooks

The registry (`~/gt/hooks/registry.toml`) defines 7 hooks, 5 enabled by default:

| Hook | Event | Enabled | Roles |
|---|---|---|---|
| pr-workflow-guard | PreToolUse | Yes | crew, polecat |
| session-prime | SessionStart | Yes | all |
| pre-compact-prime | PreCompact | Yes | all |
| mail-check | UserPromptSubmit | Yes | all |
| costs-record | Stop | Yes | crew, polecat, witness, refinery |
| clone-guard | PreToolUse | No | crew, polecat |
| dangerous-command-guard | PreToolUse | Yes | crew, polecat |

Additional hooks exist in settings.json files but are not yet in the registry:

- **bd init guard** (gastown/crew, beads/crew) - blocks `bd init*` inside `.beads/`
- **mol patrol guards** (gastown roles) - blocks persistent patrol molecules
- **tmux clear-history** (gastown root) - clears terminal history on session start
- **SessionStart .beads/ validation** (gastown/crew, beads/crew) - validates CWD

## Design Decision: Registry as Catalog vs Source of Truth

> **Decision: The registry is a catalog, not the source of truth.**
>
> The registry (`registry.toml`) lists available hooks. The base/overrides system
> (`~/.gt/hooks-base.json` + `~/.gt/hooks-overrides/`) defines what is active.
> `gt hooks install` copies from the registry into the base/overrides config.
>
> This separation provides:
> - Per-machine customization (PATH differences across machines)
> - Per-role overrides without polluting the shared registry
> - Clear distinction between "what hooks exist" and "what hooks are active where"
>
> The registry is the menu. The base/overrides are the order.

## Known Gaps

1. **Registry doesn't cover all active hooks** — Several hooks in settings.json
   files are not in `registry.toml` (bd-init-guard, mol-patrol-guard, tmux-clear,
   cwd-validation). These should be added so `gt hooks install` can manage them.

2. **No `gt tap` commands beyond pr-workflow** — The tap framework has only one
   guard implemented. `gt tap guard dangerous-command` is referenced in the
   registry but does not exist yet. Priority order: dangerous-command, bd-init,
   mol-patrol, then audit git-push.

3. **No `gt tap disable/enable` convenience commands** — Per-worktree
   enable/disable is possible via the override mechanism (`gt hooks override`
   with empty hooks list), but there is no convenience wrapper yet.

4. **Private hooks (settings.local.json)** — Claude Code supports
   `settings.local.json` for personal overrides. Gas Town doesn't manage
   these yet. Low priority since Gas Town is primarily agent-operated.

5. **Hook ordering** — No action needed currently. The merge chain
   (base -> override) produces deterministic order, and per-matcher merge
   ensures one entry per event type.

## Rig pre-push guard

Everything above manages Claude Code hooks. The rig pre-push guard is a
**git** hook that Gas Town installs in every rig repository agents push from
(gt-pd1c.72). It exists because instructions alone did not stop an agent: in a
pilot the refinery merged nine MRs with a raw `git push origin temp-mr:<target>`
and once landed an epic on `main` without approval. Project repos have no
pre-push hook of their own, and the Gas Town repo's `.githooks/pre-push` only
guards the Gas Town repo itself.

### What is installed where

The hook file is a short `/bin/sh` shim (marker `gastown-managed-pre-push`,
code in `internal/pushguard`). It pipes git's pre-push input to
`gt hooks pre-push <remote> <url>` and, if that passes, runs the project's own
hook. If `gt` is not on `PATH` the push is refused.

| Repository | Hook file |
|---|---|
| `<rig>/.repo.git` (shared bare repo) | `.repo.git/hooks/pre-push`, which also serves `refinery/rig` and every polecat worktree |
| `<rig>/mayor/rig`, `<rig>/crew/<name>` (separate clones) | `<clone>/.git/hooks/pre-push` |

It is installed (idempotently; an up-to-date hook is never rewritten) by
`gt rig add`, `gt up` (all rigs, before agents start), polecat worktree
creation/repair/reuse and `gt crew add`, and checked by `gt doctor`
(`rig-prepush-guard`, fixable with `gt doctor --fix`). Installation problems are
warnings, never fatal.

**Project hooks are chained, never overwritten.**

- `core.hooksPath` unset and a project `pre-push` already present: it is kept
  as `pre-push.gt-chained` (hard link, so an existing chained file is never
  clobbered) and runs after the Gas Town check with the same arguments and
  stdin. A non-zero exit from either rejects the push. If both `pre-push`
  (foreign) and `pre-push.gt-chained` exist, the repo is reported as a
  conflict and left alone.
- `core.hooksPath` set (a project-owned, usually tracked, directory such as
  `.githooks`): Gas Town does not write there. The repo counts as guarded
  ("delegated") when that `pre-push` contains `gt hooks pre-push` and feeds it
  git's stdin, e.g. `printf '%s\n' "$input" | gt hooks pre-push "$@" || exit 1`;
  otherwise `gt doctor` reports a conflict. The Gas Town repo's own
  `.githooks/pre-push` delegates this way.

### Rules (`gt hooks pre-push`)

A caller is an **agent** when `GT_ROLE`, `BD_ACTOR`, `GT_POLECAT` or `GT_CREW`
is set (the same helper as the `gt mq integration land` gate). For the
coordinator rules (gt-pd1c.78) every identity variable that is set makes a
claim (`GT_ROLE` and `BD_ACTOR` parsed like `GT_ROLE`, `GT_POLECAT` = polecat,
`GT_CREW` = crew) and conflicts are read the strictest way: the caller keeps
the human column only when every claim is crew, or `BD_ACTOR=overseer` while
`GT_ROLE` and `GT_POLECAT` are empty (e.g. `GT_ROLE=<rig>/crew/x` with
`BD_ACTOR=<rig>/witness` is an agent). Otherwise the role is the agent role
the claims agree on; disagreeing agent claims make an unknown role. The bash
fallback classifies the same way (one env matrix tests both). All other rows
treat crew as an agent, as before.

| Push | Agent | Human |
|---|---|---|
| Push to `polecat/*`, other branches, tags | allowed, nothing checked | allowed |
| Delete `polecat/*` (agents other than crew), or rewrite it (not a fast-forward) when the agent is neither the refinery nor the polecat that owns the branch | the old remote tip is checked; allowed only when discarding it loses no work: the tip is preserved (ancestor, merge-tree no-op or every patch present, the `gt polecat prune` rule) on the remote default branch or a remote `integration/*` branch; or the merge queue (`MergeQueue.BranchHold`, the one predicate) shows no open MR from the branch and every MR from it definitely not landed (or none); or the deciding MR landed **by git proof** and its `commit_sha` is the tip (a landed verdict resting only on `close_reason`/`merge_commit` or a human `landing:` verdict is not trusted for discarding work). An MR closed by hand that git proves did not land counts as not landed, so its branch may be deleted (user decision). An open MR, a landing git cannot decide, landed work with later commits or an unreadable merge queue refuse. The git proof runs first, so the refinery and `gt mq post-merge` / `gt polecat prune --remote` (which delete only landed tips) never read beads | allowed |
| Update the default branch or `integration/*` by an agent other than the refinery or crew (witness, mayor, deacon, dog, boot, polecat, unknown role) | refused. A polecat may push there only when the rig's `merge_queue.enabled` is `false` (`gt done`, direct convoy strategy); any agent may create an `integration/*` branch whose tip is already on a remote target (`gt mq integration create`). The refinery and crew are unchanged | allowed |
| Default branch or `integration/*` from a repository that runs the managed hook but whose rig cannot be detected | refused (fail closed) | checked as below, without rig settings |
| Default branch or `integration/*` from a repository without the managed hook and outside any rig (e.g. the Gas Town source repo, whose `.githooks/pre-push` delegates) | checked as below without rig settings: auto-land off, no scope | same |
| Update of the default branch or `integration/*` that is not a fast-forward (`remote_sha` not an ancestor of `local_sha`, or unknown locally) | refused: rewinding `integration/x` onto the default branch would hide its old tip from the land check. No gt flow force-pushes these branches | not checked (git itself needs `-f`) |
| Delete the default branch | refused | allowed |
| Delete `integration/*` | allowed only when its tip is already contained in the default branch on the push remote (what `gt mq integration land` cleanup does); otherwise refused, so delete-then-merge cannot hide a land | allowed |
| Default branch or `integration/*` that newly contains the tip of another `integration/*` branch (a land) | refused when `merge_queue.integration_branch_auto_land` is not `true`, **even with `GT_INTEGRATION_LAND=1`**; with auto-land on, allowed with `GT_INTEGRATION_LAND=1` (set by `gt mq integration land`) | refused unless `GT_INTEGRATION_LAND=1` |
| Default branch or `integration/*` | every changed path in `remote_sha..local_sha` (new branch: merge-base of the remote default-branch tip, read with `ls-remote`, and `local_sha`) must match `merge_queue.allowed_paths`, when set | not scope-checked |

The rig is derived from the real (symlink-resolved) git common dir of the
repository, then the real working directory (`<town>/<rig>/...`, where the rig
directory has `config.json`); its settings are read straight from
`<rig>/settings/config.json`. The default branch is the rig's `default_branch`
(`<rig>/config.json`). When it is unknown, humans fall back to `origin/HEAD`
and agents treat `origin/HEAD`, `main` and `master` all as default branches.
Fail closed: an unreadable settings file means auto-land off, an unknown
scope (agent pushes to targets refused) and an active merge queue; a remote that cannot be listed, a
remote tip missing locally, an option-looking remote/URL, or a malformed input
line refuses the push. Refusals never print the push URL, and messages to
agents never suggest `GT_INTEGRATION_LAND`.

Land detection compares against `integration/*` tips on the push remote, the
local `integration/*` branches and the local
`refs/remotes/<remote>/integration/*` tracking refs, so a branch deleted on the
remote still counts while the pushing repository knows it. An integration
branch that a human deleted on the remote and that is gone locally too (no
branch, tracking ref pruned) is no longer recognised. Creating a branch (remote
sha all zeros) is never a land; it is still scope-checked.

### Limits

- A local hook is advisory against a determined caller: `git push --no-verify`,
  `git -c core.hooksPath=/dev/null push`, deleting the hook, or clearing the
  agent variables skip it. The only real guarantee is on the remote: branch
  protection or a pre-receive hook.
- `merge_queue.enabled` (which lets a polecat push to a target, direct
  strategy) lives in `<rig>/settings/config.json`, a file agents can edit;
  that is a known limit of a local guard.
- Pre-push input is validated: object names must be 40-64 lowercase hex
  digits (all zeros for "no commit"); anything else refuses the push.
- Land detection is ancestry-based. A squash merge, cherry-pick or rebase of
  integration commits onto the default branch creates new SHAs and is **not**
  detected as a land (it is only scope-checked). Mitigation: remote branch
  protection.
- `GT_INTEGRATION_LAND=1` is a policy marker, not proof that the push comes
  from `gt mq integration land`: with auto-land on, any agent that sets it can
  land through a raw push.
- With `allowed_paths` unset, the refinery and crew (and a polecat on a rig
  whose merge queue is disabled, for the direct convoy merge strategy) can push
  a non-land change straight to the default branch or `integration/*`. That the
  refinery pushes only merged MRs is not checked yet (gt-pd1c.78, stage 3).
- The polecat deletion rule sees only MRs with a `source_issue` (the merge
  queue index groups by source). The `close_reason`/`merge_commit` fields that
  make an MR landed can be edited with `bd update` (cross-check in stage 2).
- `gt polecat nuke --force` uses the same deletion rule for the polecat's
  remote and local tip, and also requires a clean worktree (no uncommitted or
  untracked non-ignored files, no stash), no active hook bead, no open work
  assigned and a readable agent bead (from the nuke safety checks). When that
  does not hold, an agent is refused and a human or crew must type the polecat
  addresses at a terminal. `gt polecat remove --force` is not gated.
- The Gas Town repo's own `.githooks/pre-push` fallback (no `gt` with
  `hooks pre-push` on `PATH`) cannot read the merge queue or rig settings, so
  it is stricter: agents other than crew may not delete `polecat/*`, agents
  other than crew, the refinery and the owning polecat may not rewrite it,
  and only the refinery, crew and humans may update the default branch or
  `integration/*`.
- Agents can no longer delete an unlanded `integration/*` branch (e.g. to
  abandon an epic); a human does that.

The hard boundary for a real project is the remote:
point the rig at a local mirror and protect the real origin's branches, with
promotion through a human-reviewed PR (see
`docs/enh-masesas/runbook-real-project.md`, section 2).

## Integration

### `gt rig add`

When a new rig is created, hooks are automatically synced for all the
new rig's targets (crew, witness, refinery, polecats).

### `gt doctor`

The `hooks-sync` check verifies all settings.local.json files match what
`gt hooks sync` would generate. Use `gt doctor --fix` to auto-fix
out-of-sync targets.

## Per-matcher merge semantics

When an override has the same matcher as a base entry, the override
**replaces** the base entry entirely. Different matchers are appended.
An override entry with an empty hooks list **removes** that matcher.

Example base:
```json
{
  "SessionStart": [
    { "matcher": "", "hooks": [{ "type": "command", "command": "gt prime" }] }
  ]
}
```

Override for witness:
```json
{
  "SessionStart": [
    { "matcher": "", "hooks": [{ "type": "command", "command": "gt prime --witness" }] }
  ]
}
```

Result: The witness gets `gt prime --witness` instead of `gt prime`
(same matcher = replace).

## Default base config

When no base config exists, the system uses sensible defaults:

- **SessionStart**: PATH setup + `gt prime --hook`
- **PreCompact**: PATH setup + `gt prime --hook`
- **UserPromptSubmit**: PATH setup + `gt mail check --inject`
- **Stop**: PATH setup + `gt costs record`
