package git

import (
	"fmt"
	"strings"
)

// Merge shape read (gt-pd1c.78 tahap 3): the tree git's own merge machinery
// builds for two commits. With CommitParents (landing_proof.go) the pre-push
// guard (rule 2(c)) recognizes the one merge shape the merge engine pushes: a
// two-parent merge whose tree is exactly the clean merge of its parents.

// MergeTreeWrite returns the tree of the clean merge of a and b as git's
// merge machinery builds it (`git merge-tree --write-tree`, git >= 2.38). A
// conflicted merge is an error: it has no single tree. It runs like the
// proofs (proofRun: replace refs, grafts and the commit-graph ignored), so a
// local replace ref cannot change what is merged.
func (g *Git) MergeTreeWrite(a, b string) (string, error) {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if a == "" || b == "" || strings.HasPrefix(a, "-") || strings.HasPrefix(b, "-") {
		return "", fmt.Errorf("invalid merge-tree arguments %q %q", a, b)
	}
	out, err := g.proofRun("merge-tree", "--write-tree", "--no-messages", "--end-of-options", a, b)
	if err != nil {
		return "", fmt.Errorf("merge-tree %.12s %.12s: %w", a, b, err)
	}
	// Without conflicts the output is exactly the tree object name.
	fields := strings.Fields(out)
	if len(fields) != 1 {
		return "", fmt.Errorf("merge-tree %.12s %.12s: unexpected output", a, b)
	}
	return fields[0], nil
}

// CommitTree returns the tree commit records, read like the proofs
// (proofRun: replace refs, grafts and the commit-graph ignored), so a local
// `git replace` cannot show a different tree than the object a push sends.
func (g *Git) CommitTree(commit string) (string, error) {
	commit = strings.TrimSpace(commit)
	if commit == "" || strings.HasPrefix(commit, "-") {
		return "", fmt.Errorf("invalid commit %q", commit)
	}
	out, err := g.proofRun("rev-parse", "--verify", "--end-of-options", commit+"^{tree}")
	if err != nil {
		return "", fmt.Errorf("reading the tree of %.12s: %w", commit, err)
	}
	return strings.TrimSpace(out), nil
}
