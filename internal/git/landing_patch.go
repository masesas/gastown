package git

import (
	"fmt"
	"strconv"
	"strings"
)

// comparePatches is the patch-id decision of PatchEquivalentOnPushTarget for
// objects already known to be present (gt-pd1c.81). The target side is
// `tip ^commit` (and `^base` when base is set), the work side `commit ^tip`.
func (g *Git) comparePatches(tip, commit, base string) (bool, error) {
	targetRange := []string{tip, "^" + commit}
	if base != "" {
		targetRange = append(targetRange, "^"+base)
	}
	workRange := []string{commit, "^" + tip}
	for _, rng := range [][]string{targetRange, workRange} {
		if err := g.checkScanSize(rng); err != nil {
			return false, err
		}
	}
	onTarget, err := g.patchIDs(logPatchArgs(targetRange)...)
	if err != nil {
		return false, err
	}
	targetSet := make(map[string]bool, len(onTarget))
	for _, id := range onTarget {
		targetSet[id] = true
	}
	work, err := g.patchIDs(logPatchArgs(workRange)...)
	if err != nil {
		return false, err
	}
	mergeBase, err := g.proofRun("merge-base", "--end-of-options", tip, commit)
	if err != nil {
		return false, fmt.Errorf("patch_check_failed: no merge base of %s and %s: %w", shortSHA(tip), shortSHA(commit), err)
	}
	diffArgs := append(append([]string{"diff"}, patchDiffOptions()...), "--end-of-options", mergeBase, commit, "--")
	combined, err := g.patchIDs(diffArgs...)
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
	}
	return false, g.subjectSignal(targetRange, workRange, commit)
}

// checkScanSize fails when rng holds more than maxPatchScanCommits non-merge
// commits, or when the count cannot be read.
func (g *Git) checkScanSize(rng []string) error {
	args := append([]string{"rev-list", "--count", "--no-merges", "--end-of-options"}, rng...)
	n, err := g.proofRun(args...)
	if err != nil {
		return fmt.Errorf("patch_check_failed: unable to count commits: %w", err)
	}
	count, err := strconv.Atoi(n)
	if err != nil {
		return fmt.Errorf("patch_check_failed: unexpected commit count %q from git rev-list", n)
	}
	if count > maxPatchScanCommits {
		return fmt.Errorf("patch_check_failed: %d commits to compare, more than the limit of %d", count, maxPatchScanCommits)
	}
	return nil
}

// subjectSignal is the cheap companion of a "no equivalent patch" answer: a
// conflict-resolved rebase keeps the commit subjects, and a squash usually
// lists them in its message, while the patch-ids change. If any subject of
// the work side appears as a line of a target commit message, the answer is
// left open (error); otherwise nil confirms the definite "not landed".
func (g *Git) subjectSignal(targetRange, workRange []string, commit string) error {
	subjects, err := g.proofRun(append([]string{"log", "--no-merges", "--format=%s", "--end-of-options"}, append(workRange, "--")...)...)
	if err != nil {
		return fmt.Errorf("patch_check_failed: unable to read commit subjects: %w", err)
	}
	messages, err := g.proofRun(append([]string{"log", "--format=%B", "--end-of-options"}, append(targetRange, "--")...)...)
	if err != nil {
		return fmt.Errorf("patch_check_failed: unable to read target commit messages: %w", err)
	}
	lines := make(map[string]bool)
	for _, line := range strings.Split(messages, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			lines[line] = true
		}
	}
	for _, subject := range strings.Split(subjects, "\n") {
		if subject = strings.TrimSpace(subject); subject != "" && lines[subject] {
			return fmt.Errorf("patch_check_failed: no equivalent patch, but a target commit carries the subject %q of %s (conflict-resolved rebase or squash?)", subject, shortSHA(commit))
		}
	}
	return nil
}

// patchDiffOptions pins the diff output both sides are hashed from, so that
// user configuration cannot make equal changes hash differently.
func patchDiffOptions() []string {
	return []string{"--no-color", "--no-ext-diff", "--no-textconv", "--no-renames"}
}

// logPatchArgs lists the non-merge commits of rng with their patches, in the
// form `git patch-id` reads.
func logPatchArgs(rng []string) []string {
	args := append([]string{"log", "-p", "--no-merges", "--format=commit %H"}, patchDiffOptions()...)
	args = append(args, "--end-of-options")
	args = append(args, rng...)
	return append(args, "--")
}

// patchIDs runs the given git command and returns the stable patch-ids of
// the patches in its output (one per commit, or one for a plain diff; none
// for empty patches).
func (g *Git) patchIDs(args ...string) ([]string, error) {
	patches, err := g.proofRaw(nil, args...)
	if err != nil {
		return nil, fmt.Errorf("patch_check_failed: git %s: %w", args[0], err)
	}
	if strings.TrimSpace(patches) == "" {
		return nil, nil
	}
	out, err := g.proofRaw(&patches, "patch-id", "--stable")
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
