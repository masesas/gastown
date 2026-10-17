package git

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/steveyegge/gastown/internal/util"
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
)

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
// always sound, so each check here can only err towards "undecidable". why
// names the reason when the answer is not definite.
func (g *Git) ancestryDecidable(tip, commit string) (bool, string) {
	out, err := g.run("rev-parse", "--is-shallow-repository")
	if err != nil {
		return false, "unverifiable (shallow check failed)"
	}
	if strings.TrimSpace(out) != "true" {
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
		reachable, err := g.IsAncestor(b, tip)
		if err != nil {
			return false, "shallow (boundary check failed)"
		}
		if !reachable {
			continue
		}
		below, err := g.IsAncestor(b, commit)
		if err != nil || !below {
			return false, "shallow (commit may predate the shallow boundary)"
		}
	}
	return true, ""
}

// shallowBoundaries lists the commits recorded in $GIT_DIR/shallow (the
// commits whose parents this clone does not have).
func (g *Git) shallowBoundaries() ([]string, error) {
	path, err := g.run("rev-parse", "--git-path", "shallow")
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
// uses them.
//
// (true, nil): an equivalent patch is on the branch. (false, nil): definitely
// none (the clone's history decides, both sides were compared completely).
// (false, err): cannot tell — the branch could not be read or fetched, an
// object is missing, a shallow clone cannot decide (see ancestryDecidable),
// only part of the work matches, the commit carries no diff, or the range
// exceeds maxPatchScanCommits. A conflict-resolved rebase or a squash whose
// context lines changed produces different patch-ids; such work still reads
// as (false, nil).
func (g *Git) PatchEquivalentOnPushTarget(remote, branch, commit string) (bool, error) {
	commit = strings.TrimSpace(commit)
	if commit == "" {
		return false, fmt.Errorf("patch_check_failed: empty commit for %s/%s", remote, branch)
	}
	tip, err := g.PushRemoteBranchTip(remote, branch)
	if err != nil {
		return false, fmt.Errorf("patch_check_failed: unable to read %s/%s: %w", remote, branch, err)
	}
	if tip == "" {
		return false, fmt.Errorf("patch_check_failed: branch %s/%s missing", remote, branch)
	}
	// The reachability check that runs first has usually fetched this tip
	// already; fetch only when its object is missing.
	if have, err := g.commitObjectExists(tip); err != nil || !have {
		if _, err := g.run("fetch", "--no-tags", g.pushTarget(remote), "refs/heads/"+branch); err != nil {
			return false, fmt.Errorf("patch_check_failed: unable to fetch %s/%s: %w", remote, branch, err)
		}
	}
	for _, obj := range []string{tip, commit} {
		exists, err := g.commitObjectExists(obj)
		if err != nil {
			return false, fmt.Errorf("patch_check_failed: unable to look up commit %s: %w", shortSHA(obj), err)
		}
		if !exists {
			return false, fmt.Errorf("patch_check_failed: commit %s absent from the clone; cannot compare patches", shortSHA(obj))
		}
	}
	if decidable, why := g.ancestryDecidable(tip, commit); !decidable {
		return false, fmt.Errorf("patch_check_failed: %s clone cannot decide whether %s is on %s/%s", why, shortSHA(commit), remote, branch)
	}
	return g.comparePatches(tip, commit)
}

// comparePatches implements the patch-id decision of
// PatchEquivalentOnPushTarget for objects already known to be present.
func (g *Git) comparePatches(tip, commit string) (bool, error) {
	for _, rng := range [][2]string{{tip, commit}, {commit, tip}} {
		n, err := g.run("rev-list", "--count", "--no-merges", rng[0], "^"+rng[1])
		if err != nil {
			return false, fmt.Errorf("patch_check_failed: unable to count commits: %w", err)
		}
		if count, err := strconv.Atoi(strings.TrimSpace(n)); err != nil || count > maxPatchScanCommits {
			return false, fmt.Errorf("patch_check_failed: more than %d commits to compare", maxPatchScanCommits)
		}
	}
	onTarget, err := g.patchIDs(logPatchArgs(tip, commit)...)
	if err != nil {
		return false, err
	}
	targetSet := make(map[string]bool, len(onTarget))
	for _, id := range onTarget {
		targetSet[id] = true
	}
	work, err := g.patchIDs(logPatchArgs(commit, tip)...)
	if err != nil {
		return false, err
	}
	base, err := g.MergeBase(tip, commit)
	if err != nil {
		return false, fmt.Errorf("patch_check_failed: no merge base of %s and %s: %w", shortSHA(tip), shortSHA(commit), err)
	}
	combined, err := g.patchIDs(append([]string{"diff"}, append(patchDiffOptions(), base, commit)...)...)
	if err != nil {
		return false, err
	}

	if len(combined) == 1 && targetSet[combined[0]] {
		return true, nil // squash merge
	}
	matched := 0
	for _, id := range work {
		if targetSet[id] {
			matched++
		}
	}
	switch {
	case len(work) > 0 && matched == len(work):
		return true, nil // rebase / cherry-pick
	case len(work) == 0 && len(combined) == 0:
		return false, fmt.Errorf("patch_check_failed: commit %s carries no diff to compare", shortSHA(commit))
	case matched > 0:
		return false, fmt.Errorf("patch_check_failed: only %d of %d commits of %s have an equivalent on the target", matched, len(work), shortSHA(commit))
	default:
		return false, nil
	}
}

// patchDiffOptions pins the diff output both sides are hashed from, so that
// user configuration cannot make equal changes hash differently.
func patchDiffOptions() []string {
	return []string{"--no-color", "--no-ext-diff", "--no-textconv", "--no-renames"}
}

// logPatchArgs lists the non-merge commits of include that are not in
// exclude, with their patches, in the form `git patch-id` reads.
func logPatchArgs(include, exclude string) []string {
	args := []string{"log", "-p", "--no-merges", "--format=commit %H"}
	args = append(args, patchDiffOptions()...)
	return append(args, include, "^"+exclude, "--")
}

// patchIDs runs the given git command and returns the stable patch-ids of
// the patches in its output (one per commit, or one for a plain diff; none
// for empty patches).
func (g *Git) patchIDs(args ...string) ([]string, error) {
	patches, err := g.runRaw(args...)
	if err != nil {
		return nil, fmt.Errorf("patch_check_failed: git %s: %w", args[0], err)
	}
	if strings.TrimSpace(patches) == "" {
		return nil, nil
	}
	out, err := g.runRawStdin(patches, "patch-id", "--stable")
	if err != nil {
		return nil, fmt.Errorf("patch_check_failed: git patch-id: %w", err)
	}
	var ids []string
	for _, line := range strings.Split(out, "\n") {
		if fields := strings.Fields(line); len(fields) >= 1 {
			ids = append(ids, fields[0])
		}
	}
	return ids, nil
}

// runRawStdin is runRaw with stdin.
func (g *Git) runRawStdin(stdin string, args ...string) (string, error) {
	if err := g.guardUnsafeTownRootMutation(args); err != nil {
		return "", err
	}
	if g.gitDir != "" {
		args = append([]string{"--git-dir=" + g.gitDir}, args...)
	}
	cmd := exec.Command("git", args...)
	util.SetDetachedProcessGroup(cmd)
	if g.workDir != "" {
		cmd.Dir = g.workDir
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdin = strings.NewReader(stdin)
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", g.wrapError(err, stdout.String(), stderr.String(), args)
	}
	return stdout.String(), nil
}
