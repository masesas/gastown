// Package pushguard installs and inspects the Gas Town managed git pre-push
// hook in rig repositories (gt-pd1c.72).
//
// The hook is a thin shell shim: it feeds git's pre-push input to
// `gt hooks pre-push <remote> <url>`, where all rules live (in Go, tested),
// and then runs the project's own pre-push hook, if one existed before Gas
// Town installed its hook. It fails closed: without gt on PATH the push is
// refused.
//
// Where the hook goes:
//   - core.hooksPath unset: <git-common-dir>/hooks/pre-push. For the rig's
//     shared bare repo (.repo.git) this one file covers the refinery and every
//     polecat worktree. A pre-existing project pre-push is kept as
//     pre-push.gt-chained and run after the Gas Town check.
//   - core.hooksPath set (a project-owned, usually tracked, hooks dir): Gas
//     Town never writes there. The repo is reported as delegated when that
//     pre-push calls `gt hooks pre-push`, otherwise as a conflict.
//
// Local hooks are advisory against a determined caller: `git push --no-verify`
// and `git -c core.hooksPath=<dir> push` skip them. Branch protection on the
// real remote is the hard boundary (docs/enh-masesas/runbook-real-project.md).
package pushguard

// Marker identifies a Gas Town managed pre-push hook. The version suffix in
// the script header changes whenever the script changes, so an installed copy
// that differs from Script is reported as outdated and rewritten.
const Marker = "gastown-managed-pre-push"

// ChainedHookName is the file, next to the managed hook, that holds the
// project's original pre-push hook. The managed hook runs it after the Gas
// Town check, with the same arguments and stdin.
const ChainedHookName = "pre-push.gt-chained"

// DelegationMarker is the command a project-owned pre-push hook (under
// core.hooksPath) must contain for the repo to count as guarded.
const DelegationMarker = "gt hooks pre-push"

const script = `#!/bin/sh
# ` + Marker + ` v1
#
# Managed by Gas Town (gt rig add, gt up, polecat/crew creation, gt doctor --fix).
# Do not edit: this file is rewritten on upgrade. The push rules live in Go:
# "gt hooks pre-push" (see docs/HOOKS.md, "Rig pre-push guard"). A project
# pre-push hook that existed before installation was moved to
# ` + ChainedHookName + ` and runs after the Gas Town check.
input=$(cat)
feed() { if [ -n "$input" ]; then printf '%s\n' "$input"; fi; }
if ! command -v gt >/dev/null 2>&1; then
  echo "BLOCKED: Gas Town pre-push guard cannot run: 'gt' was not found in PATH." >&2
  echo "Put the gt binary on PATH and push again." >&2
  exit 1
fi
feed | gt hooks pre-push "$@" || exit 1
chained="$(dirname "$0")/` + ChainedHookName + `"
if [ -x "$chained" ]; then
  feed | "$chained" "$@" || exit $?
fi
exit 0
`

// Script returns the managed pre-push hook content.
func Script() string { return script }
