package cmd

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/spf13/cobra"
	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/mergeproof"
	"github.com/steveyegge/gastown/internal/pushguard"
	"github.com/steveyegge/gastown/internal/rig"
	"github.com/steveyegge/gastown/internal/workspace"
)

// Rig pre-push guard (gt-pd1c.72).
//
// The Gas Town managed pre-push hook (internal/pushguard) pipes git's
// pre-push input into `gt hooks pre-push <remote> <url>`. All rules live here:
//
//   - Targets: the rig default branch and integration/* (the merge-request
//     targets). Pushes to polecat/* and every other branch and tags pass
//     without any I/O beyond reading stdin, so normal polecat work is not
//     slowed down.
//   - Polecat branch tips (gt-pd1c.78, agents other than crew): a deletion,
//     or a rewrite (not a fast-forward) by an agent other than the refinery
//     and the owning polecat, discards the remote tip and is allowed only
//     when that loses no work (polecatBranchDeletion): the old tip is
//     preserved on the remote default branch or a remote integration/*
//     branch, or archived under refs/gt/archive/* on the push remote, or the
//     MR of exactly that commit landed by git proof
//     (beads.MergeQueue.TipDisposition). Everything else (no MR, rejected,
//     conflict, an open MR, an unreadable merge queue) refuses with its one
//     exit. Humans and crew are not checked.
//   - Archive refs (refs/gt/archive/*, gt-pd1c.78 tahap 2, agents other than
//     crew): never deleted or rewritten; created only in gt polecat
//     abandon's shape for work whose MR was closed as rejected or superseded
//     (checkArchiveUpdate).
//   - Target roles (gt-pd1c.78): an update of a target by an agent other than
//     the refinery or crew (witness, mayor, deacon, dog, boot, polecat, an
//     unknown role) is refused. Exceptions already in Gas Town: a polecat on a
//     rig whose merge queue is disabled (gt done's direct strategy,
//     rigMergeQueueActive) and creating an integration/* branch whose tip is
//     already on a remote target (gt mq integration create).
//   - Rig detection: from the real (symlink-resolved) git common dir, then the
//     real working directory. An agent push to a target from a repository
//     that runs the managed hook but whose rig cannot be detected is refused
//     (fail closed); other repositories (the Gas Town source repo delegating
//     from .githooks, the town repo) get the rules without rig settings
//     (auto-land off, no scope). When the rig's
//     default_branch is unknown, agents treat main, master and origin/HEAD
//     as default branches.
//   - Rewrites (agents): an update of a target must be a fast-forward
//     (remote_sha ancestor of local_sha, both known locally). Otherwise
//     rewinding integration/x onto the default branch would hide its old tip.
//   - Deletions (agents): the default branch may never be deleted; an
//     integration/* branch only once its tip is already on the remote default
//     branch (landed), as gt mq integration land cleanup does. Otherwise a
//     delete-then-merge would hide the land from the land guard.
//   - Land guard: a push to a target that newly contains the tip of another
//     integration/* branch (on the push remote, or a local integration/*
//     branch or refs/remotes/<remote>/integration/* tracking ref) is a land.
//     An agent (agentSessionEnvVar, shared with gt mq integration land) is
//     refused while merge_queue.integration_branch_auto_land is not true,
//     whatever GT_INTEGRATION_LAND says. With auto-land on, an agent push with
//     GT_INTEGRATION_LAND=1 passes; the variable is a policy marker set by
//     gt mq integration land, not proof of origin (any agent could set it).
//     Humans keep the previous rule: landing needs GT_INTEGRATION_LAND=1.
//   - Scope guard (agents only): the pushed range remote_sha..local_sha (for a
//     new branch: merge-base of the remote default branch tip, read with
//     ls-remote, and local_sha) must stay inside merge_queue.allowed_paths
//     (checkMergeScopeWith, as gt done and gt mq submit).
//
// Everything that cannot be verified is refused (fail closed). Errors never
// include the push URL (it may carry credentials).

// prePushZeroSHA is the object name git uses for "no commit" in pre-push input.
const prePushZeroSHA = "0000000000000000000000000000000000000000"

const integrationBranchPrefix = mergeproof.IntegrationBranchPrefix

var hooksPrePushCmd = &cobra.Command{
	Use:   "pre-push <remote> [<url>]",
	Short: "Git pre-push guard for rig repositories (run by the managed hook)",
	Long: `Check a git push against the rig's merge rules. The Gas Town managed
pre-push hook runs this with git's arguments and pipes git's pre-push input
("<local_ref> <local_sha> <remote_ref> <remote_sha>" per line) to stdin.

Rules (agent = GT_ROLE, BD_ACTOR, GT_POLECAT or GT_CREW is set):
  - Pushes to polecat/*, other branches, tags: allowed, not checked.
  - Agent (not crew) deletion of polecat/*, or a non-fast-forward update by
    an agent other than the refinery and the owning polecat: allowed only
    when the old tip is preserved on the remote default branch or
    integration/*, archived under refs/gt/archive/*, or the MR of that tip
    landed by git proof. Rejected/superseded work is abandoned with
    gt polecat abandon; anything else is reworked or resubmitted.
    Unreadable beads: refused.
  - Agent (not crew) push to refs/gt/archive/*: delete and rewrite refused;
    create only for work whose MR was closed as rejected or superseded.
  - Update of the default branch or integration/* by an agent other than the
    refinery or crew: refused (a polecat may push there only when the rig's
    merge queue is disabled; a new integration/* branch with no new commits
    may be created by any agent).
  - Agent push to the default branch or integration/* from a repository that
    runs the managed hook but whose rig cannot be detected: refused.
  - Agent update of the default branch or integration/* that is not a
    fast-forward: refused.
  - Agent deletion of the default branch: refused; of integration/*: refused
    unless the branch has already landed on the default branch.
  - Push to the rig default branch or integration/* that lands another
    integration branch: refused for agents unless
    merge_queue.integration_branch_auto_land is true (and GT_INTEGRATION_LAND=1,
    set by gt mq integration land). Humans need GT_INTEGRATION_LAND=1.
  - Agent push to the default branch or integration/*: every changed path in
    the pushed range must match merge_queue.allowed_paths (when set).
Anything that cannot be verified is refused.

The hook is installed by gt rig add, gt up, polecat/crew creation and
gt doctor --fix. Local hooks can be skipped by the pusher; protect the real
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
	// First: nothing the guard starts (git, bd, provers of rig repositories)
	// may inherit the pushing repository's GIT_DIR & co. (clearGitLocalEnv).
	clearGitLocalEnv()
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
			return detectPrePushRig(prePushLocations(cwd))
		},
		isManaged: func() bool {
			st, err := pushguard.Inspect(cwd)
			if err != nil {
				return true // cannot tell: fail closed
			}
			return st.State == pushguard.StateInstalled || st.State == pushguard.StateOutdated
		},
	}
	g.loadQueue = func() *beads.MergeQueue { return prePushMergeQueue(cwd) }
	return g.check(updates)
}

// prePushMergeQueue reads the town's merge queue for the repository at cwd
// (runHooksPrePush has already cleared git's repository-local variables).
func prePushMergeQueue(cwd string) *beads.MergeQueue {
	for _, loc := range prePushLocations(cwd) {
		if town, err := workspace.Find(loc); err == nil && town != "" {
			return mergeproof.TownMergeQueue(town)
		}
	}
	return beads.NewMergeQueue(nil, errNoTown, nil)
}

// realPath resolves symlinks so a cwd reached through a symlink still maps
// to its town and rig. Falls back to the absolute path.
func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	if a, err := filepath.Abs(p); err == nil {
		return a
	}
	return p
}

// prePushLocations returns the real paths that identify the repository: its
// git common dir (shared .repo.git for worktrees, <clone>/.git otherwise),
// then the working directory.
func prePushLocations(cwd string) []string {
	var locs []string
	cmd := exec.Command("git", "rev-parse", "--path-format=absolute", "--git-common-dir")
	cmd.Dir = cwd
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err == nil {
		if common := strings.TrimSpace(out.String()); common != "" {
			locs = append(locs, realPath(common))
		}
	}
	return append(locs, realPath(cwd))
}

// detectPrePushRig returns the rig of the first location inside a rig, or
// nil when none is.
func detectPrePushRig(locations []string) (*prePushRig, error) {
	for _, loc := range locations {
		townRoot, _ := workspace.Find(loc)
		r, err := resolvePrePushRig(townRoot, loc)
		if err != nil || r != nil {
			return r, err
		}
	}
	return nil, nil
}

// prePushSHA is a SHA-1 or SHA-256 object name as git writes it to pre-push
// input (all zeros for "no commit").
var prePushSHA = regexp.MustCompile(`^[0-9a-f]{40,64}$`)

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
		if !prePushSHA.MatchString(f[1]) || !prePushSHA.MatchString(f[3]) {
			return nil, errors.New("BLOCKED: pre-push guard: malformed input line (object names must be 40-64 lowercase hex digits)")
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
	defaultBranch string // "" = unknown (agents check main, master and origin/HEAD)
	autoLand      autoLandSetting
	allowed       []string
	allowedErr    error // scope unknown: agent pushes to targets fail closed
	// mergeQueueOff: merge_queue.enabled=false (rigMergeQueueActive), the
	// only case in which a polecat lands its own work (gt done, direct
	// strategy). Zero value = active queue (fail closed).
	mergeQueueOff bool
}

// resolvePrePushRig maps path to its rig (<townRoot>/<rig>/..., where the rig
// directory has config.json) and reads the policy straight from the rig
// files, so a broken rigs.json cannot turn the guard off. Returns nil, nil
// outside a rig. Unreadable settings fail closed: auto-land off, scope error.
func resolvePrePushRig(townRoot, path string) (*prePushRig, error) {
	if townRoot == "" {
		return nil, nil
	}
	rel, err := filepath.Rel(townRoot, path)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return nil, nil
	}
	name := strings.Split(rel, string(filepath.Separator))[0]
	if name == "mayor" || strings.HasPrefix(name, ".") {
		return nil, nil
	}
	rigPath := filepath.Join(townRoot, name)
	if _, err := os.Stat(filepath.Join(rigPath, "config.json")); err != nil {
		return nil, nil
	}
	r := &prePushRig{name: name, autoLand: loadAutoLandSetting(rigPath)}
	if cfg, err := rig.LoadRigConfig(rigPath); err == nil {
		r.defaultBranch = strings.TrimSpace(cfg.DefaultBranch)
	}
	r.allowed, r.allowedErr = loadMergeScope(rigPath)
	r.mergeQueueOff = !rigMergeQueueActive(townRoot, name)
	return r, nil
}

// prePushGit is the git surface the guard needs.
type prePushGit interface {
	ListRemoteRefsWithHashes(remote, prefix string) ([]git.RemoteRef, error)
	ListLocalRefsWithHashes(prefix string) ([]git.RemoteRef, error)
	IsAncestor(ancestor, descendant string) (bool, error)
	Rev(ref string) (string, error)
	MergeBase(a, b string) (string, error)
	DiffNameOnlyRange(from, to string) ([]string, error)
	PreservedOn(head, base string) (git.BranchPreservationStatus, error)
	RemoteDefaultBranch() string
}

// prePushGuard evaluates one push.
type prePushGuard struct {
	remote  string // remote name as passed by git (may be a URL)
	url     string // push URL as passed by git; never printed
	getenv  func(string) string
	git     prePushGit
	loadRig func() (*prePushRig, error)
	// isManaged reports whether the repository runs the managed hook (nil =
	// assume yes). Agents pushing to a target from a managed repo whose rig
	// cannot be detected are refused; other repos (e.g. the Gas Town source
	// repo delegating from .githooks) run the rules without rig settings.
	isManaged func() bool
	// loadQueue reads the town's merge queue, only for an agent deleting a
	// polecat branch whose tip is not on a target (nil = unreadable).
	loadQueue func() *beads.MergeQueue

	rigLoaded bool
	rig       *prePushRig
	rigErr    error
	tips      []git.RemoteRef
	tipsErr   error
	tipsDone  bool
	// remoteList reads the push remote; every remote read of one push
	// (branch tips, targets, archives) goes through listRemote, so each
	// prefix is read once.
	remoteList mergeproof.RemoteLister
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
	if strings.HasPrefix(u.RemoteRef, mergeproof.ArchiveRefPrefix) {
		return g.checkArchiveUpdate(u)
	}
	branch, ok := strings.CutPrefix(u.RemoteRef, "refs/heads/")
	if !ok {
		return nil // tag or other ref
	}
	if strings.HasPrefix(branch, "polecat/") {
		return g.checkPolecatUpdate(u, branch)
	}
	agentVar := agentSessionEnvVar(g.getenv)
	deletion := u.LocalSHA == prePushZeroSHA
	if deletion && agentVar == "" {
		return nil
	}
	r, err := g.loadRigOnce()
	if err != nil {
		if agentVar != "" {
			return fmt.Errorf("BLOCKED: push to %s: cannot load rig policy (%v); refusing agent push", branch, err)
		}
		r = nil // human: fall back to git defaults
	}
	defaults := g.defaultBranches(r, agentVar != "")
	if !containsString(defaults, branch) && !strings.HasPrefix(branch, integrationBranchPrefix) {
		return nil
	}
	if agentVar != "" && r == nil && g.managedRepo() {
		return fmt.Errorf("BLOCKED: push to %s: this repository runs the Gas Town managed pre-push hook "+
			"but no rig was detected for it; refusing agent push to a merge target (fail closed)", branch)
	}
	if deletion {
		return g.checkAgentDelete(u, branch, defaults)
	}
	if err := g.checkTargetRole(u, branch, r, defaults); err != nil {
		return err
	}
	if agentVar != "" {
		if err := g.checkFastForward(u, branch); err != nil {
			return err
		}
	}
	if err := g.checkLand(u, branch, r, agentVar); err != nil {
		return err
	}
	if agentVar == "" {
		return nil
	}
	return g.checkScope(u, branch, defaults[0], r)
}

// managedRepo reports whether the pushing repository runs the Gas Town
// managed hook. Unknown (no probe) counts as managed: fail closed.
func (g *prePushGuard) managedRepo() bool {
	if g.isManaged == nil {
		return true
	}
	return g.isManaged()
}

// checkFastForward refuses an agent update of a merge target that is not a
// fast-forward. Rewinding integration/x to a commit already on the default
// branch would otherwise hide its old tip from the land guard (B1); no gt
// flow force-pushes the default branch or integration/*.
func (g *prePushGuard) checkFastForward(u prePushUpdate, branch string) error {
	if u.RemoteSHA == prePushZeroSHA {
		return nil // branch creation
	}
	if _, err := g.git.Rev(u.RemoteSHA + "^{commit}"); err != nil {
		return fmt.Errorf("BLOCKED: push to %s: the remote branch tip is not in this repository; "+
			"fetch first, then push again (fail closed)", branch)
	}
	ff, err := g.git.IsAncestor(u.RemoteSHA, u.LocalSHA)
	if err != nil {
		return fmt.Errorf("BLOCKED: push to %s: cannot verify fast-forward: %s (fail closed)", branch, firstLine(err.Error()))
	}
	if !ff {
		return fmt.Errorf("BLOCKED: push to %s is not a fast-forward; agents may not rewrite or rewind "+
			"merge target branches (default branch, integration/*)", branch)
	}
	return nil
}

func (g *prePushGuard) loadRigOnce() (*prePushRig, error) {
	if !g.rigLoaded {
		g.rig, g.rigErr = g.loadRig()
		g.rigLoaded = true
	}
	return g.rig, g.rigErr
}

// defaultBranches returns the branch names treated as the default branch; the
// first is the one used as merge base. The rig's default_branch when known;
// otherwise origin/HEAD for humans and, for agents (fail closed), also main
// and master.
func (g *prePushGuard) defaultBranches(r *prePushRig, agent bool) []string {
	if r != nil && r.defaultBranch != "" {
		return []string{r.defaultBranch}
	}
	out := []string{g.git.RemoteDefaultBranch()}
	if agent {
		for _, b := range []string{"main", "master"} {
			if !containsString(out, b) {
				out = append(out, b)
			}
		}
	}
	return out
}

func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// errInvalidPushRemote: an empty or option-looking remote/URL (L1).
var errInvalidPushRemote = errors.New("invalid push remote")

// pushTarget is the repository git pushes to (URL when known), validated so
// it cannot be taken for an option.
func (g *prePushGuard) pushTarget() (string, error) {
	target := g.url
	if target == "" {
		target = g.remote
	}
	if target == "" || strings.HasPrefix(target, "-") {
		return "", errInvalidPushRemote
	}
	return target, nil
}

// errPushRemoteUnreadable: ls-remote of the push remote failed (the URL is
// never shown: it may carry credentials).
var errPushRemoteUnreadable = errors.New("cannot read the push remote; retry when it is reachable")

// listRemote lists refs under prefix on the push remote, once per prefix per
// push (mergeproof.CachedLister over readRemote).
func (g *prePushGuard) listRemote(prefix string) ([]git.RemoteRef, error) {
	if g.remoteList == nil {
		g.remoteList = mergeproof.CachedLister(g.readRemote)
	}
	return g.remoteList(prefix)
}

// readRemote is one ls-remote of the push remote. An invalid remote is
// errInvalidPushRemote; a failed read is errPushRemoteUnreadable.
func (g *prePushGuard) readRemote(prefix string) ([]git.RemoteRef, error) {
	target, err := g.pushTarget()
	if err != nil {
		return nil, err
	}
	refs, err := g.git.ListRemoteRefsWithHashes(target, prefix)
	if err != nil {
		return nil, errPushRemoteUnreadable
	}
	return refs, nil
}

// checkAgentDelete allows an agent to delete integration/<x> only once its
// tip is already contained in the remote default branch, and never the
// default branch itself.
func (g *prePushGuard) checkAgentDelete(u prePushUpdate, branch string, defaults []string) error {
	if containsString(defaults, branch) {
		return fmt.Errorf("BLOCKED: agents may not delete the default branch %s", branch)
	}
	refuse := func(why string) error {
		return fmt.Errorf("BLOCKED: agent deletion of %s refused: %s. Integration branches are "+
			"deleted by gt mq integration land after landing, or by a human", branch, why)
	}
	defaultSHA, err := g.tipResolver(defaults).BranchTip(defaults[0])
	if err != nil {
		return refuse(err.Error())
	}
	if defaultSHA == "" {
		return refuse("default branch " + defaults[0] + " not found on the push remote")
	}
	for _, sha := range []string{u.RemoteSHA, defaultSHA} {
		if _, err := g.git.Rev(sha + "^{commit}"); err != nil {
			return refuse("the remote tips are not in this repository; fetch first")
		}
	}
	landed, err := g.git.IsAncestor(u.RemoteSHA, defaultSHA)
	if err != nil {
		return refuse("cannot verify ancestry: " + firstLine(err.Error()))
	}
	if !landed {
		return refuse("it has not landed on " + defaults[0])
	}
	return nil
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
	if errors.Is(err, errInvalidPushRemote) {
		return verifyError(branch, err.Error(), agentVar)
	}
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

// integrationTips lists integration/* tips on the push remote plus the local
// integration/* branches and remote-tracking refs, so a branch deleted on the
// remote still counts while this repository knows it. Names are normalised to
// refs/heads/integration/<x>.
func (g *prePushGuard) integrationTips() ([]git.RemoteRef, error) {
	if g.tipsDone {
		return g.tips, g.tipsErr
	}
	g.tipsDone = true
	tips, err := g.listRemote("refs/heads/" + integrationBranchPrefix)
	if err != nil {
		g.tipsErr = err
		return nil, err
	}
	remotes := []string{"origin"}
	if g.remote != "" && g.remote != "origin" && !strings.ContainsAny(g.remote, "/:\\") && !strings.HasPrefix(g.remote, "-") {
		remotes = append(remotes, g.remote)
	}
	seen := map[string]bool{}
	for _, t := range tips {
		seen[t.Name+" "+t.Hash] = true
	}
	prefixes := []string{"refs/heads/"} // local integration branches
	for _, name := range remotes {
		prefixes = append(prefixes, "refs/remotes/"+name+"/")
	}
	for _, prefix := range prefixes {
		local, err := g.git.ListLocalRefsWithHashes(prefix + integrationBranchPrefix)
		if err != nil {
			continue // tracking refs are defense in depth only
		}
		for _, t := range local {
			t.Name = "refs/heads/" + strings.TrimPrefix(t.Name, prefix)
			if !seen[t.Name+" "+t.Hash] {
				seen[t.Name+" "+t.Hash] = true
				tips = append(tips, t)
			}
		}
	}
	g.tips = tips
	return tips, nil
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
		return nil // unmanaged repo outside a rig: no scope declared
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

// newBranchBase returns the merge-base of local with the default branch tip
// on the push remote (ls-remote, not a local ref that could be moved).
func (g *prePushGuard) newBranchBase(local, defaultBranch string) (string, error) {
	sha, err := g.tipResolver([]string{defaultBranch}).BranchTip(defaultBranch)
	if err != nil {
		return "", fmt.Errorf("%v; cannot check merge scope", err)
	}
	if sha == "" {
		return "", fmt.Errorf("default branch %s not found on the push remote; cannot check merge scope", defaultBranch)
	}
	if _, err := g.git.Rev(sha + "^{commit}"); err != nil {
		return "", fmt.Errorf("the remote %s tip is not in this repository; fetch first, then push again", defaultBranch)
	}
	mb, err := g.git.MergeBase(sha, local)
	if err != nil {
		return "", fmt.Errorf("no merge-base with %s; cannot check merge scope", defaultBranch)
	}
	return mb, nil
}
