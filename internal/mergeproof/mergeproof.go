// Package mergeproof resolves the git-based proof that a merge request's work
// landed on its target branch (beads.MergeProver) for a rig.
package mergeproof

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/steveyegge/gastown/internal/beads"
	"github.com/steveyegge/gastown/internal/git"
)

// RigGit returns a Git object for the rig's repository. It prefers .repo.git
// (bare repo) and falls back to mayor/rig.
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
	g    *git.Git
	mu   sync.Mutex
	seen map[string]error
}

// IsAncestor forwards to git (beads.MRFieldsLanded's no-new-work guard).
func (m *memoProver) IsAncestor(ancestor, descendant string) (bool, error) {
	return m.g.IsAncestor(ancestor, descendant)
}

func (m *memoProver) VerifyPushedCommitReachableFromPushTarget(remote, branch, commit string) error {
	key := remote + "\x00" + branch + "\x00" + commit
	m.mu.Lock()
	defer m.mu.Unlock()
	if err, ok := m.seen[key]; ok {
		return err
	}
	err := m.g.VerifyPushedCommitReachableFromPushTarget(remote, branch, commit)
	if err != nil {
		// Integration branches are deleted after landing: the work then lives
		// on the default branch.
		if def := m.g.RemoteDefaultBranch(); def != "" && def != branch {
			err = combineFallback(err, m.g.VerifyPushedCommitReachableFromPushTarget(remote, def, commit))
		}
	}
	m.seen[key] = err
	return err
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
	return &memoProver{g: g, seen: make(map[string]error)}
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
