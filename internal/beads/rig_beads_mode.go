package beads

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/steveyegge/gastown/internal/config"
)

// MayorCloneRedirectTarget is the redirect written into a rig-mode mayor clone
// (<rig>/mayor/rig/.beads/redirect). It is resolved relative to mayor/rig and
// therefore points at the rig's own <rig>/.beads.
const MayorCloneRedirectTarget = "../../.beads"

// TrackedRigBeadsDir returns <rigPath>/mayor/rig/.beads and true when Gas Town
// should treat the repository's tracked .beads/ (cloned into the mayor clone)
// as the rig's canonical beads location: the directory exists and the rig is
// not in config.RigBeadsModeRig.
//
// Every "does this rig use tracked beads?" decision goes through this helper,
// so beads.mode=rig consistently means "never use or write the repository's
// tracked .beads/".
func TrackedRigBeadsDir(rigPath string) (string, bool) {
	mayorBeads := filepath.Join(rigPath, "mayor", "rig", ".beads")
	if _, err := os.Stat(mayorBeads); err != nil {
		return "", false
	}
	if config.RigUsesOwnBeads(rigPath) {
		return "", false
	}
	return mayorBeads, true
}

// MayorCloneExcludeEntry is the pattern EnsureMayorCloneRedirect adds to the
// mayor clone's .git/info/exclude so the redirect never shows in git status,
// whatever the project's own .gitignore says.
const MayorCloneExcludeEntry = ".beads/redirect"

// MayorCloneHasBeads reports whether the repository cloned into
// <rig>/mayor/rig ships a .beads directory (i.e. tracks its own beads).
func MayorCloneHasBeads(rigPath string) bool {
	info, err := os.Stat(filepath.Join(rigPath, "mayor", "rig", ".beads"))
	return err == nil && info.IsDir()
}

// EnsureMayorCloneRedirect points a rig-mode mayor clone at the rig's own
// beads by writing <rig>/mayor/rig/.beads/redirect = MayorCloneRedirectTarget
// and adding MayorCloneExcludeEntry to <rig>/mayor/rig/.git/info/exclude.
//
// It only acts when the rig is in config.RigBeadsModeRig and the repository
// tracks .beads/ (mayor/rig/.beads exists). It never modifies or removes the
// repository's tracked files: the redirect is an extra local file, hidden from
// git by the clone-local exclude (skipped when .git is not a directory).
// Returns true when the redirect file was (re)written. Idempotent.
func EnsureMayorCloneRedirect(rigPath string) (bool, error) {
	if !config.RigUsesOwnBeads(rigPath) {
		return false, nil
	}
	mayorBeads := filepath.Join(rigPath, "mayor", "rig", ".beads")
	info, err := os.Lstat(mayorBeads)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("checking mayor clone beads dir: %w", err)
	}
	if !info.IsDir() {
		return false, fmt.Errorf("mayor clone .beads is not a directory: %s", mayorBeads)
	}
	if err := ensureGitInfoExclude(filepath.Join(rigPath, "mayor", "rig"), MayorCloneExcludeEntry); err != nil {
		return false, err
	}
	if MayorCloneRedirectOK(rigPath) {
		return false, nil
	}
	redirect := filepath.Join(mayorBeads, "redirect")
	if err := os.WriteFile(redirect, []byte(MayorCloneRedirectTarget+"\n"), 0644); err != nil { //nolint:gosec // G306: redirect is a non-secret local pointer
		return false, fmt.Errorf("writing mayor clone redirect: %w", err)
	}
	return true, nil
}

// ensureGitInfoExclude appends entry to <clonePath>/.git/info/exclude unless a
// line equal to it already exists. A missing .git or a .git file (linked
// worktree) is skipped without error.
func ensureGitInfoExclude(clonePath, entry string) error {
	gitDir := filepath.Join(clonePath, ".git")
	info, err := os.Lstat(gitDir)
	if err != nil || !info.IsDir() {
		return nil
	}
	excludePath := filepath.Join(gitDir, "info", "exclude")
	data, err := os.ReadFile(excludePath) //nolint:gosec // G304: path is constructed internally
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("reading %s: %w", excludePath, err)
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == entry {
			return nil
		}
	}
	content := string(data)
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += entry + "\n"
	if err := os.MkdirAll(filepath.Dir(excludePath), 0755); err != nil {
		return fmt.Errorf("creating %s: %w", filepath.Dir(excludePath), err)
	}
	if err := os.WriteFile(excludePath, []byte(content), 0644); err != nil { //nolint:gosec // G306: git exclude file is not secret
		return fmt.Errorf("writing %s: %w", excludePath, err)
	}
	return nil
}

// MayorCloneRedirectOK reports whether <rig>/mayor/rig/.beads/redirect
// already points at MayorCloneRedirectTarget.
func MayorCloneRedirectOK(rigPath string) bool {
	data, err := os.ReadFile(filepath.Join(rigPath, "mayor", "rig", ".beads", "redirect")) //nolint:gosec // G304: path is constructed internally
	if err != nil {
		return false
	}
	return strings.TrimSpace(string(data)) == MayorCloneRedirectTarget
}
