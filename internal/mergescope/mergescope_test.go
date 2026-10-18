package mergescope

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	gitpkg "github.com/steveyegge/gastown/internal/git"
)

func runGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func writeRepoFile(t *testing.T, repo, rel, content string) {
	t.Helper()
	full := filepath.Join(repo, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeRigSettings writes <rigPath>/settings/config.json with the given
// merge_queue section (nil writes settings without merge_queue).
func writeRigSettings(t *testing.T, rigPath string, mq map[string]any) {
	t.Helper()
	settings := map[string]any{"type": "rig-settings", "version": 1}
	if mq != nil {
		settings["merge_queue"] = mq
	}
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(rigPath, "settings"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rigPath, "settings", "config.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// initRepo builds origin (bare) + clone with main pushed and a feature branch
// "polecat/nux/gt-1" that changes featurePaths. Returns the clone path.
func initRepo(t *testing.T, featurePaths ...string) string {
	t.Helper()
	remote := t.TempDir()
	runGit(t, remote, "init", "--bare")
	repo := t.TempDir()
	runGit(t, repo, "init")
	runGit(t, repo, "config", "user.email", "test@example.com")
	runGit(t, repo, "config", "user.name", "Test User")
	runGit(t, repo, "remote", "add", "origin", remote)
	writeRepoFile(t, repo, "apps/mobile/main.dart", "v1\n")
	writeRepoFile(t, repo, "docs/guide.md", "guide\n")
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "main")
	runGit(t, repo, "branch", "-M", "main")
	runGit(t, repo, "push", "-u", "origin", "main")

	runGit(t, repo, "checkout", "-b", "polecat/nux/gt-1")
	for _, p := range featurePaths {
		writeRepoFile(t, repo, p, "changed\n")
	}
	runGit(t, repo, "add", "-A")
	runGit(t, repo, "commit", "-m", "feature")
	return repo
}

func TestLoad(t *testing.T) {
	t.Run("missing settings file is unrestricted", func(t *testing.T) {
		got, err := Load(t.TempDir())
		if err != nil || got != nil {
			t.Fatalf("Load() = %q, %v; want nil, nil", got, err)
		}
	})
	t.Run("settings without merge_queue is unrestricted", func(t *testing.T) {
		rig := t.TempDir()
		writeRigSettings(t, rig, nil)
		got, err := Load(rig)
		if err != nil || got != nil {
			t.Fatalf("Load() = %q, %v; want nil, nil", got, err)
		}
	})
	t.Run("configured allowed_paths are returned", func(t *testing.T) {
		rig := t.TempDir()
		writeRigSettings(t, rig, map[string]any{"allowed_paths": []string{"apps/**"}})
		got, err := Load(rig)
		if err != nil || !reflect.DeepEqual(got, []string{"apps/**"}) {
			t.Fatalf("Load() = %q, %v; want [apps/**], nil", got, err)
		}
	})
	t.Run("unreadable settings fail closed", func(t *testing.T) {
		rig := t.TempDir()
		if err := os.MkdirAll(filepath.Join(rig, "settings"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(rig, "settings", "config.json"), []byte("{not json"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(rig); err == nil {
			t.Fatal("Load() on corrupt settings = nil error, want fail-closed error")
		}
	})
	t.Run("invalid pattern fails closed", func(t *testing.T) {
		rig := t.TempDir()
		writeRigSettings(t, rig, map[string]any{"allowed_paths": []string{"/abs/**"}})
		if _, err := Load(rig); err == nil {
			t.Fatal("Load() with an absolute pattern = nil error, want fail-closed error")
		}
	})
	// Regression (PR #38 review, HIGH): a rig without allowed_paths whose
	// settings carry an unrelated invalid field must not block submission.
	t.Run("unrelated invalid settings are ignored", func(t *testing.T) {
		rig := t.TempDir()
		writeRigSettings(t, rig, map[string]any{"poll_interval": "bogus"})
		got, err := Load(rig)
		if err != nil || got != nil {
			t.Fatalf("Load() = %q, %v; want nil, nil (unrestricted)", got, err)
		}
	})
}

func TestCheck(t *testing.T) {
	allowed := []string{"apps/**", "contracts/openapi.yaml"}

	t.Run("in-scope diff passes", func(t *testing.T) {
		repo := initRepo(t, "apps/mobile/main.dart", "contracts/openapi.yaml")
		n, err := Check(allowed, "main", MergeBase, "HEAD", gitpkg.NewGit(repo).DiffNameOnly)
		if err != nil {
			t.Fatalf("Check() = %v, want nil", err)
		}
		if n != 2 {
			t.Fatalf("Check() checked %d paths, want 2", n)
		}
	})

	t.Run("out-of-scope diff is rejected with offending paths", func(t *testing.T) {
		repo := initRepo(t, ".brief-reviews/kp-4az-r1/mobile-consumer.md", "apps/mobile/main.dart")
		_, err := Check(allowed, "main", MergeBase, "HEAD", gitpkg.NewGit(repo).DiffNameOnly)
		var scopeErr *Error
		if !errors.As(err, &scopeErr) {
			t.Fatalf("Check() = %v, want *Error", err)
		}
		if !reflect.DeepEqual(scopeErr.Paths, []string{".brief-reviews/kp-4az-r1/mobile-consumer.md"}) {
			t.Fatalf("violations = %q", scopeErr.Paths)
		}
		if !strings.Contains(err.Error(), ".brief-reviews/kp-4az-r1/mobile-consumer.md") || !strings.Contains(err.Error(), "allowed_paths") {
			t.Fatalf("error message %q does not name the offending path and config key", err)
		}
		if !strings.Contains(err.Error(), "main...HEAD") {
			t.Fatalf("error message %q does not name the merge-base range", err)
		}
	})

	t.Run("rename into scope still reports the removed source path", func(t *testing.T) {
		repo := initRepo(t, "apps/mobile/extra.dart")
		runGit(t, repo, "mv", "docs/guide.md", "apps/guide.md")
		runGit(t, repo, "commit", "-m", "move in")
		_, err := Check(allowed, "main", MergeBase, "HEAD", gitpkg.NewGit(repo).DiffNameOnly)
		var scopeErr *Error
		if !errors.As(err, &scopeErr) || !reflect.DeepEqual(scopeErr.Paths, []string{"docs/guide.md"}) {
			t.Fatalf("Check() = %v, want violation [docs/guide.md]", err)
		}
	})

	t.Run("leading-space directory is not confused with apps/", func(t *testing.T) {
		repo := initRepo(t, " apps/evil.txt")
		_, err := Check(allowed, "main", MergeBase, "HEAD", gitpkg.NewGit(repo).DiffNameOnly)
		var scopeErr *Error
		if !errors.As(err, &scopeErr) || !reflect.DeepEqual(scopeErr.Paths, []string{" apps/evil.txt"}) {
			t.Fatalf("Check() = %v, want violation [\" apps/evil.txt\"]", err)
		}
	})

	t.Run("two-dot range is named in the error", func(t *testing.T) {
		repo := initRepo(t, "docs/other.md")
		base := runGit(t, repo, "rev-parse", "main")
		_, err := Check(allowed, base, TwoDot, "HEAD", gitpkg.NewGit(repo).DiffNameOnlyRange)
		if err == nil || !strings.Contains(err.Error(), base+"..HEAD") || strings.Contains(err.Error(), base+"...HEAD") {
			t.Fatalf("Check(TwoDot) = %v, want violation naming %s..HEAD", err, base)
		}
	})

	t.Run("empty allowed list is unrestricted and does not run the diff", func(t *testing.T) {
		called := false
		diff := func(string, string) ([]string, error) { called = true; return nil, nil }
		if _, err := Check(nil, "origin/main", MergeBase, "HEAD", diff); err != nil || called {
			t.Fatalf("Check(nil allowed) = %v (diff called=%v), want nil without diff", err, called)
		}
	})

	t.Run("diff failure fails closed", func(t *testing.T) {
		if _, err := Check(allowed, "origin/main", MergeBase, "HEAD", gitpkg.NewGit(t.TempDir()).DiffNameOnly); err == nil {
			t.Fatal("Check() with broken git = nil, want error")
		}
	})
}

func TestLoadThenCheck(t *testing.T) {
	allowed := []string{"apps/**", "contracts/openapi.yaml"}
	check := func(rig, repo string) error {
		paths, err := Load(rig)
		if err != nil {
			return err
		}
		_, err = Check(paths, "main", MergeBase, "HEAD", gitpkg.NewGit(repo).DiffNameOnly)
		return err
	}
	t.Run("rejects out-of-scope branch", func(t *testing.T) {
		rig := t.TempDir()
		writeRigSettings(t, rig, map[string]any{"allowed_paths": allowed})
		var scopeErr *Error
		if err := check(rig, initRepo(t, ".brief-reviews/x.md")); !errors.As(err, &scopeErr) {
			t.Fatalf("check = %v, want *Error", err)
		}
	})
	t.Run("accepts in-scope branch", func(t *testing.T) {
		rig := t.TempDir()
		writeRigSettings(t, rig, map[string]any{"allowed_paths": allowed})
		if err := check(rig, initRepo(t, "contracts/openapi.yaml")); err != nil {
			t.Fatalf("check = %v, want nil", err)
		}
	})
	t.Run("rig without allowed_paths is unrestricted", func(t *testing.T) {
		rig := t.TempDir()
		writeRigSettings(t, rig, map[string]any{"run_tests": true})
		if err := check(rig, initRepo(t, ".brief-reviews/x.md")); err != nil {
			t.Fatalf("check = %v, want nil (unrestricted)", err)
		}
	})
}

func TestErrorTruncatesLongLists(t *testing.T) {
	paths := make([]string, 0, MaxListed+5)
	for i := 0; i < MaxListed+5; i++ {
		paths = append(paths, filepath.ToSlash(filepath.Join("out", string(rune('a'+i%26)), "f")))
	}
	msg := (&Error{Paths: paths, Base: "origin/main", Head: "HEAD"}).Error()
	if !strings.Contains(msg, "and 5 more") {
		t.Fatalf("message %q does not summarise the remainder", msg)
	}
	if !strings.Contains(msg, "origin/main...HEAD") {
		t.Fatalf("message %q: an unset Range must read as the merge-base range", msg)
	}
}
