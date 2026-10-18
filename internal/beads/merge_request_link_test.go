package beads

import (
	"errors"
	"testing"
)

func TestMRBackLink(t *testing.T) {
	if got := MRBackLinkComment(" kp-wisp-6lwd "); got != "MR created: kp-wisp-6lwd" {
		t.Fatalf("MRBackLinkComment = %q", got)
	}
	comments := []Comment{
		{Author: "posin/polecats/institute", Text: "MR created: kp-wisp-6lwd"},
		{Author: "someone", Text: "MR created: kp-wisp-other trailing words"},
	}
	if !HasMRBackLink(comments, "kp-wisp-6lwd") {
		t.Fatal("the gt done back-link was not recognised")
	}
	for _, mr := range []string{"kp-wisp-other", "kp-wisp", "", "kp-wisp-6lw"} {
		if HasMRBackLink(comments, mr) {
			t.Fatalf("HasMRBackLink(%q) = true: only an exact back-link binds", mr)
		}
	}
	if HasMRBackLink(nil, "kp-wisp-6lwd") {
		t.Fatal("no comments: no back-link")
	}
}

func TestOpenMergeRequestFor(t *testing.T) {
	const commit = "20880ee42027a38931367af922f64377b36d2d9c"
	mr := func(id, status, target, rig, sha string) *Issue {
		return &Issue{ID: id, Status: status, Description: "branch: polecat/x/kp-1\ntarget: " + target +
			"\nsource_issue: kp-1\nrig: " + rig + "\ncommit_sha: " + sha}
	}
	q := NewMergeQueue(MergeQueueIndex{
		"kp-1": {
			mr("kp-c", "closed", "main", "posin", commit),
			mr("kp-b", "open", "integration/epic", "posin", commit),
			mr("kp-o", "open", "main", "other", commit),
		},
		"kp-2": {mr("kp-a", "open", "main", "posin", "ffff"+commit[4:])},
		"kp-3": {mr("kp-n", "open", "integration/norig", "", commit)},
	}, nil, nil)

	if id, err := q.OpenMergeRequestFor("posin", "main", commit); err != nil || id != "" {
		t.Fatalf("closed, other-target and other-rig MRs must not count: id=%q err=%v", id, err)
	}
	if id, err := q.OpenMergeRequestFor("posin", "refs/heads/integration/epic", commit); err != nil || id != "kp-b" {
		t.Fatalf("open MR for integration/epic: id=%q err=%v", id, err)
	}
	if id, err := q.OpenMergeRequestFor("other", "main", commit); err != nil || id != "kp-o" {
		t.Fatalf("open MR of rig other: id=%q err=%v", id, err)
	}
	if id, err := q.OpenMergeRequestFor("posin", "integration/norig", commit); err != nil || id != "" {
		t.Fatalf("an MR that records no rig must not count: id=%q err=%v", id, err)
	}
	if _, err := q.OpenMergeRequestFor("", "main", commit); err == nil {
		t.Fatal("an empty rig must be refused (no wildcard)")
	}
	unread := NewMergeQueue(nil, errors.New("dolt down"), nil)
	if _, err := unread.OpenMergeRequestFor("posin", "main", commit); err == nil {
		t.Fatal("an unread queue must fail closed")
	}
	if _, err := q.OpenMergeRequestFor("posin", "", commit); err == nil {
		t.Fatal("an empty target must be refused")
	}
}
