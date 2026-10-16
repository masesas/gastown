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
is set (the same helper as the `gt mq integration land` gate).

| Push | Agent | Human |
|---|---|---|
| `polecat/*`, other branches, tags, deletions | allowed, nothing checked | allowed |
| Default branch or `integration/*` that newly contains the tip of another `integration/*` branch on the push remote (a land) | refused when `merge_queue.integration_branch_auto_land` is not `true`, **even with `GT_INTEGRATION_LAND=1`**; with auto-land on, allowed only from `gt mq integration land` (`GT_INTEGRATION_LAND=1`) | refused unless `GT_INTEGRATION_LAND=1` (set by `gt mq integration land`) |
| Default branch or `integration/*` | every changed path in `remote_sha..local_sha` (new branch: merge-base with the default branch `..local_sha`) must match `merge_queue.allowed_paths`, when set | not scope-checked |

The default branch is the rig's `default_branch` (`<rig>/config.json`),
falling back to `origin/HEAD`. The rig is derived from the directory
(`<town>/<rig>/...`) and its settings are read straight from
`<rig>/settings/config.json`. Fail closed: an unreadable settings file means
auto-land off and an unknown scope (agent pushes to targets refused); a
remote that cannot be listed, a remote tip missing locally, or a malformed
input line refuses the push. Refusals never print the push URL, and messages
to agents never suggest `GT_INTEGRATION_LAND`.

Land detection compares against integration branches that exist on the push
remote. Creating a branch (remote sha all zeros) is never a land; it is still
scope-checked. Cherry-picked integration commits (new SHAs) are not detected.

### Limits

A local hook is advisory against a determined caller: `git push --no-verify`,
`git -c core.hooksPath=/dev/null push`, deleting the hook, or clearing the
agent variables skip it. The hard boundary for a real project is the remote:
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
