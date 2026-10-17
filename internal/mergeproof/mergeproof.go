// Package mergeproof resolves the git-based proof that a merge request's work
// landed on its target branch (beads.MergeProver) for a rig.
package mergeproof

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/config"
	"github.com/steveyegge/gastown/internal/git"
)

// RigGit returns a Git object for the rig's repository. It prefers .repo.git
// (bare repo) and falls back to mayor/rig. It is unbounded (also used for
// merges and pushes); the provers bound their copy (ForRigPath).
func RigGit(rigPath string) (*git.Git, error) {
	bareRepoPath := filepath.Join(rigPath, ".repo.git")
	if info, err := os.Stat(bareRepoPath); err == nil && info.IsDir() {
		return git.NewGitWithDir(bareRepoPath, ""), nil
	}
	mayorPath := filepath.Join(rigPath, "mayor", "rig")
	if _, err := os.Stat(mayorPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("no repo base found (neither .repo.git nor mayor/rig exists)")
	}
	return git.NewGit(mayorPath), nil
}

type memoProver struct {
	g       *git.Git
	reaches memo[error]
	patches memo[patchAnswer]
	// configured is the rig's default_branch (config.RigDefaultBranch, "" =
	// unset). When set it is the only trusted default branch.
	configured string
	// remoteHead reads the default branch the remote advertises (its HEAD
	// symref); used only when nothing is configured, read once.
	remoteHead     func() (string, error)
	remoteHeadOnce sync.Once
	remoteHeadName string
}

// defaultBranch is the rig's default branch: the configured default_branch,
// else the branch the remote's HEAD names (never the local
// refs/remotes/origin/HEAD, which an agent in a shared repository can repoint,
// gt-pd1c.78 S-M1). "" when neither is known: no default branch is trusted.
func (m *memoProver) defaultBranch() string {
	if m.configured != "" {
		return m.configured
	}
	m.remoteHeadOnce.Do(func() {
		if m.remoteHead != nil {
			if b, err := m.remoteHead(); err == nil {
				m.remoteHeadName = strings.TrimSpace(b)
			}
		}
	})
	return m.remoteHeadName
}

// untrustedTarget refuses an MR target that is not a merge target (gt-pd1c.78
// tahap 2): the rig's default branch (defaultBranch) or integration/*
// (TrustedMergeTarget). The target is a bead field anyone can edit; set to
// the polecat's own branch it would "prove" any commit landed. The error is
// not a definite "not on target", so the landing stays undecided (held, the
// overseer decides) instead of being read as either verdict.
func (m *memoProver) untrustedTarget(branch string) error {
	return UntrustedMergeTarget(branch, m.defaultBranch())
}

// RigDefaultBranch is the rig's default branch as the provers read it: the
// configured default_branch (config.RigDefaultBranch), else the branch the
// remote's HEAD names (git.RemoteHeadBranch on origin), never the local
// origin/HEAD. "" when neither is known.
func RigDefaultBranch(rigPath string, g *git.Git) string {
	if b := config.RigDefaultBranch(rigPath); b != "" {
		return b
	}
	if g == nil {
		return ""
	}
	b, err := g.RemoteHeadBranch("origin")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(b)
}

// UntrustedMergeTarget is the one check of an MR target used as landing
// proof (the provers and gt mq post-merge's proof): nil when target is
// defaultBranch (non-empty) or an integration/* branch, else why it is not
// trusted.
func UntrustedMergeTarget(target, defaultBranch string) error {
	target = strings.TrimSpace(target)
	if rest, ok := strings.CutPrefix(target, IntegrationBranchPrefix); ok && rest != "" {
		return nil
	}
	if def := strings.TrimSpace(defaultBranch); def != "" && target == def {
		return nil
	}
	return fmt.Errorf("MR target %q is not a merge target (the rig default branch or integration/*); "+
		"it is not trusted as landing proof", target)
}

// errProofAborted answers callers that waited for a computation that panicked.
var errProofAborted = errors.New("landing proof aborted")

// IsAncestor forwards to git (beads.MRFieldsLanded's no-new-work guard).
func (m *memoProver) IsAncestor(ancestor, descendant string) (bool, error) {
	return m.g.IsAncestor(ancestor, descendant)
}

func (m *memoProver) VerifyPushedCommitReachableFromPushTarget(remote, branch, commit string) error {
	if err := m.untrustedTarget(branch); err != nil {
		return err
	}
	key := remote + "\x00" + branch + "\x00" + commit
	return m.reaches.do(key, func() error {
		err := m.g.VerifyPushedCommitReachableFromPushTarget(remote, branch, commit)
		if err != nil {
			// Integration branches are deleted after landing: the work then
			// lives on the default branch.
			if def := m.defaultBranch(); def != "" && def != branch {
				err = combineFallback(err, m.g.VerifyPushedCommitReachableFromPushTarget(remote, def, commit))
			}
		}
		return err
	}, errProofAborted)
}

type patchAnswer struct {
	equivalent bool
	err        error
}

// PatchEquivalentOnPushTarget forwards to git (beads.PatchProver: rebase or
// squash merges), memoized like reachability and with the same fallback to
// the default branch.
func (m *memoProver) PatchEquivalentOnPushTarget(remote, branch, commit, base string) (bool, error) {
	if err := m.untrustedTarget(branch); err != nil {
		return false, err
	}
	key := remote + "\x00" + branch + "\x00" + commit + "\x00" + base
	a := m.patches.do(key, func() patchAnswer {
		ok, err := m.g.PatchEquivalentOnPushTarget(remote, branch, commit, base)
		if !ok {
			if def := m.defaultBranch(); def != "" && def != branch {
				defOK, defErr := m.g.PatchEquivalentOnPushTarget(remote, def, commit, base)
				ok, err = combinePatchFallback(ok, err, defOK, defErr)
			}
		}
		return patchAnswer{equivalent: ok, err: err}
	}, patchAnswer{err: errProofAborted})
	return a.equivalent, a.err
}

// combinePatchFallback merges the target and default-branch patch answers: an
// equivalent patch on either is landed; "none" is definite only when both
// answers are definite; any failure keeps the answer uncertain.
func combinePatchFallback(targetOK bool, targetErr error, defaultOK bool, defaultErr error) (bool, error) {
	switch {
	case targetOK || (defaultOK && defaultErr == nil):
		return true, nil
	case targetErr != nil:
		return false, targetErr
	default:
		return false, defaultErr
	}
}

// combineFallback merges the target and default-branch answers: reachable from
// either is landed; "not reachable" is definite only when both are definite
// (beads.IsCommitNotOnTarget); any other failure keeps the answer uncertain.
func combineFallback(targetErr, defaultErr error) error {
	switch {
	case defaultErr == nil:
		return nil
	case !beads.IsCommitNotOnTarget(targetErr):
		return targetErr
	default:
		return defaultErr
	}
}

// ForRigPath returns a prover for the rig at rigPath (memoized per prover, so
// one scan checks each commit/target once), or nil if the repo is unavailable.
func ForRigPath(rigPath string) beads.MergeProver {
	g, err := RigGit(rigPath)
	if err != nil {
		return nil
	}
	// Patrol paths must never hang on git: every command of the prover,
	// reachability included, runs under the proof bounds.
	bounded := g.WithProofLimits()
	return &memoProver{g: bounded, configured: config.RigDefaultBranch(rigPath),
		remoteHead: func() (string, error) { return bounded.RemoteHeadBranch("origin") }}
}

// Resolver returns a ProverResolver for rigs under townRoot (<town>/<rig>).
// Provers are created once per rig. An empty rig name resolves to nil.
func Resolver(townRoot string) beads.ProverResolver {
	var mu sync.Mutex
	cache := make(map[string]beads.MergeProver)
	return func(rig string) beads.MergeProver {
		rig = strings.TrimSpace(rig)
		if rig == "" || rig != filepath.Base(rig) {
			return nil
		}
		mu.Lock()
		defer mu.Unlock()
		if p, ok := cache[rig]; ok {
			return p
		}
		p := ForRigPath(filepath.Join(townRoot, rig))
		cache[rig] = p
		return p
	}
}

// ForRig returns a resolver that always yields the prover for rigPath,
// whatever rig name an MR records (callers already scoped to one rig).
func ForRig(rigPath string) beads.ProverResolver {
	p := ForRigPath(rigPath)
	return func(string) beads.MergeProver { return p }
}

// TownMergeQueue reads the town's merge queue (every beads DB) with this
// town's git provers: the one scope in which work beads are classified as
// held (beads.MergeQueue, gt-pd1c.59).
func TownMergeQueue(townRoot string) *beads.MergeQueue {
	return beads.ReadMergeQueue(townRoot, Resolver(townRoot))
}
