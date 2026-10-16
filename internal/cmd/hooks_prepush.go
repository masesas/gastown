package cmd

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/rig"
	"github.com/steveyegge/gastown/internal/workspace"
)

// Rig pre-push guard (gt-pd1c.72).
//
// The Gas Town managed pre-push hook (internal/pushguard) pipes git's
// pre-push input into `gt hooks pre-push <remote> <url>`. All rules live here:
//
//   - Push target classification: only the rig default branch and
//     integration/* (the merge-request targets) are checked. polecat/* and
//     every other branch, tags and deletions pass without any I/O beyond
//     reading stdin, so normal polecat work is not slowed down.
//   - Land guard: a push to a target that newly contains the tip of another
//     integration/* branch on the push remote is an integration land. An
//     agent (agentSessionEnvVar, shared with gt mq integration land) may land
//     only when merge_queue.integration_branch_auto_land is true AND the push
//     comes from gt mq integration land (GT_INTEGRATION_LAND=1). With
//     auto-land off GT_INTEGRATION_LAND is ignored for agents. Humans keep the
//     previous rule: landing needs GT_INTEGRATION_LAND=1.
//   - Scope guard (agents only): the pushed range remote_sha..local_sha (for a
//     new branch: merge-base with the default branch..local_sha) must stay
//     inside merge_queue.allowed_paths (checkMergeScopeWith, as gt done and
//     gt mq submit).
//
// Everything that cannot be verified is refused (fail closed). Errors never
// include the push URL (it may carry credentials).

// prePushZeroSHA is the object name git uses for "no commit" in pre-push input.
const prePushZeroSHA = "0000000000000000000000000000000000000000"

const integrationBranchPrefix = "integration/"

var hooksPrePushCmd = &cobra.Command{
	Use:   "pre-push <remote> [<url>]",
	Short: "Git pre-push guard for rig repositories (run by the managed hook)",
	Long: `Check a git push against the rig's merge rules. The Gas Town managed
pre-push hook runs this with git's arguments and pipes git's pre-push input
("<local_ref> <local_sha> <remote_ref> <remote_sha>" per line) to stdin.

Rules (agent = GT_ROLE, BD_ACTOR, GT_POLECAT or GT_CREW is set):
  - polecat/*, other branches, tags, deletions: allowed, not checked.
  - Push to the rig default branch or integration/* that lands another
    integration branch: refused for agents unless
    merge_queue.integration_branch_auto_land is true and the push comes from
    gt mq integration land. Humans need GT_INTEGRATION_LAND=1 (set by
    gt mq integration land).
  - Agent push to the default branch or integration/*: every changed path in
    the pushed range must match merge_queue.allowed_paths (when set).
Anything that cannot be verified is refused.

The hook is installed by gt rig add, gt up, polecat/crew creation and
gt doctor --fix. git push --no-verify skips local hooks; protect the real
remote with branch protection (see docs/HOOKS.md).`,
	Args:          cobra.RangeArgs(1, 2),
	SilenceUsage:  true,
	SilenceErrors: false,
	RunE:          runHooksPrePush,
}

func init() {
	hooksCmd.AddCommand(hooksPrePushCmd)
}

func runHooksPrePush(cmd *cobra.Command, args []string) error {
	updates, err := parsePrePushUpdates(cmd.InOrStdin())
	if err != nil {
		return err
	}
	cwd, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("BLOCKED: pre-push guard: getting current directory: %w", err)
	}
	url := ""
	if len(args) > 1 {
		url = args[1]
	}
	g := &prePushGuard{
		remote: args[0],
		url:    url,
		getenv: os.Getenv,
		git:    git.NewGit(cwd),
		loadRig: func() (*prePushRig, error) {
			townRoot, _ := workspace.Find(cwd) // not in a town: no rig
			return resolvePrePushRig(townRoot, cwd)
		},
	}
	return g.check(updates)
}

// prePushUpdate is one line of git's pre-push input.
type prePushUpdate struct {
	LocalRef, LocalSHA, RemoteRef, RemoteSHA string
}

// parsePrePushUpdates reads git's pre-push input. Blank lines are skipped; a
// malformed line is an error (fail closed).
func parsePrePushUpdates(r io.Reader) ([]prePushUpdate, error) {
	var out []prePushUpdate
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 4 {
			return nil, fmt.Errorf("BLOCKED: pre-push guard: malformed input line (want 4 fields, got %d)", len(f))
		}
		out = append(out, prePushUpdate{LocalRef: f[0], LocalSHA: f[1], RemoteRef: f[2], RemoteSHA: f[3]})
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("BLOCKED: pre-push guard: reading input: %w", err)
	}
	return out, nil
}

// prePushRig is the rig policy the guard applies.
type prePushRig struct {
	name          string
	defaultBranch string // "" = detect from git
	autoLand      autoLandSetting
	allowed       []string
	allowedErr    error // scope unknown: agent pushes to targets fail closed
}

// resolvePrePushRig maps cwd to its rig (<townRoot>/<rig>/...) and reads the
// policy straight from the rig files, so a broken rigs.json cannot turn the
// guard off. Returns nil, nil outside a rig. Unreadable settings fail closed:
// auto-land off, scope error.
func resolvePrePushRig(townRoot, cwd string) (*prePushRig, error) {
	if townRoot == "" {
		return nil, nil
	}
	rel, err := filepath.Rel(townRoot, cwd)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return nil, nil
	}
	name := strings.Split(rel, string(filepath.Separator))[0]
	if name == "mayor" || strings.HasPrefix(name, ".") {
		return nil, nil
	}
	rigPath := filepath.Join(townRoot, name)
	r := &prePushRig{name: name, autoLand: loadAutoLandSetting(rigPath)}
	if cfg, err := rig.LoadRigConfig(rigPath); err == nil {
		r.defaultBranch = strings.TrimSpace(cfg.DefaultBranch)
	}
	r.allowed, r.allowedErr = loadMergeScope(rigPath)
	return r, nil
}

// prePushGit is the git surface the guard needs.
type prePushGit interface {
	ListRemoteRefsWithHashes(remote, prefix string) ([]git.RemoteRef, error)
	IsAncestor(ancestor, descendant string) (bool, error)
	Rev(ref string) (string, error)
	MergeBase(a, b string) (string, error)
	DiffNameOnlyRange(from, to string) ([]string, error)
	RemoteDefaultBranch() string
}

// prePushGuard evaluates one push.
type prePushGuard struct {
	remote  string // remote name as passed by git (may be a URL)
	url     string // push URL as passed by git; never printed
	getenv  func(string) string
	git     prePushGit
	loadRig func() (*prePushRig, error)

	rigLoaded bool
	rig       *prePushRig
	rigErr    error
	tips      []git.RemoteRef
	tipsErr   error
	tipsDone  bool
}

// check returns nil when every update may be pushed, else an error naming
// every refused update.
func (g *prePushGuard) check(updates []prePushUpdate) error {
	var errs []error
	for _, u := range updates {
		if err := g.checkUpdate(u); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (g *prePushGuard) checkUpdate(u prePushUpdate) error {
	branch, ok := strings.CutPrefix(u.RemoteRef, "refs/heads/")
	if !ok || strings.HasPrefix(branch, "polecat/") || u.LocalSHA == prePushZeroSHA {
		return nil // tag/other ref, polecat work, or deletion
	}
	agentVar := agentSessionEnvVar(g.getenv)
	r, err := g.loadRigOnce()
	if err != nil {
		if agentVar != "" {
			return fmt.Errorf("BLOCKED: push to %s: cannot load rig policy (%v); refusing agent push", branch, err)
		}
		r = nil // human: fall back to git defaults
	}
	defaultBranch := ""
	if r != nil {
		defaultBranch = r.defaultBranch
	}
	if defaultBranch == "" {
		defaultBranch = g.git.RemoteDefaultBranch()
	}
	if branch != defaultBranch && !strings.HasPrefix(branch, integrationBranchPrefix) {
		return nil
	}
	if err := g.checkLand(u, branch, r, agentVar); err != nil {
		return err
	}
	if agentVar == "" {
		return nil
	}
	return g.checkScope(u, branch, defaultBranch, r)
}

func (g *prePushGuard) loadRigOnce() (*prePushRig, error) {
	if !g.rigLoaded {
		g.rig, g.rigErr = g.loadRig()
		g.rigLoaded = true
	}
	return g.rig, g.rigErr
}

// checkLand refuses a push that lands an integration branch (see file doc).
func (g *prePushGuard) checkLand(u prePushUpdate, branch string, r *prePushRig, agentVar string) error {
	landEnv := g.getenv("GT_INTEGRATION_LAND") == "1"
	autoLand := r != nil && r.autoLand.enabled
	if landEnv && (agentVar == "" || autoLand) {
		return nil // gt mq integration land (or a human who set it)
	}
	if u.RemoteSHA == prePushZeroSHA {
		return nil // creating a branch is not a land; scope still applies
	}
	tips, err := g.integrationTips()
	if err != nil {
		return verifyError(branch, "cannot list integration branches on the push remote; retry when it is reachable", agentVar)
	}
	for _, tip := range tips {
		if tip.Name == u.RemoteRef {
			continue // pushing to that integration branch itself
		}
		landed, err := g.introduces(tip.Hash, u)
		if err != nil {
			return verifyError(branch, err.Error(), agentVar)
		}
		if landed {
			return g.landError(branch, strings.TrimPrefix(tip.Name, "refs/heads/"), agentVar, r)
		}
	}
	return nil
}

func (g *prePushGuard) integrationTips() ([]git.RemoteRef, error) {
	if !g.tipsDone {
		target := g.url
		if target == "" {
			target = g.remote
		}
		g.tips, g.tipsErr = g.git.ListRemoteRefsWithHashes(target, "refs/heads/"+integrationBranchPrefix)
		g.tipsDone = true
	}
	return g.tips, g.tipsErr
}

// introduces reports whether tip is reachable from the pushed commit but not
// from the remote's current commit. A tip unknown locally cannot be in the
// pushed history. A remote commit unknown locally cannot be verified.
func (g *prePushGuard) introduces(tip string, u prePushUpdate) (bool, error) {
	if _, err := g.git.Rev(tip + "^{commit}"); err != nil {
		return false, nil
	}
	in, err := g.git.IsAncestor(tip, u.LocalSHA)
	if err != nil {
		return false, fmt.Errorf("cannot verify integration ancestry: %v", firstLine(err.Error()))
	}
	if !in {
		return false, nil
	}
	if _, err := g.git.Rev(u.RemoteSHA + "^{commit}"); err != nil {
		return false, errors.New("the remote branch tip is not in this repository; fetch first, then push again")
	}
	already, err := g.git.IsAncestor(tip, u.RemoteSHA)
	if err != nil {
		return false, fmt.Errorf("cannot verify integration ancestry: %v", firstLine(err.Error()))
	}
	return !already, nil
}

// landError refuses a push that lands integration. Agents are never told
// about GT_INTEGRATION_LAND: it does not apply to them while auto-land is off,
// and with auto-land on they must use gt mq integration land.
func (g *prePushGuard) landError(branch, integration, agentVar string, r *prePushRig) error {
	msg := fmt.Sprintf("BLOCKED: push to %s lands integration branch %s.", branch, integration)
	switch {
	case agentVar != "" && (r == nil || !r.autoLand.enabled):
		where, why := "no rig settings found", "auto-land treated as off"
		if r != nil {
			where, why = "rig "+r.name, "merge_queue.integration_branch_auto_land=false"
			if r.autoLand.loadErr != "" {
				why = "rig settings unreadable, auto-land treated as off"
			}
		}
		return fmt.Errorf("%s %s (%s): agents may not land integration branches "+
			"(caller identified as an agent via %s). A human must run gt mq integration land <epic-id>.",
			msg, where, why, agentVar)
	case agentVar != "":
		return fmt.Errorf("%s Land integration branches only with gt mq integration land <epic-id>.", msg)
	default:
		return fmt.Errorf("%s Land integration branches with gt mq integration land <epic-id> "+
			"(it sets GT_INTEGRATION_LAND=1 for its push); set GT_INTEGRATION_LAND=1 yourself only "+
			"for an intentional manual land.", msg)
	}
}

// verifyError refuses a push whose land status cannot be verified.
func verifyError(branch, reason, agentVar string) error {
	msg := fmt.Sprintf("BLOCKED: push to %s: %s (fail closed).", branch, reason)
	if agentVar == "" {
		msg += " For an intentional land, GT_INTEGRATION_LAND=1 skips this check."
	}
	return errors.New(msg)
}

// checkScope enforces merge_queue.allowed_paths on an agent push to an MR
// target branch.
func (g *prePushGuard) checkScope(u prePushUpdate, branch, defaultBranch string, r *prePushRig) error {
	if r == nil {
		return nil // not in a rig: no scope declared
	}
	if r.allowedErr != nil {
		return fmt.Errorf("BLOCKED: push to %s: %v (refusing agent push while the merge scope is unknown)", branch, r.allowedErr)
	}
	if len(r.allowed) == 0 {
		return nil
	}
	base := u.RemoteSHA
	if base == prePushZeroSHA {
		mb, err := g.newBranchBase(u.LocalSHA, defaultBranch)
		if err != nil {
			return fmt.Errorf("BLOCKED: push to new branch %s: %v", branch, err)
		}
		base = mb
	} else if _, err := g.git.Rev(base + "^{commit}"); err != nil {
		return fmt.Errorf("BLOCKED: push to %s: the remote branch tip is not in this repository; fetch first, then push again", branch)
	}
	if _, err := checkMergeScopeWith(r.allowed, base, "..", u.LocalSHA, g.git.DiffNameOnlyRange); err != nil {
		return fmt.Errorf("BLOCKED: push to %s (rig %s): %w", branch, r.name, err)
	}
	return nil
}

// newBranchBase returns the merge-base of local with the default branch, for
// a push that creates a branch.
func (g *prePushGuard) newBranchBase(local, defaultBranch string) (string, error) {
	var candidates []string
	if g.remote != "" && !strings.ContainsAny(g.remote, "/:") {
		candidates = append(candidates, "refs/remotes/"+g.remote+"/"+defaultBranch)
	}
	candidates = append(candidates, "refs/remotes/origin/"+defaultBranch, "refs/heads/"+defaultBranch)
	for _, ref := range candidates {
		if _, err := g.git.Rev(ref + "^{commit}"); err != nil {
			continue
		}
		mb, err := g.git.MergeBase(ref, local)
		if err != nil {
			return "", fmt.Errorf("no merge-base with %s; cannot check merge scope", defaultBranch)
		}
		return mb, nil
	}
	return "", fmt.Errorf("default branch %s not found locally; cannot check merge scope", defaultBranch)
}
