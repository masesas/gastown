package cmd

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
	gitpkg "github.com/steveyegge/gastown/internal/git"
)

// gt-pd1c.78 tahap 2: gt polecat abandon is the one way an agent discards
// polecat work that is not on a merge target: archive the tip on the remote,
// verify the archive, then delete the branch, then record it on the beads.

type abandonFixture struct {
	f      prePushFixture
	repo   *gitpkg.Git
	branch string
	notes  map[string]string
	at     time.Time
}

func newAbandonFixture(t *testing.T) *abandonFixture {
	t.Helper()
	a := &abandonFixture{f: newPrePushFixture(t), branch: "polecat/nux/gt-1", notes: map[string]string{},
		at: time.Unix(1760000000, 0)}
	a.repo = gitpkg.NewGit(a.f.repo)
	runGitForMQSubmitTest(t, a.f.repo, "push", "-q", "origin", a.f.good+":refs/heads/"+a.branch)
	return a
}

func (a *abandonFixture) request(reason string) abandonRequest {
	return abandonRequest{rig: "gastown", branch: a.branch, reason: reason, actor: "mayor", now: a.at, defaultBranch: "main"}
}

func (a *abandonFixture) record(id, note string) error {
	a.notes[id] = note
	return nil
}

func (a *abandonFixture) remoteRef(t *testing.T, ref string) string {
	t.Helper()
	refs, err := a.repo.ListRemoteRefsWithHashes("origin", ref)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range refs {
		if r.Name == ref {
			return r.Hash
		}
	}
	return ""
}

func (a *abandonFixture) archives(t *testing.T) int {
	t.Helper()
	refs, err := a.repo.ListRemoteRefsWithHashes("origin", "refs/gt/archive/")
	if err != nil {
		t.Fatal(err)
	}
	return len(refs)
}

func TestAbandonPolecatBranchArchivesVerifiesDeletesRecords(t *testing.T) {
	for _, verdict := range []string{"rejected", "superseded"} {
		t.Run(verdict, func(t *testing.T) {
			a := newAbandonFixture(t)
			queue := mqWithProver(gitLanded{}, mrIssue("gt-mr1", "closed", a.branch, a.f.good, "close_reason: "+verdict+"\n"))
			res, err := abandonPolecatBranch(a.request("superseded by\n gt-mr9; token=https://u:secret@host/x"), a.repo, queue, a.record)
			if err != nil {
				t.Fatalf("abandon: %v", err)
			}
			want := "refs/gt/archive/" + a.branch + "/1760000000-" + a.f.good[:12]
			if res.ArchiveRef != want || res.Tip != a.f.good || res.MRID != "gt-mr1" {
				t.Fatalf("result = %+v, want archive %s", res, want)
			}
			if got := a.remoteRef(t, want); got != a.f.good {
				t.Fatalf("archive ref on the remote = %q, want the tip", got)
			}
			if got := a.remoteRef(t, "refs/heads/"+a.branch); got != "" {
				t.Fatalf("branch still on the remote at %s", got)
			}
			for _, id := range []string{"gt-mr1", "gt-src-gt-mr1"} {
				note := a.notes[id]
				for _, must := range []string{want, a.f.good, "mayor", "superseded by gt-mr9"} {
					if !strings.Contains(note, must) {
						t.Fatalf("note on %s must contain %q: %q", id, must, note)
					}
				}
				if strings.Contains(note, "secret") || strings.Contains(note, "\n") {
					t.Fatalf("note on %s must be one redacted line: %q", id, note)
				}
			}
		})
	}
}

func TestAbandonPolecatBranchRefusals(t *testing.T) {
	cases := []struct {
		name    string
		mutate  func(a *abandonFixture, r *abandonRequest)
		queue   func(a *abandonFixture) func() *beads.MergeQueue
		wantErr string
	}{
		{name: "conflict: rework, not abandon", wantErr: "closed for conflicts",
			queue: func(a *abandonFixture) func() *beads.MergeQueue {
				return mqWithProver(gitLanded{}, mrIssue("gt-mr1", "closed", a.branch, a.f.good, "close_reason: conflict\n"))
			}},
		{name: "no MR: resubmit", wantErr: "no MR recorded this tip",
			queue: func(a *abandonFixture) func() *beads.MergeQueue { return mqWith() }},
		{name: "raw close: rework", wantErr: "without an official verdict",
			queue: func(a *abandonFixture) func() *beads.MergeQueue {
				return mqWithProver(gitLanded{}, mrIssue("gt-mr1", "closed", a.branch, a.f.good, ""))
			}},
		{name: "open MR: the refinery owns it", wantErr: "is open",
			queue: func(a *abandonFixture) func() *beads.MergeQueue {
				return mqWith(mrIssue("gt-mr1", "open", a.branch, a.f.good, ""))
			}},
		{name: "unverified: overseer", wantErr: "cannot tell",
			queue: func(a *abandonFixture) func() *beads.MergeQueue {
				return mqWith(mrIssue("gt-mr1", "closed", a.branch, a.f.good, "close_reason: rejected\n"))
			}},
		{name: "unreadable queue: fail closed", wantErr: "cannot read the merge queue",
			queue: func(a *abandonFixture) func() *beads.MergeQueue {
				return func() *beads.MergeQueue { return beads.NewMergeQueue(nil, errors.New("dolt down"), nil) }
			}},
		{name: "tip already on a merge target: nothing to abandon", wantErr: "already on a merge target",
			mutate: func(a *abandonFixture, r *abandonRequest) {
				runGitForMQSubmitTest(t, a.f.repo, "push", "-q", "--force", "origin", a.f.mainSHA+":refs/heads/"+a.branch)
			}},
		{name: "branch missing on the remote", wantErr: "not on the remote",
			mutate: func(a *abandonFixture, r *abandonRequest) { r.branch = "polecat/ghost/gt-2" }},
		{name: "not a polecat branch", wantErr: "not a polecat branch",
			mutate: func(a *abandonFixture, r *abandonRequest) { r.branch = "main" }},
		{name: "unsafe branch name", wantErr: "not a polecat branch",
			mutate: func(a *abandonFixture, r *abandonRequest) { r.branch = "polecat/nux;rm -rf" }},
		{name: "path escape", wantErr: "not a polecat branch",
			mutate: func(a *abandonFixture, r *abandonRequest) { r.branch = "polecat/../main" }},
		{name: "unsafe rig", wantErr: "invalid rig",
			mutate: func(a *abandonFixture, r *abandonRequest) { r.rig = "../x" }},
		{name: "empty reason", wantErr: "--reason is required",
			mutate: func(a *abandonFixture, r *abandonRequest) { r.reason = " \n " }},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			a := newAbandonFixture(t)
			req := a.request("not needed")
			if c.mutate != nil {
				c.mutate(a, &req)
			}
			queue := mqWithProver(gitLanded{}, mrIssue("gt-mr1", "closed", a.branch, a.f.good, "close_reason: rejected\n"))
			if c.queue != nil {
				queue = c.queue(a)
			}
			before := a.remoteRef(t, "refs/heads/"+a.branch)
			_, err := abandonPolecatBranch(req, a.repo, queue, a.record)
			if err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("want refusal containing %q, got %v", c.wantErr, err)
			}
			if n := a.archives(t); n != 0 {
				t.Fatalf("a refusal must not archive anything (%d archive refs)", n)
			}
			if after := a.remoteRef(t, "refs/heads/"+a.branch); after != before {
				t.Fatalf("a refusal must not touch the branch (%q -> %q)", before, after)
			}
			if len(a.notes) != 0 {
				t.Fatalf("a refusal must not record anything: %v", a.notes)
			}
		})
	}
}

// abandonGitFault wraps the real git: push or delete can be made to fail, or
// push can silently do nothing (an archive that is then not on the remote).
type abandonGitFault struct {
	*gitpkg.Git
	pushErr, deleteErr error
	pushNoop           bool
}

func (g abandonGitFault) Push(remote, refspec string, force bool) error {
	if g.pushErr != nil {
		return g.pushErr
	}
	if g.pushNoop {
		return nil
	}
	return g.Git.Push(remote, refspec, force)
}

func (g abandonGitFault) DeleteRemoteBranchIfAt(remote, branch, expected string) error {
	if g.deleteErr != nil {
		return g.deleteErr
	}
	return g.Git.DeleteRemoteBranchIfAt(remote, branch, expected)
}

// Order: a failed or unverified archive deletes nothing; a failed delete
// leaves the archive (retry is safe).
func TestAbandonPolecatBranchOrder(t *testing.T) {
	rejected := func(a *abandonFixture) func() *beads.MergeQueue {
		return mqWithProver(gitLanded{}, mrIssue("gt-mr1", "closed", a.branch, a.f.good, "close_reason: rejected\n"))
	}
	t.Run("archive push fails: branch kept", func(t *testing.T) {
		a := newAbandonFixture(t)
		_, err := abandonPolecatBranch(a.request("r"), abandonGitFault{Git: a.repo, pushErr: errors.New("hook said no")}, rejected(a), a.record)
		if err == nil || !strings.Contains(err.Error(), "nothing was deleted") || a.remoteRef(t, "refs/heads/"+a.branch) != a.f.good {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("archive not on the remote: branch kept", func(t *testing.T) {
		a := newAbandonFixture(t)
		_, err := abandonPolecatBranch(a.request("r"), abandonGitFault{Git: a.repo, pushNoop: true}, rejected(a), a.record)
		if err == nil || !strings.Contains(err.Error(), "could not be verified") || a.remoteRef(t, "refs/heads/"+a.branch) != a.f.good {
			t.Fatalf("err=%v", err)
		}
	})
	t.Run("delete fails: archive kept, retry advice", func(t *testing.T) {
		a := newAbandonFixture(t)
		_, err := abandonPolecatBranch(a.request("r"), abandonGitFault{Git: a.repo, deleteErr: errors.New("stale info")}, rejected(a), a.record)
		if err == nil || !strings.Contains(err.Error(), "archived at refs/gt/archive/") || a.archives(t) != 1 || len(a.notes) != 0 {
			t.Fatalf("err=%v archives=%d notes=%v", err, a.archives(t), a.notes)
		}
	})
	t.Run("recording fails: abandoned, error reported", func(t *testing.T) {
		a := newAbandonFixture(t)
		res, err := abandonPolecatBranch(a.request("r"), a.repo, rejected(a), func(string, string) error { return errors.New("dolt down") })
		if err != nil || res.RecordErr == nil || a.remoteRef(t, "refs/heads/"+a.branch) != "" {
			t.Fatalf("err=%v res=%+v", err, res)
		}
	})
}

func TestResolveAbandonTarget(t *testing.T) {
	cases := []struct {
		addr, rig, rest string
		ok              bool
	}{
		{"gastown/polecat/nux/gt-1", "gastown", "polecat/nux/gt-1", true},
		{"gastown/nux", "gastown", "nux", true},
		{"gastown", "", "", false},
		{"/polecat/x", "", "", false},
		{"gastown/", "", "", false},
	}
	for _, c := range cases {
		rig, rest, err := splitAbandonAddress(c.addr)
		if (err == nil) != c.ok || rig != c.rig || rest != c.rest {
			t.Errorf("splitAbandonAddress(%q) = %q, %q, %v", c.addr, rig, rest, err)
		}
	}
}

// Git errors may carry a remote URL with a token: never shown raw.
func TestGitErrLineRedacts(t *testing.T) {
	got := gitErrLine(errors.New("fatal: unable to access 'https://bot:s3cret@example.com/x.git/'\nmore"))
	if strings.Contains(got, "s3cret") || strings.Contains(got, "more") {
		t.Fatalf("gitErrLine = %q", got)
	}
}
