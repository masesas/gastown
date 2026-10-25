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

// EnsureMayorCloneRedirect points a rig-mode mayor clone at the rig's own
// beads by writing <rig>/mayor/rig/.beads/redirect = MayorCloneRedirectTarget.
//
// It only acts when the rig is in config.RigBeadsModeRig and the repository
// tracks .beads/ (mayor/rig/.beads exists). It never modifies or removes the
// tracked files: the redirect is an extra local file, which bd's default
// .beads/.gitignore ignores. Returns true when the file was (re)written.
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
	if MayorCloneRedirectOK(rigPath) {
		return false, nil
	}
	redirect := filepath.Join(mayorBeads, "redirect")
	if err := os.WriteFile(redirect, []byte(MayorCloneRedirectTarget+"\n"), 0644); err != nil { //nolint:gosec // G306: redirect is a non-secret local pointer
		return false, fmt.Errorf("writing mayor clone redirect: %w", err)
	}
	return true, nil
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
