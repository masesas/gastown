package git

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Landing proof helpers (gt-pd1c.81). They decide, from git alone, whether a
// merge request's recorded commit landed on its target. Every negative answer
// is definite only when the clone's history can support it; anything else is
// an error, which callers treat as "cannot tell" (fail closed).

const (
	// maxShallowBoundaries bounds ancestryDecidable; a clone with more
	// shallow boundaries is treated as unable to decide.
	maxShallowBoundaries = 64
	// maxPatchScanCommits bounds the commits PatchEquivalentOnPushTarget
	// compares on each side; a larger range is treated as unable to decide.
	maxPatchScanCommits = 2000
	// proofTimeout bounds every git command of the patch proof (the network
	// ones included); a command that runs longer makes the answer unknown.
	proofTimeout = 60 * time.Second
	// proofMaxOutput bounds the stdout of every git command of the patch
	// proof; larger output makes the answer unknown.
	proofMaxOutput = 16 << 20
)

// proofHistoryEnv makes every proof command read the commit graph as the
// objects themselves record it (gt-pd1c.86 H1). The remote's tips are honest
// (ls-remote), but ancestry is computed locally, and anyone who can write the
// repository can rewrite history views without touching an object:
// refs/replace/* (git replace), info/grafts and a forged commit-graph file
// can make a remote tip "contain" a commit that was never pushed. Replace
// refs and grafts are switched off and the commit-graph file is not read
// (command-scope config, which outranks the repository's own). This is the
// one place: proofLocalEnv, proofRemoteOptions and so WithProofLimits (the
// merge-queue provers, the witness reconciler) and PreservedOn all use it.
//
// GIT_CONFIG_PARAMETERS is emptied: an inherited one (git -c … running a hook
// that calls gt) outranks GIT_CONFIG_COUNT and could turn commit-graph back on.
var proofHistoryEnv = []string{
	"GIT_NO_REPLACE_OBJECTS=1",
	"GIT_CONFIG_PARAMETERS=",
	"GIT_GRAFT_FILE=" + os.DevNull,
	"GIT_CONFIG_COUNT=1",
	"GIT_CONFIG_KEY_0=core.commitGraph",
	"GIT_CONFIG_VALUE_0=false",
}

// proofLocalEnv keeps local proof commands from reaching the network: in a
// partial clone a missing object is an unknown answer, never a lazy fetch.
var proofLocalEnv = append([]string{"GIT_NO_LAZY_FETCH=1", "GIT_TERMINAL_PROMPT=0"}, proofHistoryEnv...)

// proofLocalOptions bound a local git command of a proof (timeout, output
// limit, no lazy fetch, honest history).
func proofLocalOptions() runOptions {
	return runOptions{timeout: proofTimeout, maxOutput: proofMaxOutput, env: proofLocalEnv}
}

// withLocalProofLimits returns a copy of g whose every command runs as a
// local proof command (proofLocalOptions): PreservedOn's ancestry, merge-tree
// and cherry checks.
func (g *Git) withLocalProofLimits() *Git {
	c := *g
	c.limits = proofLocalOptions()
	return &c
}

// proofRaw runs a local git command of the landing proof (timeout, output
// limit, no lazy fetch) with optional stdin and returns stdout byte-exact.
func (g *Git) proofRaw(stdin *string, args ...string) (string, error) {
	opts := proofLocalOptions()
	opts.stdin = stdin
	return g.runCmd(opts, args...)
}

// proofRun is proofRaw without stdin, with whitespace trimmed.
func (g *Git) proofRun(args ...string) (string, error) {
	out, err := g.proofRaw(nil, args...)
	return strings.TrimSpace(out), err
}

// proofRemoteOptions bound a git command of the landing proof that may use
// the network (ls-remote, fetch, and every command of a WithProofLimits Git).
func proofRemoteOptions() runOptions {
	return runOptions{timeout: proofTimeout, maxOutput: proofMaxOutput, env: append([]string{"GIT_TERMINAL_PROMPT=0"}, proofHistoryEnv...)}
}

// proofRemote runs a network git command of the landing proof (ls-remote,
// fetch): bounded like proofRun, but objects may be fetched.
func (g *Git) proofRemote(args ...string) (string, error) {
	out, err := g.runCmd(proofRemoteOptions(), args...)
	return strings.TrimSpace(out), err
}

// WithProofLimits returns a copy of g whose every command runs under the
// landing proof's bounds (proofTimeout, proofMaxOutput, no terminal prompt):
// the merge-queue provers use it so that a patrol can never hang on git,
// including in VerifyPushedCommitReachableFromPushTarget. Callers that verify
// their own push (refinery, gt done) keep using an unbounded Git.
func (g *Git) WithProofLimits() *Git {
	c := *g
	c.limits = proofRemoteOptions()
	return &c
}

// HasProofLimits reports whether g was made by WithProofLimits.
func (g *Git) HasProofLimits() bool { return g.limits.timeout > 0 }

// proofIsAncestor is IsAncestor under the proof limits.
func (g *Git) proofIsAncestor(ancestor, descendant string) (bool, error) {
	_, err := g.proofRun("merge-base", "--is-ancestor", "--end-of-options", ancestor, descendant)
	if err == nil {
		return true, nil
	}
	if isNotAncestorExit(err) {
		return false, nil
	}
	return false, err
}

// proofObjectExists is commitObjectExists under the proof limits (no lazy
// fetch: in a partial clone a missing object reads as absent).
func (g *Git) proofObjectExists(commit string) (bool, error) {
	_, err := g.proofRun("cat-file", "-e", "--end-of-options", commit+"^{commit}")
	if err == nil {
		return true, nil
	}
	if code, ok := gitExitCode(err); ok && code == 1 {
		return false, nil
	}
	return false, err
}

// ancestryDecidable reports whether "commit is not an ancestor of tip", as
// answered by `git merge-base --is-ancestor` in this clone, is definite.
//
// A complete or partial clone (partial clones filter blobs/trees, never
// commits) always decides. In a shallow clone the walk from tip stops at the
// shallow boundaries, so a path from tip to commit is hidden only if it runs
// through a boundary B that is reachable from tip and has commit among its
// (cut-off) ancestors. When every boundary reachable from tip is itself an
// ancestor of commit, commit cannot be below it (history is acyclic) and the
// answer is definite. Positive ancestry answers in the truncated graph are
// always sound, so each check here can only err towards "undecidable".
// Boundaries on the commit's side count too: with merges, commit's own
// history may be cut at another boundary, so "B is an ancestor of commit" can
// read false although it holds, which again only errs towards
// "undecidable". why names the reason when the answer is not definite.
func (g *Git) ancestryDecidable(tip, commit string) (bool, string) {
	out, err := g.proofRun("rev-parse", "--is-shallow-repository")
	if err != nil {
		return false, "unverifiable (shallow check failed)"
	}
	if out != "true" {
		return true, ""
	}
	boundaries, err := g.shallowBoundaries()
	if err != nil {
		return false, "shallow (boundary list unreadable)"
	}
	if len(boundaries) > maxShallowBoundaries {
		return false, "shallow (too many shallow boundaries)"
	}
	for _, b := range boundaries {
		reachable, err := g.proofIsAncestor(b, tip)
		if err != nil {
			return false, "shallow (boundary check failed)"
		}
		if !reachable {
			continue
		}
		below, err := g.proofIsAncestor(b, commit)
		if err != nil || !below {
			return false, "shallow (commit may predate the shallow boundary)"
		}
	}
	return true, ""
}

// shallowBoundaries lists the commits recorded in $GIT_DIR/shallow (the
// commits whose parents this clone does not have).
func (g *Git) shallowBoundaries() ([]string, error) {
	path, err := g.proofRun("rev-parse", "--git-path", "shallow")
	if err != nil {
		return nil, err
	}
	if !filepath.IsAbs(path) && g.workDir != "" {
		path = filepath.Join(g.workDir, path)
	}
	data, err := os.ReadFile(path) //nolint:gosec // path comes from git rev-parse
	if err != nil {
		return nil, err
	}
	var shas []string
	for _, line := range strings.Split(string(data), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			shas = append(shas, line)
		}
	}
	return shas, nil
}

// PatchEquivalentOnPushTarget reports whether the work of commit is on the
// push target branch as equivalent patches even though commit itself is not
// reachable from it: a rebase or cherry-pick (every non-merge commit of
// commit's side has a patch-id-equal commit on the branch) or a squash merge
// (the combined diff merge-base..commit is patch-id-equal to one commit on
// the branch). Patch-ids come from `git patch-id --stable`, as `git cherry`
// uses them. base is the MR's base_sha (target tip at submission) or "":
// when set, only target commits after base count, so an older commit that
// happens to carry the same (trivial or reverted) patch cannot prove landing.
//
// (true, nil): an equivalent patch is on the branch. (false, nil): definitely
// none — the clone's history decides, both sides were compared completely
// and no target commit message carries a subject of commit's side.
// (false, err): cannot tell — the branch could not be read or
// fetched, an object is missing (no lazy fetch), a shallow clone cannot
// decide (see ancestryDecidable), base is not an ancestor of the branch,
// only part of the work matches, the commit carries no diff, the range
// exceeds maxPatchScanCommits, a git command timed out or produced more than
// proofMaxOutput, or a target commit reuses an MR commit subject (a
// conflict-resolved rebase or a squash with changed context lines changes
// the patch-ids, so equal subjects keep the answer open).
func (g *Git) PatchEquivalentOnPushTarget(remote, branch, commit, base string) (bool, error) {
	commit, base = strings.TrimSpace(commit), strings.TrimSpace(base)
	if commit == "" {
		return false, fmt.Errorf("patch_check_failed: empty commit for %s/%s", remote, branch)
	}
	// Callers validate the inputs (beads.gitLandingVerdict); every command
	// below also ends option parsing before them (--end-of-options).
	pushTarget := g.pushTarget(remote)
	out, err := g.proofRemote("ls-remote", "--heads", "--end-of-options", pushTarget, "refs/heads/"+branch)
	if err != nil {
		return false, fmt.Errorf("patch_check_failed: unable to read %s/%s: %w", remote, branch, err)
	}
	tip := parseLSRemoteTip(out, branch)
	if tip == "" {
		return false, fmt.Errorf("patch_check_failed: branch %s/%s missing", remote, branch)
	}
	// The reachability check that runs first has usually fetched this tip
	// already; fetch only when its object is missing.
	if have, err := g.proofObjectExists(tip); err != nil || !have {
		if _, err := g.proofRemote("fetch", "--no-tags", "--end-of-options", pushTarget, "refs/heads/"+branch); err != nil {
			return false, fmt.Errorf("patch_check_failed: unable to fetch %s/%s: %w", remote, branch, err)
		}
	}
	if err := g.requireProofObjects(tip, commit, base); err != nil {
		return false, err
	}
	if decidable, why := g.ancestryDecidable(tip, commit); !decidable {
		return false, fmt.Errorf("patch_check_failed: %s clone cannot decide whether %s is on %s/%s", why, shortSHA(commit), remote, branch)
	}
	if base != "" {
		onBranch, err := g.proofIsAncestor(base, tip)
		if err != nil || !onBranch {
			return false, fmt.Errorf("patch_check_failed: base %s is not an ancestor of %s/%s (history rewritten?)", shortSHA(base), remote, branch)
		}
	}
	return g.comparePatches(tip, commit, base)
}

// requireProofObjects fails unless every non-empty object is in the clone.
func (g *Git) requireProofObjects(objs ...string) error {
	for _, obj := range objs {
		if obj == "" {
			continue
		}
		exists, err := g.proofObjectExists(obj)
		if err != nil {
			return fmt.Errorf("patch_check_failed: unable to look up commit %s: %w", shortSHA(obj), err)
		}
		if !exists {
			return fmt.Errorf("patch_check_failed: commit %s absent from the clone; cannot compare patches", shortSHA(obj))
		}
	}
	return nil
}

// CommitParents returns the parents of commit as the commit object records
// them (proofLocalOptions: replace refs, grafts and the commit-graph file are
// ignored, bounded, no lazy fetch), in order; an empty slice for a root
// commit. It is the one reader of parents for merge-shape decisions ("is M a
// two-parent merge of X?", gt-pd1c.86 R2-1); never read them with Rev("M^2").
// Any git failure (unknown object, not a commit, timeout) is an error, never
// "no such parent".
func (g *Git) CommitParents(commit string) ([]string, error) {
	commit = strings.TrimSpace(commit)
	if commit == "" || strings.HasPrefix(commit, "-") {
		return nil, fmt.Errorf("invalid commit %q", commit)
	}
	out, err := g.proofRun("rev-list", "--parents", "-n", "1", "--end-of-options", commit+"^{commit}")
	if err != nil {
		return nil, fmt.Errorf("reading parents of %s: %w", shortSHA(commit), err)
	}
	fields := strings.Fields(out)
	if len(fields) == 0 {
		return nil, fmt.Errorf("reading parents of %s: no commit", shortSHA(commit))
	}
	return fields[1:], nil
}
