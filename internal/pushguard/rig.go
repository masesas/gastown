package pushguard

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// RigRepoPaths lists the git repositories of a rig that agents push from:
// the shared bare repo (.repo.git, whose hooks also serve the refinery and
// polecat worktrees), mayor/rig, refinery/rig, crew/<name> and polecat
// worktrees (polecats/<name>/<rig> and the legacy polecats/<name>). Only
// existing repositories are returned.
func RigRepoPaths(rigPath string) []string {
	var paths []string
	if info, err := os.Stat(filepath.Join(rigPath, ".repo.git")); err == nil && info.IsDir() {
		paths = append(paths, filepath.Join(rigPath, ".repo.git"))
	}
	for _, p := range []string{filepath.Join(rigPath, "mayor", "rig"), filepath.Join(rigPath, "refinery", "rig")} {
		if hasGitEntry(p) {
			paths = append(paths, p)
		}
	}
	paths = append(paths, repoChildren(filepath.Join(rigPath, "crew"))...)
	for _, pc := range subdirs(filepath.Join(rigPath, "polecats")) {
		if hasGitEntry(pc) {
			paths = append(paths, pc)
			continue
		}
		paths = append(paths, repoChildren(pc)...)
	}
	return paths
}

func hasGitEntry(dir string) bool {
	_, err := os.Lstat(filepath.Join(dir, ".git"))
	return err == nil
}

func subdirs(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, filepath.Join(dir, e.Name()))
		}
	}
	sort.Strings(out)
	return out
}

func repoChildren(dir string) []string {
	var out []string
	for _, d := range subdirs(dir) {
		if hasGitEntry(d) {
			out = append(out, d)
		}
	}
	return out
}

// InspectRig returns one Status per distinct hooks directory of the rig.
func InspectRig(rigPath string) ([]Status, error) {
	return forEachRigRepo(rigPath, Inspect)
}

// EnsureRig installs the guard in every hooks directory of the rig. It keeps
// going past failures; the returned error joins them (conflicts wrap
// ErrConflict).
func EnsureRig(rigPath string) ([]Status, error) {
	return forEachRigRepo(rigPath, Install)
}

func forEachRigRepo(rigPath string, fn func(string) (Status, error)) ([]Status, error) {
	var (
		out  []Status
		errs []error
		seen = map[string]bool{}
	)
	for _, repo := range RigRepoPaths(rigPath) {
		l, err := resolveLayout(repo)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		key := repo
		if l.hooksDir != "" {
			key = l.hooksDir
			if resolved, err := filepath.EvalSymlinks(l.hooksDir); err == nil {
				key = resolved
			}
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		st, err := fn(repo)
		out = append(out, st)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", repo, err))
		}
	}
	return out, errors.Join(errs...)
}
