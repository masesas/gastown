package beads

import (
	"errors"
	"strings"
)

// Links between a merge request and the records around it (gt-pd1c.78 tahap 3).

// mrBackLinkPrefix starts the comment gt done and gt mq submit add to a
// source bead when they create its MR (GH#2599).
const mrBackLinkPrefix = "MR created: "

// MRBackLinkComment is the back-link comment for MR mrID on its source bead.
// gt done and gt mq submit write it when they create the MR; the refinery
// closes a source "Merged in <mr>" only when the source carries it
// (HasMRBackLink). That catches stale or mistaken data (an MR whose
// source_issue names a bead it was not created for, an MR older than the
// back-link). It is defense in depth, not a guard against a bd writer: anyone
// who can write beads can add the comment too (binding that a bd writer cannot
// forge is stage 4).
func MRBackLinkComment(mrID string) string {
	return mrBackLinkPrefix + strings.TrimSpace(mrID)
}

// HasMRBackLink reports whether comments contain the back-link of MR mrID
// (MRBackLinkComment, compared after trimming surrounding space).
func HasMRBackLink(comments []Comment, mrID string) bool {
	mrID = strings.TrimSpace(mrID)
	if mrID == "" {
		return false
	}
	want := MRBackLinkComment(mrID)
	for _, c := range comments {
		if strings.TrimSpace(c.Text) == want {
			return true
		}
	}
	return false
}

// OpenMergeRequestFor returns the ID of an open gt:merge-request whose
// commit_sha is commit, whose target is target and whose rig is rig (an MR
// that records another rig, or no rig, does not count: gt done and gt mq
// submit always record it). "" when there is none. An unread queue is an error (fail closed). The pre-push
// guard uses it to accept a refinery merge only for work an open MR submitted
// (rule 2(c)).
func (q *MergeQueue) OpenMergeRequestFor(rig, target, commit string) (string, error) {
	if err := q.Err(); err != nil {
		return "", err
	}
	target = plainBranch(target)
	commit = strings.TrimSpace(commit)
	rig = strings.TrimSpace(rig)
	if target == "" || commit == "" || rig == "" {
		return "", errors.New("rig, target and commit are required")
	}
	var found string
	for _, mrs := range q.index {
		for _, mr := range mrs {
			if mr == nil || IssueStatus(strings.TrimSpace(mr.Status)) != StatusOpen || !mrCommitIs(mr, commit) {
				continue
			}
			fields := ParseMRFields(mr)
			if fields == nil || plainBranch(fields.Target) != target {
				continue
			}
			if r := strings.TrimSpace(fields.Rig); r == "" || !strings.EqualFold(r, rig) {
				continue
			}
			if found == "" || mr.ID < found {
				found = mr.ID
			}
		}
	}
	return found, nil
}
