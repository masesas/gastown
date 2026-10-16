package pushguard

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrConflict means the managed hook cannot be installed without overwriting
// something Gas Town does not own. Install never resolves a conflict itself.
var ErrConflict = errors.New("pre-push guard conflict")

// State is the pre-push guard state of one hooks directory.
type State string

const (
	// StateInstalled: the current managed hook is in place.
	StateInstalled State = "installed"
	// StateDelegated: core.hooksPath points at a project hooks dir whose
	// pre-push calls `gt hooks pre-push`. Guarded; Gas Town does not touch it.
	StateDelegated State = "delegated"
	// StateNotApplicable: a bare repo with core.hooksPath set. The relative
	// hooks path only resolves inside its worktrees, which are inspected
	// separately.
	StateNotApplicable State = "not-applicable"
	// StateMissing: no pre-push hook. Fixable.
	StateMissing State = "missing"
	// StateOutdated: a managed hook from another Gas Town version. Fixable.
	StateOutdated State = "outdated"
	// StateForeign: a project pre-push hook that is not chained yet. Fixable
	// (moved to ChainedHookName, run after the Gas Town check).
	StateForeign State = "foreign"
	// StateConflict: not fixable automatically (see Status.Detail).
	StateConflict State = "conflict"
)

// Status describes the pre-push guard of one repository.
type Status struct {
	RepoPath string // repository the status was computed from
	HooksDir string // effective hooks directory ("" when not applicable)
	State    State
	Chained  bool   // a project hook is kept as ChainedHookName
	Changed  bool   // Install wrote a file
	Detail   string // human-readable reason for conflict states
}

// OK reports whether pushes from this repository go through the guard.
func (s Status) OK() bool {
	return s.State == StateInstalled || s.State == StateDelegated || s.State == StateNotApplicable
}

// Fixable reports whether Install can bring the repository to StateInstalled.
func (s Status) Fixable() bool {
	return s.State == StateMissing || s.State == StateOutdated || s.State == StateForeign
}

// layout is where git looks for hooks in a repository.
type layout struct {
	hooksDir  string
	hooksPath string // core.hooksPath value, "" when unset
	owned     bool   // hooksDir is <git-common-dir>/hooks (Gas Town may write)
	bare      bool
}

func runGit(repo string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", args[0], err, strings.TrimSpace(stderr.String()))
	}
	return strings.TrimSpace(stdout.String()), nil
}

// configHooksPath returns core.hooksPath, or "" when it is unset.
func configHooksPath(repo string) (string, error) {
	cmd := exec.Command("git", "-C", repo, "config", "--get", "core.hooksPath")
	out, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 1 {
			return "", nil // key not set
		}
		return "", fmt.Errorf("git config core.hooksPath: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func resolveLayout(repo string) (layout, error) {
	common, err := runGit(repo, "rev-parse", "--path-format=absolute", "--git-common-dir")
	if err != nil {
		return layout{}, fmt.Errorf("%s is not a git repository: %w", repo, err)
	}
	isBare, err := runGit(repo, "rev-parse", "--is-bare-repository")
	if err != nil {
		return layout{}, err
	}
	hp, err := configHooksPath(repo)
	if err != nil {
		return layout{}, err
	}
	l := layout{hooksPath: hp, bare: isBare == "true"}
	if hp == "" {
		l.hooksDir = filepath.Join(common, "hooks")
		l.owned = true
		return l, nil
	}
	if l.bare {
		return l, nil
	}
	if filepath.IsAbs(hp) {
		l.hooksDir = hp
		return l, nil
	}
	top, err := runGit(repo, "rev-parse", "--show-toplevel")
	if err != nil {
		return layout{}, err
	}
	l.hooksDir = filepath.Join(top, hp)
	return l, nil
}

// Inspect reports the pre-push guard state of the repository at repo (a
// worktree, a clone, or a bare repository). It changes nothing.
func Inspect(repo string) (Status, error) {
	l, err := resolveLayout(repo)
	if err != nil {
		return Status{RepoPath: repo}, err
	}
	return inspectLayout(repo, l)
}

func inspectLayout(repo string, l layout) (Status, error) {
	st := Status{RepoPath: repo, HooksDir: l.hooksDir}
	if !l.owned {
		return inspectProjectHooksPath(st, l)
	}
	if _, err := os.Lstat(filepath.Join(l.hooksDir, ChainedHookName)); err == nil {
		st.Chained = true
	}
	hook := filepath.Join(l.hooksDir, "pre-push")
	content, err := os.ReadFile(hook)
	switch {
	case errors.Is(err, os.ErrNotExist):
		st.State = StateMissing
	case err != nil:
		return st, fmt.Errorf("reading pre-push hook: %w", err)
	case string(content) == Script() && isExecutable(hook):
		st.State = StateInstalled
	case bytes.Contains(content, []byte(Marker)):
		st.State = StateOutdated
	case st.Chained:
		st.State = StateConflict
		st.Detail = fmt.Sprintf("%s holds a project pre-push hook and %s already exists; "+
			"merge them into %s by hand, then run gt doctor --fix", filepath.Join(l.hooksDir, "pre-push"),
			ChainedHookName, ChainedHookName)
	default:
		st.State = StateForeign
	}
	return st, nil
}

func inspectProjectHooksPath(st Status, l layout) (Status, error) {
	if l.bare {
		st.State = StateNotApplicable
		st.Detail = "bare repository with core.hooksPath; its worktrees are checked instead"
		return st, nil
	}
	content, err := os.ReadFile(filepath.Join(l.hooksDir, "pre-push"))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return st, fmt.Errorf("reading project pre-push hook: %w", err)
	}
	if err == nil && isExecutable(filepath.Join(l.hooksDir, "pre-push")) && delegates(content) {
		st.State = StateDelegated
		return st, nil
	}
	st.State = StateConflict
	st.Detail = fmt.Sprintf("core.hooksPath=%s is a project-owned hooks dir and its pre-push does not call "+
		"`%s`; Gas Town does not write there. Add `%s \"$@\" || exit 1` to that hook "+
		"(feeding it git's stdin), or unset core.hooksPath and run gt doctor --fix",
		l.hooksPath, DelegationMarker, DelegationMarker)
	return st, nil
}

// Install makes the repository's pushes go through the Gas Town guard. It is
// idempotent: an installed or delegated repository is left untouched
// (Changed=false). A project pre-push hook is preserved as ChainedHookName.
// A conflict returns the status with an error wrapping ErrConflict.
func Install(repo string) (Status, error) {
	l, err := resolveLayout(repo)
	if err != nil {
		return Status{RepoPath: repo}, err
	}
	st, err := inspectLayout(repo, l)
	if err != nil {
		return st, err
	}
	switch st.State {
	case StateConflict:
		return st, fmt.Errorf("%w: %s", ErrConflict, st.Detail)
	case StateForeign:
		// Link (not rename) so an existing chained hook is never clobbered,
		// even by a concurrent installer.
		hook := filepath.Join(l.hooksDir, "pre-push")
		if err := os.Link(hook, filepath.Join(l.hooksDir, ChainedHookName)); err != nil {
			return st, fmt.Errorf("preserving project pre-push hook as %s: %w", ChainedHookName, err)
		}
		st.Chained = true
	case StateMissing, StateOutdated:
	default:
		return st, nil
	}
	if err := writeHook(l.hooksDir); err != nil {
		return st, err
	}
	st.State = StateInstalled
	st.Changed = true
	return st, nil
}

// writeHook atomically writes the managed hook (temp file + rename).
func writeHook(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("creating hooks dir: %w", err)
	}
	tmp, err := os.CreateTemp(dir, ".pre-push.gt-*")
	if err != nil {
		return fmt.Errorf("creating pre-push hook: %w", err)
	}
	defer func() { _ = os.Remove(tmp.Name()) }()
	if _, err := tmp.WriteString(Script()); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing pre-push hook: %w", err)
	}
	if err := tmp.Chmod(0o755); err != nil { //nolint:gosec // G302: git hooks must be executable
		_ = tmp.Close()
		return fmt.Errorf("chmod pre-push hook: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing pre-push hook: %w", err)
	}
	if err := os.Rename(tmp.Name(), filepath.Join(dir, "pre-push")); err != nil {
		return fmt.Errorf("installing pre-push hook: %w", err)
	}
	return nil
}

// isExecutable reports whether git would run the file as a hook.
func isExecutable(p string) bool {
	info, err := os.Stat(p)
	return err == nil && info.Mode().IsRegular() && info.Mode().Perm()&0o111 != 0
}

// delegates reports whether a project hook calls DelegationMarker on a line
// that is not a shell comment.
func delegates(content []byte) bool {
	for _, line := range strings.Split(string(content), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if code, _, _ := strings.Cut(line, " #"); strings.Contains(code, DelegationMarker) {
			return true
		}
	}
	return false
}
