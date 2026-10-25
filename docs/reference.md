# Gas Town Reference

Technical reference for Gas Town internals. Read the README first.

> For directory structure details, see [architecture.md](design/architecture.md).

## Beads Routing

Gas Town `gt` commands route beads work based on issue ID prefix. For direct
`bd` commands, run from the owning repository/root so the active `.beads`
directory matches the database you intend to touch.

```bash
bd -C ~/gt/greenplace/mayor/rig show gp-xyz  # Greenplace rig beads
bd -C ~/gt show hq-abc                       # Town-level beads
bd -C ~/gt/wyvern/mayor/rig show wyv-123     # Wyvern rig beads
```

**How it works**: Routes are defined in `~/gt/.beads/routes.jsonl`. Each rig's
prefix maps to its beads location (the mayor's clone in that rig).

| Prefix | Routes To | Purpose |
|--------|-----------|---------|
| `hq-*` | `~/gt/.beads/` | Mayor mail, cross-rig coordination |
| `gp-*` | `~/gt/greenplace/mayor/rig/.beads/` | Greenplace project issues |
| `wyv-*` | `~/gt/wyvern/mayor/rig/.beads/` | Wyvern project issues |

Debug routing: `BD_DEBUG_ROUTING=1 bd -C <owning-root> show <id>`

`bd --global` is not Gas Town's town database. In Beads it targets a separate
shared-server database named `beads_global`; run `bd -C ~/gt ...` for
town-level Gas Town beads.

## Configuration

### Rig Config (`config.json`)

```json
{
  "type": "rig",
  "name": "myproject",
  "git_url": "https://github.com/...",
  "default_branch": "main",
  "beads": { "prefix": "mp" }
}
```

**Rig config fields:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `default_branch` | `string` | `"main"` | Default branch for the rig. Auto-detected from remote during `gt rig add`. Used as the merge target by the Refinery and as the base for polecats when no integration branch is active. |
| `beads.mode` | `string` | `"auto"` | Where the rig keeps its beads (set by `gt rig add --beads`). `auto`: if the repo tracks `.beads/`, use it in `mayor/rig` (`<rig>/.beads/redirect` → `mayor/rig/.beads`), else a self-contained `<rig>/.beads/`. `rig`: always a self-contained `<rig>/.beads/`; Gas Town never initializes, rewrites or redirects into the repo's tracked `.beads/` (only an ignored `mayor/rig/.beads/redirect` → `../../.beads` is added). Missing or unknown values mean `auto`. |

### Settings (`settings/config.json`)

```json
{
  "theme": {
    "disabled": false,
    "name": "forest",
    "custom": {
      "bg": "#111111",
      "fg": "#eeeeee"
    },
    "role_themes": {
      "witness": "rust",
      "refinery": "plum",
      "crew": "none"
    }
  },
  "merge_queue": {
    "enabled": true,
    "run_tests": true,
    "setup_command": "",
    "typecheck_command": "",
    "lint_command": "",
    "test_command": "",
    "build_command": "",
    "on_conflict": "assign_back",
    "delete_merged_branches": true,
    "retry_flaky_tests": 1,
    "poll_interval": "30s",
    "max_concurrent": 1,
    "integration_branch_polecat_enabled": true,
    "integration_branch_refinery_enabled": true,
    "integration_branch_template": "integration/{title}",
    "integration_branch_auto_land": false
  }
}
```

**Theme fields:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `disabled` | `bool` | `false` | Disable tmux status/window theming for the rig |
| `name` | `string` | auto-assigned by rig name | Use a named built-in palette theme |
| `custom.bg` | `string` | unset | Custom tmux background color |
| `custom.fg` | `string` | unset | Custom tmux foreground color |
| `role_themes` | `map[string]string` | unset | Per-role overrides for `witness`, `refinery`, `crew`, `polecat`; use `"none"` to disable theming for a role |

Theme resolution:
- No `theme` config: auto-assign a built-in palette theme by rig name
- `disabled: true`: skip both `status-style` and `window-style`
- `name`: use that built-in theme
- `custom`: use exact `{bg, fg}` colors
- `role_themes`: override role-specific sessions within the rig

Town-level role defaults live in `mayor/config.json` under:

```json
{
  "theme": {
    "disabled": false,
    "name": "forest",
    "custom": {
      "bg": "#111111",
      "fg": "#eeeeee"
    },
    "role_defaults": {
      "mayor": "forest",
      "deacon": "plum",
      "witness": "rust",
      "crew": "none"
    }
  }
}
```

`role_defaults` supports `mayor`, `deacon`, `witness`, `refinery`, `crew`, and `polecat`.

**Merge queue fields:**

| Field | Type | Default | Description |
|-------|------|---------|-------------|
| `enabled` | `bool` | `true` | Whether the merge queue is active |
| `run_tests` | `bool` | `true` | Run tests before merging |
| `setup_command` | `string` | `""` | Setup/install command (e.g., `pnpm install`) |
| `typecheck_command` | `string` | `""` | Type check command (e.g., `tsc --noEmit`) |
| `lint_command` | `string` | `""` | Lint command (e.g., `eslint .`) |
| `test_command` | `string` | `""` | Test command to run. Empty = skip. |
| `build_command` | `string` | `""` | Build command (e.g., `go build ./...`) |
| `on_conflict` | `string` | `"assign_back"` | Conflict strategy: `assign_back` or `auto_rebase` |
| `delete_merged_branches` | `bool` | `true` | Delete source branches after merging |
| `retry_flaky_tests` | `int` | `1` | Number of times to retry flaky tests |
| `poll_interval` | `string` | `"30s"` | How often Refinery polls for new MRs |
| `max_concurrent` | `int` | `1` | Maximum concurrent merges |
| `integration_branch_polecat_enabled` | `*bool` | `true` | Polecats auto-source worktrees from integration branches |
| `integration_branch_refinery_enabled` | `*bool` | `true` | `gt done` / `gt mq submit` auto-target integration branches |
| `integration_branch_template` | `string` | `"integration/{title}"` | Branch name template (`{title}`, `{epic}`, `{prefix}`, `{user}`) |
| `integration_branch_auto_land` | `*bool` | `false` | Refinery patrol auto-lands when all children closed |
| `allowed_paths` | `[]string` | unset (unrestricted) | Repo paths an MR may change, as slash-separated globs (`*`, `?`, `[...]` per segment; `**` = zero or more segments), e.g. `["apps/**", "contracts/openapi.yaml"]`. `[]` or an absent key = unrestricted. Every path in the MR diff (merge-base of target..head, renames count on both sides) must match one entry. **Hard enforcement:** at submit time `gt done` / `gt mq submit` refuse the branch; at push time the Gas Town managed pre-push hook (`gt hooks pre-push`, see `docs/HOOKS.md` "Rig pre-push guard") refuses an agent push to the default branch or `integration/*` whose range `remote_sha..local_sha` leaves the scope, which also stops a refinery that bypasses the formula (pilot: `git push origin temp-mr:<target>` plus a raw `bd close`). Local hooks can be skipped by the pusher (`--no-verify`); remote branch protection is the hard boundary. At merge time `gt mq merge` (the refinery merge engine) checks the pinned `commit_sha` against the target tip before it merges anything (submodule commits included) and never merges or pushes a violation (`scope_violation`, FIX_NEEDED to the polecat); an unreadable scope fails closed. The refinery formula has no separate scope step: `gt mq merge` is its only merge command, and `gt mq check-scope` remains a read-only diagnostic. The gate reads only this key, so an unrelated invalid setting does not block submission; unparseable JSON or an invalid pattern fails closed. An invalid pattern also makes the whole settings load fail for every other caller (most of them then behave as if the rig had no settings). Paths are judged by their tree path: a symlink inside an allowed directory that points outside it is allowed (design limit). |

**Refinery merge engine (`gt mq merge`):** the engine reads `merge_queue` from
`<rig>/config.json` first and then layers this file (`<rig>/settings/config.json`)
on top: here `merge_strategy`, `vcs_provider`, `require_review`,
`delete_merged_branches` and `run_tests` win over `config.json`. The
`setup_command`, `typecheck_command`, `lint_command`, `build_command` and
`test_command` become the engine's gates, run on the merged tree before push:
`setup` first (pre-merge group), then `typecheck`, `lint`, `build`, `test` in
that order (post-squash group, sequential); blank commands are skipped. An
explicit `merge_queue.gates` map in `<rig>/config.json` wins over the
generated gates (the engine logs it); its gates may set `"order"` (default 0;
gates run by order, then name). `run_tests: false` is the explicit opt-out: it
disables every gate, an explicit `gates` map included, and the merge reports a
warning. With `run_tests` on and no gate at all, `gt mq merge` refuses
(`precondition`). A failed gate always blocks the merge. Gates are
branch-controlled code: they run without Gas Town identity or credentials in
their environment (`GT_*`, `BD_*`, `BEADS_*`, `GIT_*`, `GH_*`/`GITHUB_*`, cloud
and model API keys, `SSH_AUTH_SOCK`, and any variable whose name contains
`TOKEN`, `SECRET`, `PASSWORD`, `API_KEY`, `CREDENTIAL`, ...), and a gate that
moves HEAD (commit, amend, reset) fails the merge (`gates_failed`). The scrub
only removes environment variables: gates still run as the refinery's OS user,
with its filesystem access and whatever its git credential helper, `~/.ssh`
keys or other on-disk credentials allow, so a gate can still push to a remote
or edit `.git` itself. The engine only guarantees what *it* pushes (the
verified merge, by SHA); the real boundary is the stage 4 pre-receive hook.
Gates that need `NPM_TOKEN`, a private-registry token, `SSH_AUTH_SOCK` or
similar variables fail, because those are scrubbed. An unreadable settings
file stops the merge (fail closed).

See [Integration Branches](concepts/integration-branches.md) for integration branch details.

### Runtime (`.runtime/` - gitignored)

Process state, PIDs, ephemeral data.

### Rig-Level Configuration

Rigs support layered configuration through:
1. **Wisp layer** (`.beads-wisp/config/`) - transient, local overrides
2. **Rig identity bead labels** - persistent rig settings
3. **Town defaults** (`~/gt/settings/config.json`)
4. **System defaults** - compiled-in fallbacks

#### Polecat Branch Naming

Configure custom branch name templates for polecats:

```bash
# Set via wisp (transient - for testing)
echo '{"polecat_branch_template": "adam/{year}/{month}/{description}"}' > \
  ~/gt/.beads-wisp/config/myrig.json

# Or set via rig identity bead labels (persistent)
bd update gt-rig-myrig --labels="polecat_branch_template:adam/{year}/{month}/{description}"
```

**Template Variables:**

| Variable | Description | Example |
|----------|-------------|---------|
| `{user}` | From `git config user.name` | `adam` |
| `{year}` | Current year (YY format) | `26` |
| `{month}` | Current month (MM format) | `01` |
| `{name}` | Polecat name | `alpha` |
| `{issue}` | Issue ID without prefix | `123` (from `gt-123`) |
| `{description}` | Sanitized issue title | `fix-auth-bug` |
| `{timestamp}` | Unique timestamp | `1ks7f9a` |

**Default Behavior (backward compatible):**

When `polecat_branch_template` is empty or not set:
- With issue: `polecat/{name}/{issue}+{timestamp}`
- Without issue: `polecat/{name}-{timestamp}`

**Example Configurations:**

```bash
# GitHub enterprise format
"adam/{year}/{month}/{description}"

# Simple feature branches
"feature/{issue}"

# Include polecat name for clarity
"work/{name}/{issue}"
```

## Formula Format

```toml
formula = "name"
type = "workflow"           # workflow | expansion | aspect
version = 1
description = "..."

[vars.feature]
description = "..."
required = true

[[steps]]
id = "step-id"
title = "{{feature}}"
description = "..."
needs = ["other-step"]      # Dependencies
```

**Composition:**

```toml
extends = ["base-formula"]

[compose]
aspects = ["cross-cutting"]

[[compose.expand]]
target = "step-id"
with = "macro-formula"
```

## Molecule Lifecycle

> For the full lifecycle diagram and detailed command reference, see [concepts/molecules.md](concepts/molecules.md).

**Summary**: Formula (TOML) --`bd cook`--> Protomolecule --`bd mol pour`--> Mol (persistent) or Wisp (ephemeral) --`bd squash`--> Digest.

| Operation | bd (data) | gt (agent) |
|-----------|-----------|------------|
| Cook/pour/wisp | `bd cook`, `bd mol pour/wisp` | — |
| Squash/burn | `bd mol squash/burn <id>` | `gt mol squash/burn` (attached) |
| Navigate | `bd mol current`, `bd mol show` | `gt hook`, `gt mol current` |
| Attach | — | `gt mol attach/detach` |

## Agent Lifecycle

### Polecat Shutdown

```
1. Work through formula checklist (shown inline by gt prime)
2. Submit to merge queue via gt done
3. gt done preserves branch/MR metadata, closes the polecat's molecule, parks the
   issue for the merge queue (in_progress, no assignee) and exits the session
4. Refinery closes the issue when the MR merges (closed_at = merge time) and
   reopens it when the MR is rejected; without an MR gt done closes it itself
5. Witness/refinery cleanup handles any retired sandbox state
```

`gt done` records the worktree's git state on the polecat's agent bead as
`cleanup_status` (`clean`, `has_uncommitted`, `has_stash`, `has_unpushed`,
`unknown`). gt computes it from git; there is no flag to supply it. The value is
a hint for reports only: `gt polecat nuke`, `gt polecat remove`, worktree repair
and slot reuse check the worktree's git state themselves when they decide
(commits must be proven on the remote), and refuse without `--force` when that
state cannot be read or the directory is not the root of its own checkout
(a worktree whose `.git` is gone is never judged by an enclosing repository).
`gt polecat stale --cleanup` applies the same checks as `gt polecat nuke`.

### Session Cycling

```
1. Agent notices context filling
2. gt handoff (sends mail to self)
3. Manager kills session
4. Manager starts new session
5. New session reads handoff mail
```

## Environment Variables

Gas Town sets environment variables for each agent session via `config.AgentEnv()`.
These are set in tmux session environment when agents are spawned.

### Core Variables (All Agents)

| Variable | Purpose | Example |
|----------|---------|---------|
| `GT_ROLE` | Agent role type | `mayor`, `witness`, `polecat`, `crew` |
| `GT_ROOT` | Town root directory | `/home/user/gt` |
| `BD_ACTOR` | Agent identity for attribution | `gastown/polecats/toast` |
| `GIT_AUTHOR_NAME` | Commit attribution (same as BD_ACTOR) | `gastown/polecats/toast` |
| `BEADS_DIR` | Beads database location | `/home/user/gt/gastown/.beads` |

### Rig-Level Variables

| Variable | Purpose | Roles |
|----------|---------|-------|
| `GT_RIG` | Rig name | witness, refinery, polecat, crew |
| `GT_POLECAT` | Polecat worker name | polecat only |
| `GT_CREW` | Crew worker name | crew only |
| `BEADS_AGENT_NAME` | Agent name for beads operations | polecat, crew |

### Other Variables

| Variable | Purpose |
|----------|---------|
| `GIT_AUTHOR_EMAIL` | Workspace owner email (from git config) |
| `GT_TOWN_ROOT` | Override town root detection (manual use) |
| `CLAUDE_RUNTIME_CONFIG_DIR` | Custom Claude settings directory |

### Environment by Role

| Role | Key Variables |
|------|---------------|
| **Mayor** | `GT_ROLE=mayor`, `BD_ACTOR=mayor` |
| **Deacon** | `GT_ROLE=deacon`, `BD_ACTOR=deacon` |
| **Boot** | `GT_ROLE=deacon/boot`, `BD_ACTOR=deacon-boot` |
| **Witness** | `GT_ROLE=witness`, `GT_RIG=<rig>`, `BD_ACTOR=<rig>/witness` |
| **Refinery** | `GT_ROLE=refinery`, `GT_RIG=<rig>`, `BD_ACTOR=<rig>/refinery` |
| **Polecat** | `GT_ROLE=polecat`, `GT_RIG=<rig>`, `GT_POLECAT=<name>`, `BD_ACTOR=<rig>/polecats/<name>` |
| **Crew** | `GT_ROLE=crew`, `GT_RIG=<rig>`, `GT_CREW=<name>`, `BD_ACTOR=<rig>/crew/<name>` |

### Doctor Check

The `gt doctor` command verifies that running tmux sessions have correct
environment variables. Mismatches are reported as warnings:

```
⚠ env-vars: Found 3 env var mismatch(es) across 1 session(s)
    hq-mayor: missing GT_ROOT (expected "/home/user/gt")
```

Fix by restarting sessions: `gt shutdown && gt up`

## Agent Working Directories and Settings

Each agent runs in a specific working directory and has its own Claude settings.
Understanding this hierarchy is essential for proper configuration.

### Working Directories by Role

| Role | Working Directory | Notes |
|------|-------------------|-------|
| **Mayor** | `~/gt/mayor/` | Town-level coordinator, isolated from rigs |
| **Deacon** | `~/gt/deacon/` | Background supervisor daemon |
| **Witness** | `~/gt/<rig>/witness/` | No git clone, monitors polecats only |
| **Refinery** | `~/gt/<rig>/refinery/rig/` | Worktree on main branch |
| **Crew** | `~/gt/<rig>/crew/<name>/rig/` | Persistent human workspace clone |
| **Polecat** | `~/gt/<rig>/polecats/<name>/rig/` | Polecat worktree (ephemeral sandbox) |

Note: The per-rig `<rig>/mayor/rig/` directory is NOT a working directory—it's
a git clone that holds the canonical `.beads/` database for that rig.

### Settings File Locations

Settings are installed in gastown-managed parent directories and passed to
Claude Code via the `--settings` flag. This keeps customer repos clean:

```
~/gt/
├── mayor/.claude/settings.json              # Mayor settings (cwd = settings dir)
├── deacon/.claude/settings.json             # Deacon settings (cwd = settings dir)
└── <rig>/
    ├── crew/.claude/settings.json           # Shared by all crew members
    ├── polecats/.claude/settings.json       # Shared by all polecats
    ├── witness/.claude/settings.json        # Witness settings
    └── refinery/.claude/settings.json       # Refinery settings
```

The `--settings` flag loads these as a separate priority tier that merges
additively with any project-level settings in the customer repo.

### CLAUDE.md

Only `~/gt/CLAUDE.md` exists on disk — a minimal identity anchor that prevents
agents from losing their Gas Town identity after context compaction or new sessions.

Full role context (~300-500 lines per role) is injected ephemerally by `gt prime`
via the SessionStart hook. No per-directory CLAUDE.md or AGENTS.md files are created.

**Why no per-directory files?**
- Claude Code traverses upward from CWD for CLAUDE.md — all agents under `~/gt/` find the town-root file
- AGENTS.md (for Codex) uses downward traversal from git root — parent directories are invisible, so per-directory AGENTS.md never worked
- The real context comes from `gt prime`, making on-disk bootstrap pointers redundant

### Customer Repo Files (CLAUDE.md and .claude/)

Gas Town no longer uses git sparse checkout to hide customer repo files. Customer
repositories can have their own `.claude/` directory and `CLAUDE.md` — these are
preserved in all worktrees (crew, polecats, refinery, mayor/rig).

Gas Town's context comes from the town-root `CLAUDE.md` identity anchor
(picked up by all agents via Claude Code's upward directory traversal),
`gt prime` via the SessionStart hook, and the customer repo's own `CLAUDE.md`.
These coexist safely because:

- **`--settings` flag provides Gas Town settings** as a separate tier that merges
  additively with customer project settings, so both coexist cleanly
- **`gt prime` injects role context** ephemerally via SessionStart hook, which is
  additive with the customer's `CLAUDE.md` — both are loaded
- Gas Town settings live in parent directories (not in customer repos), so
  customer `.claude/` files are fully preserved

**Doctor check**: `gt doctor` warns if legacy sparse checkout is still configured.
Run `gt doctor --fix` to remove it. Tracked `settings.json` files in worktrees are
recognized as customer project config and are not flagged as stale.

### Settings Inheritance

Claude Code's settings are layered from multiple sources:

1. `.claude/settings.json` in current working directory (customer project)
2. `.claude/settings.json` in parent directories (traversing up)
3. `~/.claude/settings.json` (user global settings)
4. `--settings <path>` flag (loaded as a separate additive tier)

Gas Town uses the `--settings` flag to inject role-specific settings from
gastown-managed parent directories. This merges additively with customer
project settings rather than overriding them.

### Settings Templates

Gas Town uses two settings templates based on role type:

| Type | Roles | Key Difference |
|------|-------|----------------|
| **Interactive** | Mayor, Crew | Mail injected on `UserPromptSubmit` hook |
| **Autonomous** | Polecat, Witness, Refinery, Deacon | Mail injected on `SessionStart` hook |

Autonomous agents may start without user input, so they need mail checked
at session start. Interactive agents wait for user prompts.

### Troubleshooting

| Problem | Solution |
|---------|----------|
| Agent using wrong settings | Check `gt doctor`, verify `.claude/settings.json` in role parent dir |
| Settings not found | Run `gt install` to recreate settings, or `gt doctor --fix` |
| Source repo settings leaking | Run `gt doctor --fix` to remove legacy sparse checkout |
| Mayor settings affecting polecats | Mayor should run in `mayor/`, not town root |

## CLI Reference

### Town Management

```bash
gt install [path]            # Create town
gt install --git             # With git init
gt doctor                    # Health check
gt doctor --fix              # Auto-repair
```

### Configuration

```bash
# Agent management
gt config agent list [--json]     # List all agents (built-in + custom)
gt config agent get <name>        # Show agent configuration
gt config agent set <name> <cmd>  # Create or update custom agent
gt config agent remove <name>     # Remove custom agent (built-ins protected)

# Default agent
gt config default-agent [name]    # Get or set town default agent
```

**Built-in agents**: `claude`, `gemini`, `codex`, `kiro`, `cursor`, `auggie`, `amp`, `opencode`, `copilot`

The `kiro` preset launches `kiro-cli chat --trust-all-tools` and uses Kiro's
documented `--resume` / `--resume-id` session flags. Gas Town does not install
Kiro hooks or `.kiro` project files for this preset.

> **Note on GitHub Copilot**: The `copilot` preset uses executable lifecycle hooks in
> `.github/hooks/gastown.json` (`sessionStart`, `userPromptSubmitted`, `preToolUse`,
> `sessionEnd`) — the same lifecycle events as Claude Code, in Copilot's JSON format.
> Copilot uses a 5-second ready delay instead of prompt-based detection. Requires a
> Copilot seat and org-level CLI policy enabled.

**Custom agents**: Define per-town via CLI or JSON:
```bash
gt config agent set claude-glm "claude-glm --model glm-4"
gt config agent set claude "claude-opus"  # Override built-in
gt config default-agent claude-glm       # Set default
```

**Advanced agent config** (`settings/agents.json`):
```json
{
  "version": 1,
  "agents": {
    "opencode": {
      "command": "opencode",
      "args": [],
      "resume_flag": "--session",
      "resume_style": "flag",
      "non_interactive": {
        "subcommand": "run",
        "output_flag": "--format json"
      }
    }
  }
}
```

**Rig-level agents** (`<rig>/settings/config.json`):
```json
{
  "type": "rig-settings",
  "version": 1,
  "agent": "opencode",
  "agents": {
    "opencode": {
      "command": "opencode",
      "args": ["--session"]
    }
  }
}
```

**ACP-enabled custom agents** (`settings/config.json`):
```json
{
  "type": "town-settings",
  "version": 1,
  "default_agent": "opencode-acp-debug",
  "agents": {
    "opencode-acp-debug": {
      "command": "opencode",
      "acp": {
        "command": "acp",
        "args": ["--debug", "--print-logs"]
      }
    }
  }
}
```

The `acp` field configures Agent Communication Protocol support:
- `command`: ACP subcommand (e.g., `"acp"` for `opencode acp`)
- `args`: Additional arguments passed to the ACP subcommand

Custom agents inherit ACP support from their base command's preset. For example,
a custom agent with `"command": "opencode"` automatically inherits ACP support
from the opencode preset. You can override or extend the ACP args by specifying
the `acp` field explicitly.

**Agent resolution order**: rig-level → town-level → built-in presets.

For OpenCode autonomous mode, set env var in your shell profile:
```bash
export OPENCODE_PERMISSION='{"*":"allow"}'
```

### Rig Management

```bash
gt rig add <name> <url>
gt rig add <name> <url> --beads rig     # Self-contained <rig>/.beads/, never touch the repo's tracked .beads/
gt rig list
gt rig remove <name>
```

### Convoy Management (Primary Dashboard)

```bash
gt convoy list                          # Dashboard of active convoys
gt convoy status [convoy-id]            # Show progress (🚚 hq-cv-*)
gt convoy create "name" [issues...]     # Create convoy tracking issues
gt convoy create "name" gt-a bd-b --notify mayor/  # With notification
gt convoy list --all                    # Include landed convoys
gt convoy list --status=closed          # Only landed convoys
```

Note: "Swarm" is ephemeral (workers on a convoy's issues). See [Convoys](concepts/convoy.md).

### Work Assignment

```bash
# Standard workflow: convoy first, then sling
gt convoy create "Feature X" gt-abc gt-def
gt sling gt-abc <rig>                    # Assign to polecat
gt sling gt-abc <rig> --agent codex      # Override runtime for this sling/spawn
gt sling <proto> --on gt-def <rig>       # With workflow template

# Quick sling (auto-creates convoy)
gt sling <bead> <rig>                    # Auto-convoy for dashboard visibility
```

Review-only work (`--review-only`): the assignee evaluates and reports back, and
must not commit, push or open a merge request. The mode is stored as the
`review_only: true` field on the bead and is sticky: a later `gt sling` of the
same bead without the flag (including `--force`, scheduler dispatch, convoy
re-dispatch, or after the previous polecat was nuked) keeps it and refreshes the
assignment timestamp; detach and burn keep it too. A sling that fails (hook or
session start) rolls back to the bead's state before that sling: when the bead
was not review-only before, the rollback removes the marker it added; when it
was, or when the prior state is unknown, or when the rollback write itself
fails, the marker stays (fail closed).
`gt prime` shows a REVIEW-ONLY directive. `gt done` and `gt mq submit` refuse
commits and merge requests when the target bead (`--issue` or the branch) or
the caller's active assignment is review-only. The active assignment is picked
as `gt done` picks its hook: a `hooked` bead wins over `in_progress` ones (so a
claimed review leg still counts, and a stale `in_progress` leg does not override
the current hook); if the pick is ambiguous, every candidate counts. A lookup
failure, or a polecat session whose identity cannot be resolved, refuses too.
`gt done` never auto-commits leftover files of review-only work; complete with
a comment by the assignee, e.g.
`bd comments add <bead> "FINDINGS: head_sha=$(git rev-parse HEAD) <findings>"`
(`head_sha`/`target_head_sha` may appear anywhere in a `REPORT:`/`FINDINGS:`
line; trailing punctuation after the SHA is ignored), and no new commits.
Commits on a review-only branch are never pushed to preserve them: `gt polecat
nuke`, polecat worktree removal and `gt doctor --fix` skip the push, and the
checkpoint dog makes no WIP commit on review-only work. The branch bead and the
polecat's hooked bead are both checked. When that status cannot be read, the
push (and checkpoint) is skipped too, but nuke and stale-branch cleanup keep the
local branch so its commits survive in the rig repo, and `gt doctor --fix`
reports the push as deferred.

Where the marker counts: attachment fields (`review_only`, `no_merge`,
`attached_*`, `dispatched_by`, `convoy_id`, `merge_strategy`, `mode`,
`formula_vars`, ...) are read from the description's header block. That is the
first paragraph, outside fenced code blocks and not indented as code, whose
first line is a field with a strong attachment key: any key except `mode` and
`convoy`, which are ordinary words a prose line may start with (both are still
read inside the header). The header ends at the first blank line or Markdown
block (code fence, `>` quote, `#` heading, list item, table row). Inside it the
first occurrence of a key wins. `gt sling` writes the fields at the top of the
description; free text above them is tolerated.
`review_only` fails closed beyond the header: a `review_only` line also counts
in any later field paragraph (a paragraph outside fences whose first line has
an attachment key). Other keys there are not read. A `review_only` line in a
code block, a quote, a list item or a prose paragraph does not mark the bead.
`gt sling`, `gt done` and `gt mq submit` warn when such a line is ignored. Use
`gt sling --review-only` to mark a bead.
`attached_args` is written as a one-line JSON string (`attached_args: "..."`),
like `formula_vars` (JSON array). Older gt wrote `attached_args` and
`formula_vars` raw, possibly over several lines (blank lines, lists, fences
included). For such a raw value, the header continues at the first later line
that has a key the old writer put after it, provided that line starts a run of
field lines up to a blank line or the end. That is how the `no_merge` /
`review_only` lines after a multi-line value are found. A `review_only` line in
a further such run counts too (fail closed). Rewriting a bead consolidates its
fields into the header, and `Set(Parse(description))` is stable after one pass.

Without a paragraph that starts with a strong key, the first paragraph is the
header when every line in it is an attachment field (e.g. only `mode: ralph`).
Clearing (below) repeats until no `review_only` line counts.
Known edge cases of this format:
- A raw (non-JSON) `attached_args` from older gt can absorb a final body
  paragraph made only of field lines whose keys come later in the old writer
  order.
- A raw `attached_args` that happens to be a valid JSON string (`"hello"`) is
  decoded (`hello`) and does not continue.
- A gt binary from before this change shows new `attached_args` values as
  quoted JSON.
- A byte order mark (BOM) at the start of a line is not recognised; that line
  is not a field.

Values: only `true` and `false` (any case) are canonical. Any other value
(`no`, `off`, `0`, `f`, `maybe`, or an empty value) still marks the bead
review-only (fail closed). Before gt-pd1c.75, `0`/`f` meant false. The value is
kept exactly as written: no tool turns it into `true` or drops it. `gt sling`,
`gt done` and `gt mq submit` warn about it; fix the value, or clear the marker.

Clearing: `gt bead clear-review-only <bead> --reason "<why>"` is the one
explicit way to remove the marker. Only a human (overseer) or crew may run it,
from an interactive terminal, typing the bead ID again to confirm. Every agent
is refused, the mayor included; an agent that thinks a marker is wrong
escalates by mail. The command writes a bead comment
`review_only clear requested by <who> at <time>: <why>` before changing the
description. If that comment cannot be written, the marker is kept. After the
change is verified by re-reading the bead, it adds
`review_only cleared by <who> at <time>: <why>` and emits a
`review_only_cleared` event (best-effort). `--reason` is one line (line breaks
folded, URL credentials masked), at most 500 bytes, without control
characters. A bead that is not review-only is left as it is, without a prompt.
These are local guards against agents doing this by mistake, not an
authentication boundary: anyone who can run `bd update` on the bead can still
edit the description directly.

`no_merge` vs `review_only`: `no_merge` (`--no-merge`) is an assignment field,
like `attached_molecule`: detaching or burning the attached molecule (as a
formula re-sling does) clears it with the rest of the assignment, a failed
sling restores its value from before that sling, and no dispatch path adds it
back unless `--no-merge` is passed again. `review_only` belongs to the bead:
detach, burn and every later assignment keep it, with or without the flag,
until it is cleared with `gt bead clear-review-only` (or by editing the
description). The reason: no_merge is a delivery choice per dispatch (e.g. this
time a human opens the PR), while review-only is a property of the work itself
(a review leg must never commit), so a dispatcher forgetting the flag must not
turn a review leg into normal work.

Agent overrides:

- `gt start --agent <alias>` overrides the Mayor/Deacon runtime for this launch.
- `gt mayor start|attach|restart --agent <alias>` and `gt deacon start|attach|restart --agent <alias>` do the same.
- `gt start crew <name> --agent <alias>` and `gt crew at <name> --agent <alias>` override the crew worker runtime.

### Communication

```bash
gt mail inbox
gt mail read <id>
gt mail send <addr> -s "Subject" -m "Body"
gt mail send --human -s "..."    # To overseer
```

### Escalation

```bash
gt escalate "topic"              # Default: MEDIUM severity
gt escalate -s CRITICAL "msg"    # Urgent, immediate attention
gt escalate -s HIGH "msg"        # Important blocker
gt escalate -s MEDIUM "msg" -m "Details..."
```

See [escalation.md](design/escalation.md) for full protocol.

### Sessions

```bash
gt handoff                   # Request cycle (context-aware)
gt handoff --shutdown        # Terminate (polecats)
gt session stop <rig>/<agent>
gt peek <agent>              # Check health
gt nudge <agent> "message"   # Send message to agent
gt seance                    # List discoverable predecessor sessions
gt seance --talk <id>        # Talk to predecessor (full context)
gt seance --talk <id> -p "Where is X?"  # One-shot question
```

**Session Discovery**: Each session has a startup nudge that becomes searchable
in Claude's `/resume` picker:

```
[GAS TOWN] recipient <- sender • timestamp • topic[:mol-id]
```

Example: `[GAS TOWN] gastown/crew/gus <- human • 2025-12-30T15:42 • restart`

**IMPORTANT**: Always use `gt nudge` to send messages to Claude sessions.
Never use raw `tmux send-keys` - it doesn't handle Claude's input correctly.
`gt nudge` uses literal mode + debounce + separate Enter for reliable delivery.

### Emergency

```bash
gt stop --all                # Kill all sessions
gt stop --rig <name>         # Kill rig sessions
```

### Health Check

```bash
gt deacon health-check <agent>   # Send health check ping, track response
gt deacon health-state           # Show health check state for all agents
```

### Merge Queue (MQ)

```bash
gt mq list [rig]             # Show the merge queue
gt mq next [rig]             # Show highest-priority merge request
gt mq submit                 # Submit current branch to merge queue
gt mq status <id>            # Show detailed merge request status
gt mq retry <id>             # Retry a failed merge request
gt mq reject <id>            # Reject a merge request
gt mq reject <rig> <mr> --confirm-not-landed  # Overseer verdict: closed MR did not land (releases an unverified hold)
gt mq post-merge <rig> <mr> --confirm-landed  # Overseer verdict: closed MR landed (closes its issue)
gt mq merge <rig> <mr-id> [--json]  # Refinery: merge one MR end to end (merge, gates, push, close, clean up, MERGED)
gt mq post-merge <rig> <mr-id> [--skip-branch-delete]  # Finish an MR gt mq merge closed as merged (after cleanup_failed)
gt mq check-scope <rig> <mr-id>  # Verify the MR only changes merge_queue.allowed_paths
gt mq check-scope --range <old-sha> <new-sha> [--rig <rig>]  # Same check on the two-dot diff old..new
gt hooks pre-push <remote> [<url>]  # Git pre-push guard (stdin = git pre-push lines); run by the managed rig hook
```

`gt mq merge <rig> <mr-id>` is the refinery's single Go merge path
(gt-pd1c.78 tahap 3). One run at a time per rig: it takes the exclusive lock
`<rig>/.runtime/locks/refinery-merge.lock` (created mode 0600) without
waiting; a second run is refused (`precondition`), and the engine's batch path
takes the same lock. It refuses an MR that is not an open
`gt:merge-request` of the rig, is blocked, is claimed by anyone but the rig's
refinery (exactly `<rig>/refinery`, or one of its runs
`<rig>/refinery/merge-<pid>-<n>` whose process no longer exists on this host
or is this process; a run whose pid is alive, or cannot be parsed, is never
taken over), records a `close_reason` other than `merged`, has
a `source_issue` that is not a plain bead id, a branch that is not a plain
branch name, no full `commit_sha`, or a target that is not the rig default
branch or `integration/*`. It claims the MR for this run
(`<rig>/refinery/merge-<pid>-<n>`) and reads the claim back: if another writer
won, it stops and leaves that claim alone. Every bead write of the run is made
as that claimant (bd actor `BEADS_ACTOR=<claimant>`): bd refuses to close a
bead whose assignee is not the actor ("cannot close ...: assignee is ...,
actor is ..."), so a run acting as the caller (a human's git user, or
`<rig>/refinery`) could never close the MR it claimed. bd's audit trail
therefore names the run claimant as the writer. It then refuses a
refinery worktree with an unfinished merge/rebase/cherry-pick/revert, any
uncommitted or untracked file, or a local target with commits missing on
`origin/<target>` (they are never pushed; a local target that is only behind
is fast-forwarded). A branch missing locally but on origin at exactly
`commit_sha` is created there (after the landing check below). Before
merging it asks git whether `commit_sha` is already on the trusted target
(the same proof as after a push). If so, the run finalizes without merging,
gating or pushing again (`"already_landed": true`) only when the MR records
`merge_commit` M, git proves M on the target, and M has exactly two parents
with `commit_sha` as the second (the merge the engine pushed; it records M on
the MR right after the push, before any finalize step that can fail). A
`commit_sha` already on the target without such a merge (e.g. an MR whose
`commit_sha` is some commit already on main) is refused `not_eligible`: it is
not merged again and nothing is closed or mailed; resolve it by hand. An MR
that records `close_reason: merged` without proof that `commit_sha` landed is
refused (`not_eligible`, or `error` when git cannot decide) and nothing is
closed. When git cannot decide for an MR without that verdict
(e.g. a shallow clone whose boundary hides the branch base) the merge
proceeds: the push check below refuses a merge that adds nothing. It then rechecks eligibility (an MR whose bead
says `close_reason: merged` is closed only when git proves its `commit_sha` is
on the trusted target; otherwise nothing is closed), runs the merge-scope
gate, merges `commit_sha` with a merge commit, runs every gate on the merged
tree, verifies that HEAD is still that merge (first parent the pre-merge
target tip, second parent `commit_sha`, the same tree) and pushes exactly that
commit by SHA (`<sha>:refs/heads/<target>`); after the push it proves from git that the target
contains `commit_sha` (the trusted default branch is `default_branch` in
`<rig>/config.json`, else the remote's own HEAD, never the local `origin/HEAD`),
closes the MR `merged` and the source issue `Merged in <mr>` (the reason that
unblocks `merge-blocks` dependents; any other reason is refused on this path),
deletes the branch on origin and locally at `commit_sha` (polecat branches;
others only locally and only with `delete_merged_branches`; a branch name that
is not plain is never handed to git; the default branch and `integration/*`
are never deleted), mails `MERGED` to `<rig>/witness` and
nudges the mayor. On a conflict it files a conflict-resolution task that blocks
the MR; on a failed gate or scope violation it mails `FIX_NEEDED` to
`<rig>/polecats/<worker>` (`<rig>/witness` when the worker is not a plain
name), once per submitted `commit_sha` and failure type (the MR records
`fix_needed_for: <commit_sha>:<type>` after the send; a retry with the same
failure mails nothing, a different failure on the same commit mails again).
`fix_needed_for` is an ordinary bead field any agent can write: it only
suppresses repeat mail and gates nothing. The error text is one
line without control characters, at most 500 bytes, and labeled
`[untrusted branch output]`. Every outcome that leaves the MR open releases
the claim. `merge_queue.auto_push: false` is refused. Only the refinery or
crew of `<rig>`, or a human, may run it: other agent roles, and a refinery or
crew whose `GT_RIG`/`GT_ROLE`/`BD_ACTOR` names another rig (or no rig), are
refused. This role gate reads the caller's environment, so it stops agents
taking the wrong exit by mistake; it is **not an authentication boundary**
(stage 4 binds the pushing process through the pre-receive hook). Rig names
compare case-insensitively, as everywhere in the merge engine. Exit status
is 0 only for `merged`. With `--json` stdout is always one JSON object (engine
progress goes to stderr):

```json
{"kind": "merged", "mr_id": "gt-mr-abc", "branch": "polecat/nux/gt-1", "target": "main",
 "source_issue": "gt-1", "commit_sha": "<sha>", "merge_commit": "<sha>", "gates_ran": 2,
 "branch_cleanup": {"branch": "polecat/nux/gt-1", "remote_deleted": true, "local_deleted": true}}
```

`kind` is one of `merged`, `conflict` (`conflict_task` names the task),
`gates_failed`, `scope_violation`, `not_eligible`, `needs_approval`,
`slot_timeout`, `branch_missing`, `cleanup_failed`, `precondition`, `error`;
`message` explains a failure and `warnings` lists non-fatal problems (e.g.
the MERGED mail could not be sent, or `run_tests=false` merged without gates);
`gates_ran` counts the gate commands that ran; `already_landed` is set when
the run only finalized an MR whose `commit_sha` was already on the target.
`cleanup_failed` means the merge is on origin but finalizing it failed: the
proof, closing the MR, closing the source `Merged in <mr>`, or the branch
cleanup. Fix the cause and run the command in `next`: `gt mq merge <rig>
<mr-id>` again while the MR is still open (it finds the landed commit and only
finalizes), `gt mq post-merge <rig> <mr-id>` once the MR is closed. The source counts as finalized when it was
closed `Merged in <mr>`, was already closed with a `Merged in ...` reason, or
is not closed by a merge by design: the MR has no source bead, or the source
is `no_merge`, `review_only` or `merge_strategy=local`. Anything else (the
source cannot be read or closed, is not a concrete work bead, is already
closed with another reason, or is **not bound to the MR**) is `cleanup_failed`.

**Source binding (gt-pd1c.89).** The source is force-closed `Merged in <mr>`
(which also unblocks its `merge-blocks` dependents) only when it carries the
back-link comment `MR created: <mr>` that `gt done` and `gt mq submit` write
on it when they create the MR (before the refinery is nudged). An MR's
`source_issue` is bead data any bd writer can set; the back-link is written
on the source itself, is append-only, survives the source's park (assignee
cleared) and polecat reuse. A source without it is reported (`not bound to MR
<mr>`) and left open; if the MR really is that bead's work, add the comment
(`bd comments add <source> "MR created: <mr>"`) and run the `next` command.
This applies to every merged close (gt mq merge, its batch path, post-merge
and `--confirm-landed`). It catches stale or mistaken data; it is not a guard
against a bd writer, who can add the comment as easily as the MR (stage 4).

**Branch cleanup on a remote without pull requests (gt-pd1c.64).** Before
deleting a merged polecat branch on origin the engine checks for an open PR
(gas-fk4: deleting a PR's head closes it as "closed"); a PR state that cannot
be read keeps the branch. When origin is a local repository (a `file://` URL
or a filesystem path, e.g. a mirror, for both its fetch and push URL) and
the MR records no PR URL or number, it hosts no PRs, so no lookup runs and
the branch is deleted (leased on the submitted `commit_sha`, after the
landing proof). Any other remote is looked up as before.

`gt mq post-merge <rig> <mr-id>` finishes an MR that `gt mq merge` already
closed as merged but did not fully finalize (`cleanup_failed` with
`"next": "gt mq post-merge <rig> <mr-id>"`). It is a thin wrapper over the
engine's one finalize (the same code), under the rig merge lock, and merges or
pushes nothing. It refuses, changing nothing: an MR that is still open (run
`gt mq merge`, which finalizes a merge that already landed), an MR not closed
with `close_reason: merged`, another rig's MR, and a landing proof not bound
to the MR: the MR must record `merge_commit` M, git must prove M on the
trusted target, and M must have exactly two parents with `commit_sha` second
(`commit_sha` merely reachable from the target is refused). It then closes
the source (if bound), deletes the branch (unless `--skip-branch-delete`),
mails MERGED, runs the convoy check and nudges the mayor; a repeated MERGED
for an already cleaned polecat is ignored by the witness. Only the refinery
or crew of `<rig>`, or a human, may run it. Exit status is non-zero unless
every step succeeded. A PR merged outside the refinery (owner merge) has no
recorded `merge_commit`: `gt mq merge` refuses it (`not_eligible`) and the
refinery reports it to the mayor; a human decides.

The two `--confirm-*` flags are the only manual exit for an issue held as
*unverified* by the merge queue: its MR is closed but git cannot decide whether
the work landed (no git prover for the rig, no `commit_sha`/`target` on the
MR, or a git answer short of a definite "not on the target and no equivalent
patch", see below). The overseer does
not have to look for them: every witness patrol (`gt patrol scan`, parked-bead
phase) mails the overseer (`High`, subject `UNVERIFIED_HOLD: <issue> (MR <mr>)
needs an overseer verdict`) with the issue, MR, rig, reason, age since the MR
closed and both commands. It is sent once per MR: after a successful send the
MR records `unverified_notified_at: <RFC3339>`, and a `REMINDER` follows while
the hold lasts, every `operational.witness.unverified_reminder_interval` in
`settings/config.json` (default `24h`; zero, negative or unparsable values use
the default, values below `1h` are raised to `1h`). A mark more than 5 minutes
in the future is invalid and does not silence the notice. Delivery is at least
once: a failed send marks nothing, so the next patrol retries; a failed mark
after a good send, or two patrols (`gt patrol scan`, `gt up`) notifying the
same MR at the same moment, repeats the notice. There is no in-memory backoff:
each `gt patrol scan` is a new process. Identifiers that are not plain IDs
(`^[A-Za-z0-9][A-Za-z0-9._-]*$`) never go into the copy-paste commands (the
witness rig or `<rig>`/`<mr-id>` is shown instead), and the reason is one
line, at most 300 bytes, with URL credentials masked (`://***@`).
`gt doctor` (`null-assignee-steps`) lists the same issues with the MR, the
reason and both commands. The verdict is written on the MR (`landing: landed|not_landed`,
`landing_by: <identity> via tty`), recorded as a comment on the issue, and then
the normal rule acts on it. Guards: agent roles (polecat, refinery, witness,
deacon, mayor) and a polecat `BD_ACTOR` are refused; stdin and stdout must be
a terminal and the MR ID must be typed again (there is no bypass flag); MRs
whose landing is already decided are refused. A verdict only counts while git
cannot decide: a definite git answer always wins over a recorded `landing:`.
If applying the verdict fails after it was recorded, rerun the same command
(the verdict is then proof) or let the witness orphan scan / `gt doctor --fix`
settle the issue.

Git proof (`git.(*Git).VerifyPushedCommitReachableFromPushTarget` and
`PatchEquivalentOnPushTarget`, used through `beads.MergeProver` /
`beads.PatchProver`) answers in this order:

0. Only a full hex `commit_sha`/`base_sha` (40 or 64 characters, as `gt done`
   and `gt mq submit` record them) and a plain
   `target` (segments of `^[A-Za-z0-9][A-Za-z0-9._-]*$` separated by `/`)
   are handed to git; any other value is unverified without running git.
1. The recorded `commit_sha` is reachable from `origin/<target>` (or from the
   default branch when the target was deleted): landed.
2. Not reachable, but the work is on the target as equivalent patches
   (`git patch-id --stable`, as `git cherry` uses): every non-merge commit of
   the MR has an equal patch on the target (rebase, cherry-pick), or the
   combined diff `merge-base..commit_sha` equals one target commit (squash):
   landed. When the MR records `base_sha` (the target tip at submission),
   only target commits after it count, so an older commit with the same
   one-line or since-reverted patch cannot prove the landing.
3. Not reachable, no equivalent patch, and no target commit message has a
   line equal to the subject of one of the MR's commits: definitely not
   landed. A shared subject (a rebase that resolved conflicts keeps it, a
   squash usually lists it) leaves the MR unverified.

Anything git cannot decide stays unverified (an error never counts as "not
landed"):
- the remote or branch cannot be read or fetched;
- the commit object is absent, in a partial or shallow clone or because its
  patches cannot be compared;
- `base_sha` is not an ancestor of the target;
- only part of the MR's commits have an equal patch;
- the commit has no diff;
- more than 2000 commits would have to be compared;
- the prover has no patch check.

Every git command of the merge-queue prover (reachability and patch check)
runs with a 60 s timeout and at most 16 MiB of output. On timeout the whole
git process group is killed (ssh and remote helpers included), and output
pipes are waited for at most 5 s more. Local patch-check commands also run
with `GIT_NO_LAZY_FETCH=1`, so a partial clone never fetches a missing
object. Hitting any of these limits makes the answer unverified. The
refinery's and `gt done`'s own post-push verification keeps running without
these bounds.

In a shallow clone (gastown's `.repo.git` is cloned with `--depth 1`) "not
reachable" counts only when every shallow boundary that is reachable from the
target is an ancestor of the commit, i.e. the commit was made after the
clone's boundary. With merges, the commit's own history can be cut at
another boundary; that only leaves more MRs unverified, never fewer. A commit
that predates the boundary stays unverified. Gastown never deepens
`.repo.git` itself; an operator who wants such MRs decided by git can make
the clone complete once with
`git -C <rig>/.repo.git fetch --unshallow origin <branch>`. That costs the
branch's full history in disk and download, and the refinery and polecat
worktrees keep working on the deeper repository. A rebase that resolved
conflicts with a reworded subject, or a squash whose message does not list
the MR's subjects and whose context lines changed, still reads as not landed
unless the MR records `merge_commit` (the normal refinery close does).

#### Integration Branch Commands

```bash
gt mq integration create <epic-id>              # Create integration branch
gt mq integration create <epic-id> --branch "feat/{title}"  # Custom template
gt mq integration create <epic-id> --base-branch develop   # Non-main base
gt mq integration status <epic-id>              # Show branch status
gt mq integration status <epic-id> --json       # JSON output
gt mq integration land <epic-id>                # Merge to base branch (default: main)
gt mq integration land <epic-id> --dry-run      # Preview only
gt mq integration land <epic-id> --force        # Land with open MRs
gt mq integration land <epic-id> --skip-tests   # Skip test run
```

See [Integration Branches](concepts/integration-branches.md) for the full workflow.

## Beads Commands (bd)

```bash
bd ready                     # Work with no blockers
bd list --status=open
bd list --status=in_progress
bd show <id>
bd create --title="..." --type=task
bd update <id> --status=in_progress
bd close <id>
bd dep add <child> <parent>  # child depends on parent
```

## Patrol Agents

Deacon, Witness, and Refinery run continuous patrol loops using wisps:

| Agent | Patrol Molecule | Responsibility |
|-------|-----------------|----------------|
| **Deacon** | `mol-deacon-patrol` | Agent lifecycle, plugin execution, health checks |
| **Witness** | `mol-witness-patrol` | Monitor polecats, nudge stuck workers |
| **Refinery** | `mol-refinery-patrol` | Process merge queue, review MRs, check integration branches |

```
1. gt patrol new               # Create root-only wisp
2. gt prime                    # Shows patrol checklist inline
3. Work through each step
4. gt patrol report --summary "..."  # Close + start next cycle
```

### Daemon idle shutdown (opt-in)

The daemon can run `gt down` (default behavior, not `--all`/`--nuke`) when the
town has had no activity for a timeout. It is off unless enabled in
`daemon.json`:

```json
{"patrols": {"idle_shutdown": {"enabled": true, "idle_timeout": "30m"}}}
```

`idle_timeout` is a Go duration (default `30m`). "Idle" means, continuously for
the whole timeout: no live polecat session, no real `hooked`/`in_progress` bead
(town and rigs), an empty merge queue, and no tmux client attached to the town
socket. Deacon/witness/refinery patrol wisps, the daemon-dispatched infrastructure
dog formulas (`mol-dog-*`) and agent beads do not count as work. While the
town is active the daemon logs why (counts and a few blocking bead/MR IDs),
once per change or per `idle_timeout`. Any lookup that fails counts as active and
resets the timer. Before `gt down` the daemon logs, mails the overseer
(`gt mail send --human`) and emits an `idle_shutdown` feed event; it fires once
per idle period (if `gt down` fails to start or exits non-zero it retries after
another full idle period).

Notes:
- Idle polecat sessions keep the town active until the idle reaper kills them,
  so the timer effectively starts after the reap.
- Crew and mayor sessions are not counted directly: `gt down` stops them, so
  their work is protected only through its work bead (hooked/in_progress) or an
  attached tmux client. Gas Town starts no tmux control-mode clients of its
  own; only a human attach counts.
- The daemon exits 0 on SIGTERM (`shutdown` returns nil), which is compatible
  with a launchd `KeepAlive` of `SuccessfulExit=false`.

## Plugin Molecules

Plugins are molecules with specific labels:

```json
{
  "id": "mol-security-scan",
  "labels": ["template", "plugin", "witness", "tier:haiku"]
}
```

Patrol molecules bond plugins dynamically:

```bash
bd mol bond mol-security-scan $PATROL_ID --var scope="$SCOPE"
```

## Formula Invocation Patterns

**CRITICAL**: Different formula types require different invocation methods.

### Workflow Formulas (sequential steps, single polecat)

Examples: `shiny`, `shiny-enterprise`, `mol-polecat-work`

```bash
gt sling <formula> --on <bead-id> <target>
gt sling shiny-enterprise --on gt-abc123 gastown
```

### Convoy Formulas (parallel legs, multiple polecats)

Examples: `code-review`

**DO NOT use `gt sling` for convoy formulas!** It fails with "convoy type not supported".

```bash
# Correct invocation - use gt formula run:
gt formula run code-review --pr=123
gt formula run code-review --files="src/*.go"

# Dry run to preview:
gt formula run code-review --pr=123 --dry-run
```

### Identifying Formula Type

```bash
gt formula show <name>   # Shows "Type: convoy" or "Type: workflow"
bd formula list          # Lists formulas by type
```

### Why This Matters

- `gt sling` attempts to cook+pour the formula, which fails for convoy type
- `gt formula run` handles convoy dispatch directly, spawning parallel polecats
- Convoy formulas create multiple polecats (one per leg) + synthesis step
- Legs of a convoy formula with `review_only = true` (formula-wide or per leg)
  are created with the `review_only: true` field on the leg bead, so every later
  re-dispatch of a leg stays review-only

## Common Issues

| Problem | Solution |
|---------|----------|
| Agent in wrong directory | Check cwd, `gt doctor` |
| Beads prefix mismatch | Check `bd show` vs rig config |
| Worktree conflicts | Check worktree state, `gt doctor` |
| Stuck worker | `gt nudge`, then `gt peek` |
| Dirty git state | Commit or discard, then `gt handoff` |

> For architecture details (bare repo pattern, beads as control plane, nondeterministic idempotence), see [architecture.md](design/architecture.md).
