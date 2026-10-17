package mergeproof

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/git"
)

// Remote tips of a rig repository (gt-pd1c.78): the one reader of a polecat
// branch's tip, the merge targets' tips and the archive refs on the remote,
// shared by the coordinator guards (pre-push, gt polecat nuke --force/remove
// --force/gc, gt polecat abandon) and the witness branch reconciler. It reads
// the remote (ls-remote), never local refs that an agent could move.

// IntegrationBranchPrefix is the prefix of integration (epic) branches, the
// merge targets besides the default branch.
const IntegrationBranchPrefix = "integration/"

// ArchiveRefPrefix is the namespace of abandoned polecat branch tips on the
// remote: refs/gt/archive/<branch>/<unix-seconds>-<short-sha>. Agents create
// archive refs only through gt polecat abandon; deleting or rewriting them
// (pruning) is for humans and crew.
const ArchiveRefPrefix = "refs/gt/archive/"

// archiveShortSHALen is the length of the object name prefix in an archive
// ref's stamp.
const archiveShortSHALen = 12

// archiveStamp is the last segment of an archive ref.
var archiveStamp = regexp.MustCompile(`^[0-9]{1,20}-[0-9a-f]{7,64}$`)

// RemoteLister lists refs on the remote whose name starts with prefix.
type RemoteLister func(prefix string) ([]git.RemoteRef, error)

// CachedLister wraps list so each prefix is read once (one patrol, one push).
// Errors are not cached: a later call retries.
func CachedLister(list RemoteLister) RemoteLister {
	cache := map[string][]git.RemoteRef{}
	return func(prefix string) ([]git.RemoteRef, error) {
		if refs, ok := cache[prefix]; ok {
			return refs, nil
		}
		refs, err := list(prefix)
		if err != nil {
			return nil, err
		}
		cache[prefix] = refs
		return refs, nil
	}
}

// RemoteTips reads tips from one remote. Defaults are the default branch
// name(s) (the first is the rig's); List reads the remote.
type RemoteTips struct {
	List     RemoteLister
	Defaults []string
}

// BranchTip returns the tip of refs/heads/<branch> on the remote, "" when the
// branch does not exist there.
func (r RemoteTips) BranchTip(branch string) (string, error) {
	if r.List == nil {
		return "", errors.New("remote is not available")
	}
	ref := "refs/heads/" + branch
	refs, err := r.List(ref)
	if err != nil {
		return "", err
	}
	for _, x := range refs {
		if x.Name == ref {
			return strings.TrimSpace(x.Hash), nil
		}
	}
	return "", nil
}

// TargetTips lists the default branch(es) and every integration/* branch.
func (r RemoteTips) TargetTips() ([]git.RemoteRef, error) {
	var out []git.RemoteRef
	for _, b := range r.Defaults {
		if b = strings.TrimSpace(b); b == "" {
			continue
		}
		sha, err := r.BranchTip(b)
		if err != nil {
			return nil, err
		}
		if sha != "" {
			out = append(out, git.RemoteRef{Name: "refs/heads/" + b, Hash: sha})
		}
	}
	if r.List == nil {
		return nil, errors.New("remote is not available")
	}
	integration, err := r.List("refs/heads/" + IntegrationBranchPrefix)
	if err != nil {
		return nil, errors.New("cannot list integration branches on the remote")
	}
	return append(out, integration...), nil
}

// Archived reports whether an archive ref (ArchiveRefPrefix) on the remote
// points at commit: the tip was preserved by gt polecat abandon (or a human).
// An unreadable remote is an error, never "archived".
func (r RemoteTips) Archived(commit string) (bool, error) {
	commit = strings.TrimSpace(commit)
	if commit == "" {
		return false, nil
	}
	if r.List == nil {
		return false, errors.New("remote is not available")
	}
	refs, err := r.List(ArchiveRefPrefix)
	if err != nil {
		return false, errors.New("cannot list archive refs on the remote")
	}
	for _, ref := range refs {
		if strings.HasPrefix(ref.Name, ArchiveRefPrefix) && strings.EqualFold(strings.TrimSpace(ref.Hash), commit) {
			return true, nil
		}
	}
	return false, nil
}

// PreservationGit is the git surface of CoveredByTargets.
type PreservationGit interface {
	Rev(ref string) (string, error)
	PreservedOn(head, base string) (git.BranchPreservationStatus, error)
}

// CoveredByTargets is the one "is this commit already on a merge target?"
// proof: commit is preserved (git.PreservedOn: ancestor, merge-tree no-op or
// every patch present) on one of the targets' tips. Missing objects, an
// unreadable remote or git errors mean "not proven" (false).
func CoveredByTargets(g PreservationGit, commit string, tips RemoteTips) bool {
	if commit == "" || g == nil || tips.List == nil {
		return false
	}
	if _, err := g.Rev(commit + "^{commit}"); err != nil {
		return false
	}
	refs, err := tips.TargetTips()
	if err != nil {
		return false
	}
	for _, ref := range refs {
		if ref.Hash == "" {
			continue
		}
		if _, err := g.Rev(ref.Hash + "^{commit}"); err != nil {
			continue
		}
		if st, err := g.PreservedOn(commit, ref.Hash); err == nil && st.Preserved {
			return true
		}
	}
	return false
}

// ArchiveRef is the archive ref for tip of branch at time at:
// refs/gt/archive/<branch>/<unix>-<first 12 hex of tip>. branch must be a
// polecat/* branch whose segments are beads.IsSafeID and tip a full object
// name; anything else is refused so the ref can never carry an option or a
// path escape.
func ArchiveRef(branch string, at time.Time, tip string) (string, error) {
	branch = strings.TrimSpace(branch)
	tip = strings.ToLower(strings.TrimSpace(tip))
	if !strings.HasPrefix(branch, "polecat/") || !beads.IsSafeRefName(branch) {
		return "", fmt.Errorf("refusing to archive %q: not a polecat branch name", branch)
	}
	if !beads.IsGitObjectName(tip) {
		return "", errors.New("refusing to archive: the tip is not a full git object name")
	}
	ref := fmt.Sprintf("%s%s/%d-%s", ArchiveRefPrefix, branch, at.Unix(), tip[:archiveShortSHALen])
	if _, _, ok := ParseArchiveRef(ref); !ok {
		return "", fmt.Errorf("refusing to archive %q: invalid archive ref", branch)
	}
	return ref, nil
}

// ParseArchiveRef returns the polecat branch an archive ref preserves and the
// object name prefix in its stamp, and whether ref is a well-formed archive
// ref.
func ParseArchiveRef(ref string) (branch, shortSHA string, ok bool) {
	rest, found := strings.CutPrefix(ref, ArchiveRefPrefix)
	if !found {
		return "", "", false
	}
	slash := strings.LastIndex(rest, "/")
	if slash <= 0 || !archiveStamp.MatchString(rest[slash+1:]) {
		return "", "", false
	}
	branch = rest[:slash]
	if !strings.HasPrefix(branch, "polecat/") || !beads.IsSafeRefName(branch) {
		return "", "", false
	}
	stamp := rest[slash+1:]
	return branch, stamp[strings.Index(stamp, "-")+1:], true
}
