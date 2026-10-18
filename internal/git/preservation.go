package git

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Preservation proof (gt-pd1c.86): "is HEAD's work already on the remote?" is
// answered from the remote itself. The tips come from one bounded ls-remote of
// the push target (the same source mergeproof.RemoteTips reads through
// ListPushRemoteRefsWithHashes) and are compared with PreservedOn's rule
// (ancestor, merge-tree no-op or every patch present), the rule
// mergeproof.CoveredByTargets applies. Local refs/remotes/*, local branches and
// the local origin/HEAD are never evidence: an agent can move any of them
// without pushing (git update-ref, git symbolic-ref, git branch -f).
// mergeproof itself cannot be called from here (it imports this package).

var (
	// ErrRemoteProofUnavailable means the remote could not prove HEAD
	// preserved: the remote is not configured (e.g. removed), could not be
	// read (offline, timeout, auth) or its tips are not in the local object
	// store (fetch first). It is never "preserved"; callers fail closed.
	ErrRemoteProofUnavailable = errors.New("remote proof unavailable")
)

// Evidence values of BranchPreservationStatus besides PreservedOn's
// "ancestor", "merge_tree_noop" and "cherry".
const (
	// evidenceExactRemoteBranch: the remote branch of the same name contains HEAD.
	evidenceExactRemoteBranch = "exact_remote_branch"
	// evidenceNoRemoteBase: the remote has none of the comparison branches,
	// so every commit of HEAD is unpreserved.
	evidenceNoRemoteBase = "no_remote_base"
	// evidenceNoCommits: HEAD has no commits at all (unborn): nothing to lose.
	evidenceNoCommits = "no_commits"
)

// defaultBranchFallbacks are the branch names tried, after the branch the
// remote's HEAD names, when no target, pushed branch or upstream exists on
// the remote.
var defaultBranchFallbacks = []string{"main", "master"}

// BranchPreservationStatus describes whether HEAD is already preserved on a
// durable branch of the remote, and how many patch-unique commits remain if
// it is not. ComparisonBase is "<remote>/<branch>" of the remote branch the
// answer was computed against ("" when none exists on the remote).
type BranchPreservationStatus struct {
	Preserved             bool
	ComparisonBase        string
	UnpreservedPatchCount int
	Evidence              string
}

// BranchPreservationStatus checks whether HEAD is preserved on the remote
// (read with ls-remote, never local tracking refs). It prefers the exact
// pushed source branch, then the explicit targets, then the branch's upstream
// on the same remote; only when none of them exists on the remote does it
// compare against the remote's default branch (the branch the remote's HEAD
// names, then main, master). targets are branch names on the remote; the
// forms "<remote>/<b>", "refs/heads/<b>" and "refs/remotes/<remote>/<b>" are
// accepted, anything naming another remote is ignored.
//
// Errors: ErrRemoteProofUnavailable when the remote is not configured, cannot
// be read or its tips are not fetched. Any error means "not proven
// preserved". Only a repository without any commit is preserved without a
// remote (evidence "no_commits").
func (g *Git) BranchPreservationStatus(localBranch, remote string, targets []string) (BranchPreservationStatus, error) {
	return g.branchPreservationStatus(localBranch, remote, targets, true)
}

// BranchTargetStatus checks whether HEAD is already represented on the
// branch's target/custody branches on the remote. Unlike
// BranchPreservationStatus, the exact pushed source branch (and a polecat
// branch's upstream that is that same branch) is not enough evidence, because
// pushed-but-unsubmitted work still needs merge-queue recovery. Same remote
// proof and errors as BranchPreservationStatus.
func (g *Git) BranchTargetStatus(localBranch, remote string, targets []string) (BranchPreservationStatus, error) {
	return g.branchPreservationStatus(localBranch, remote, targets, false)
}

// remoteTip is a comparison candidate read from the remote.
type remoteTip struct {
	label string // "<remote>/<branch>"
	sha   string
	exact bool // the pushed source branch itself
}

// preservationQuery is what one status computation asks the remote for.
type preservationQuery struct {
	exact    string   // pushed source branch ("" = not evidence)
	targets  []string // target branches, in caller order
	upstream string   // the branch's upstream on the same remote ("" = none)
}

func (g *Git) branchPreservationStatus(localBranch, remote string, targets []string, includeExactBranch bool) (BranchPreservationStatus, error) {
	if remote == "" {
		remote = "origin"
	}
	if strings.HasPrefix(remote, "-") {
		return BranchPreservationStatus{}, fmt.Errorf("invalid remote %q", remote)
	}
	head, err := g.Rev("HEAD")
	if err != nil {
		if g.hasNoCommits() {
			return BranchPreservationStatus{Preserved: true, Evidence: evidenceNoCommits}, nil
		}
		return BranchPreservationStatus{}, fmt.Errorf("reading HEAD: %w", err)
	}
	if _, err := g.RemoteURL(remote); err != nil {
		// No remote (or one an agent removed) proves nothing: the commits
		// are at risk, never "nothing to push" (gt-pd1c.86 H2a).
		return BranchPreservationStatus{}, fmt.Errorf("%w: remote %s is not configured", ErrRemoteProofUnavailable, remote)
	}
	q := g.newPreservationQuery(localBranch, remote, targets, includeExactBranch)
	tips, err := g.readRemoteProofTips(remote, q.branches())
	if err != nil {
		return BranchPreservationStatus{}, err
	}
	candidates := q.candidates(remote, tips)
	if len(candidates) == 0 {
		return g.noRemoteBaseStatus()
	}
	return g.preservationAgainstTips(head, candidates)
}

func (g *Git) newPreservationQuery(localBranch, remote string, targets []string, includeExactBranch bool) preservationQuery {
	var q preservationQuery
	localBranch = strings.TrimSpace(localBranch)
	if localBranch == "HEAD" || !safeProofBranchName(localBranch) {
		localBranch = ""
	}
	if includeExactBranch {
		q.exact = localBranch
	}
	for _, t := range nonEmptyUnique(targets) {
		if b := remoteProofBranchName(t, remote); b != "" {
			q.targets = append(q.targets, b)
		}
	}
	if up := g.upstreamBranchOn(localBranch, remote); up != "" {
		// A polecat branch tracking its own pushed branch is the exact
		// branch again: not target evidence.
		if includeExactBranch || !(strings.HasPrefix(localBranch, "polecat/") && up == localBranch) {
			q.upstream = up
		}
	}
	return q
}

// branches lists every branch name the query needs from the remote.
func (q preservationQuery) branches() []string {
	all := append([]string{q.exact}, q.targets...)
	all = append(all, q.upstream)
	return nonEmptyUnique(append(all, defaultBranchFallbacks...))
}

// candidates orders the remote tips: exact branch, targets, upstream; the
// default branch(es) only when none of those exists on the remote.
func (q preservationQuery) candidates(remote string, tips remoteProofTips) []remoteTip {
	var out []remoteTip
	add := func(branch string, exact bool) {
		if sha, ok := tips.branches[branch]; ok && branch != "" {
			out = append(out, remoteTip{label: remote + "/" + branch, sha: sha, exact: exact})
		}
	}
	add(q.exact, true)
	for _, t := range q.targets {
		add(t, false)
	}
	add(q.upstream, false)
	if len(out) > 0 {
		return dedupeTips(out)
	}
	if tips.headBranch != "" && tips.headSHA != "" {
		out = append(out, remoteTip{label: remote + "/" + tips.headBranch, sha: tips.headSHA})
	}
	for _, b := range defaultBranchFallbacks {
		add(b, false)
	}
	return dedupeTips(out)
}

func dedupeTips(tips []remoteTip) []remoteTip {
	seen := make(map[string]bool, len(tips))
	out := tips[:0:0]
	for _, t := range tips {
		if seen[t.label] {
			continue
		}
		seen[t.label] = true
		out = append(out, t)
	}
	return out
}

// preservationAgainstTips returns the first candidate that preserves head,
// else the first candidate's unpreserved answer. A candidate whose tip is
// not in the local object store cannot be compared; if no candidate could be
// compared the answer is ErrRemoteProofUnavailable.
func (g *Git) preservationAgainstTips(head string, candidates []remoteTip) (BranchPreservationStatus, error) {
	var first *BranchPreservationStatus
	var lastErr error
	for _, c := range candidates {
		st, err := g.preservationAgainstTip(head, c)
		if err != nil {
			lastErr = err
			continue
		}
		if st.Preserved {
			return st, nil
		}
		if first == nil {
			first = &st
		}
	}
	if first != nil {
		return *first, nil
	}
	if lastErr == nil {
		lastErr = errors.New("no usable remote tip")
	}
	return BranchPreservationStatus{}, fmt.Errorf("%w: %w", ErrRemoteProofUnavailable, lastErr)
}

func (g *Git) preservationAgainstTip(head string, c remoteTip) (BranchPreservationStatus, error) {
	if strings.EqualFold(strings.TrimSpace(c.sha), head) {
		return BranchPreservationStatus{Preserved: true, ComparisonBase: c.label, Evidence: tipEvidence(c, "ancestor")}, nil
	}
	if have, err := g.proofObjectExists(c.sha); err != nil || !have {
		return BranchPreservationStatus{}, fmt.Errorf("remote tip of %s (%s) is not in the local repository; run git fetch", c.label, shortSHA(c.sha))
	}
	st, err := g.PreservedOn(head, c.sha)
	if err != nil {
		return BranchPreservationStatus{}, fmt.Errorf("comparing with %s: %w", c.label, err)
	}
	st.ComparisonBase = c.label
	if st.Preserved {
		st.Evidence = tipEvidence(c, st.Evidence)
	}
	return st, nil
}

func tipEvidence(c remoteTip, evidence string) string {
	if c.exact && evidence == "ancestor" {
		return evidenceExactRemoteBranch
	}
	return evidence
}

// hasNoCommits reports an unborn HEAD in a repository without any ref
// pointing at a commit, read without lazy fetch.
func (g *Git) hasNoCommits() bool {
	if _, err := g.proofRun("rev-parse", "--verify", "--quiet", "HEAD"); err == nil {
		return false
	}
	out, err := g.proofRun("for-each-ref", "--count=1", "--format=%(refname)")
	return err == nil && strings.TrimSpace(out) == ""
}

// noRemoteBaseStatus answers when the remote has none of the comparison
// branches: nothing of HEAD is proven there, so every commit counts.
func (g *Git) noRemoteBaseStatus() (BranchPreservationStatus, error) {
	out, err := g.run("rev-list", "--count", "HEAD")
	if err != nil {
		return BranchPreservationStatus{}, fmt.Errorf("counting commits: %w", err)
	}
	n, err := strconv.Atoi(strings.TrimSpace(out))
	if err != nil {
		return BranchPreservationStatus{}, fmt.Errorf("parsing commit count: %w", err)
	}
	return BranchPreservationStatus{Preserved: n == 0, UnpreservedPatchCount: n, Evidence: evidenceNoRemoteBase}, nil
}

// upstreamBranchOn returns the branch localBranch tracks on remote, "" when
// it tracks nothing there (no upstream, another remote, a local branch).
// Only the configuration is read; the tip comes from the remote.
func (g *Git) upstreamBranchOn(localBranch, remote string) string {
	if localBranch == "" {
		return ""
	}
	out, err := g.run("for-each-ref", "--format=%(upstream:remotename)%09%(upstream:remoteref)", "--", "refs/heads/"+localBranch)
	if err != nil {
		return ""
	}
	name, ref, ok := strings.Cut(strings.TrimSpace(out), "\t")
	if !ok || name != remote {
		return ""
	}
	branch, ok := strings.CutPrefix(ref, "refs/heads/")
	if !ok || !safeProofBranchName(branch) {
		return ""
	}
	return branch
}

// remoteProofBranchName maps a target to a branch name on remote: "b",
// "<remote>/b", "refs/heads/b" and "refs/remotes/<remote>/b" name b; refs of
// another remote, tags, HEAD and unsafe names map to "".
func remoteProofBranchName(ref, remote string) string {
	ref = strings.TrimSpace(ref)
	switch {
	case strings.HasPrefix(ref, "refs/heads/"):
		ref = strings.TrimPrefix(ref, "refs/heads/")
	case strings.HasPrefix(ref, "refs/remotes/"+remote+"/"):
		ref = strings.TrimPrefix(ref, "refs/remotes/"+remote+"/")
	case strings.HasPrefix(ref, "refs/"):
		return ""
	case strings.HasPrefix(ref, remote+"/"):
		ref = strings.TrimPrefix(ref, remote+"/")
	case remote != "upstream" && strings.HasPrefix(ref, "upstream/"):
		return ""
	}
	if ref == "HEAD" || !safeProofBranchName(ref) {
		return ""
	}
	return ref
}

// safeProofBranchName accepts names that can only match themselves as an
// ls-remote pattern: no option prefix, glob, space, control character or
// ref-name syntax git rejects.
func safeProofBranchName(b string) bool {
	if b == "" || strings.HasPrefix(b, "-") || strings.HasPrefix(b, "/") || strings.HasSuffix(b, "/") ||
		strings.HasSuffix(b, ".lock") || strings.HasSuffix(b, ".") || strings.Contains(b, "..") ||
		strings.Contains(b, "//") || strings.Contains(b, "@{") {
		return false
	}
	for _, r := range b {
		if unicode.IsSpace(r) || unicode.IsControl(r) || strings.ContainsRune("*?[\\~^:", r) {
			return false
		}
	}
	return true
}

// remoteProofTips is one ls-remote answer: the remote's HEAD (branch it names
// and its tip) and the tips of the requested branches that exist.
type remoteProofTips struct {
	headBranch string
	headSHA    string
	branches   map[string]string
}

// readRemoteProofTips reads HEAD and refs/heads/<b> for every b from the push
// target of remote in one ls-remote, under the landing-proof bounds (timeout,
// output limit, no terminal prompt). ls-remote matches patterns against ref
// name tails, so only exact names are kept.
func (g *Git) readRemoteProofTips(remote string, branches []string) (remoteProofTips, error) {
	target := g.pushTarget(remote)
	if target == "" || strings.HasPrefix(target, "-") {
		return remoteProofTips{}, fmt.Errorf("%w: invalid remote %q", ErrRemoteProofUnavailable, remote)
	}
	args := []string{"ls-remote", "--symref", "--", target, "HEAD"}
	for _, b := range branches {
		args = append(args, "refs/heads/"+b)
	}
	out, err := g.proofRemote(args...)
	if err != nil {
		return remoteProofTips{}, fmt.Errorf("%w: cannot read remote %s: %w", ErrRemoteProofUnavailable, remote, err)
	}
	return parseRemoteProofTips(out, branches), nil
}

func parseRemoteProofTips(out string, branches []string) remoteProofTips {
	want := make(map[string]bool, len(branches))
	for _, b := range branches {
		want["refs/heads/"+b] = true
	}
	tips := remoteProofTips{branches: map[string]string{}}
	for _, line := range strings.Split(out, "\n") {
		f := strings.Fields(line)
		switch {
		case len(f) == 3 && f[0] == "ref:" && f[2] == "HEAD" && strings.HasPrefix(f[1], "refs/heads/"):
			tips.headBranch = strings.TrimPrefix(f[1], "refs/heads/")
		case len(f) == 2 && f[1] == "HEAD":
			tips.headSHA = f[0]
		case len(f) == 2 && want[f[1]]:
			tips.branches[strings.TrimPrefix(f[1], "refs/heads/")] = f[0]
		}
	}
	if !safeProofBranchName(tips.headBranch) {
		tips.headBranch, tips.headSHA = "", ""
	}
	return tips
}
