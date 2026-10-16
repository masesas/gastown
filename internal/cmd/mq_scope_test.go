package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	gitpkg "github.com/steveyegge/gastown/internal/git"
	"github.com/steveyegge/gastown/internal/refinery"
)

// writeMQScopeFile writes a file (creating parent dirs) inside repo.
func writeMQScopeFile(t *testing.T, repo, rel, content string) {
	t.Helper()
	full := filepath.Join(repo, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// writeMQScopeRigSettings writes <rigPath>/settings/config.json with the
// given merge_queue section (nil writes settings without merge_queue).
func writeMQScopeRigSettings(t *testing.T, rigPath string, mq map[string]any) {
	t.Helper()
	settings := map[string]any{"type": "rig-settings", "version": 1}
	if mq != nil {
		settings["merge_queue"] = mq
	}
	data, err := json.Marshal(settings)
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(rigPath, "settings")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// initMQScopeRepo builds origin (bare) + clone with main pushed and a
// feature branch "polecat/nux/gt-1" that changes the given paths and is
// pushed too. Returns the clone path.
func initMQScopeRepo(t *testing.T, featurePaths ...string) string {
	t.Helper()
	remote := t.TempDir()
	runGitForMQSubmitTest(t, remote, "init", "--bare")
	repo := t.TempDir()
	runGitForMQSubmitTest(t, repo, "init")
	runGitForMQSubmitTest(t, repo, "config", "user.email", "test@example.com")
	runGitForMQSubmitTest(t, repo, "config", "user.name", "Test User")
	runGitForMQSubmitTest(t, repo, "remote", "add", "origin", remote)
	writeMQScopeFile(t, repo, "apps/mobile/main.dart", "v1\n")
	writeMQScopeFile(t, repo, "docs/guide.md", "guide\n")
	runGitForMQSubmitTest(t, repo, "add", "-A")
	runGitForMQSubmitTest(t, repo, "commit", "-m", "main")
	runGitForMQSubmitTest(t, repo, "branch", "-M", "main")
	runGitForMQSubmitTest(t, repo, "push", "-u", "origin", "main")

	runGitForMQSubmitTest(t, repo, "checkout", "-b", "polecat/nux/gt-1")
	for _, p := range featurePaths {
		writeMQScopeFile(t, repo, p, "changed\n")
	}
	runGitForMQSubmitTest(t, repo, "add", "-A")
	runGitForMQSubmitTest(t, repo, "commit", "-m", "feature")
	runGitForMQSubmitTest(t, repo, "push", "-u", "origin", "polecat/nux/gt-1")
	return repo
}

func TestLoadMergeScope(t *testing.T) {
	t.Run("missing settings file is unrestricted", func(t *testing.T) {
		got, err := loadMergeScope(t.TempDir())
		if err != nil || got != nil {
			t.Fatalf("loadMergeScope() = %q, %v; want nil, nil", got, err)
		}
	})
	t.Run("settings without merge_queue is unrestricted", func(t *testing.T) {
		rig := t.TempDir()
		writeMQScopeRigSettings(t, rig, nil)
		got, err := loadMergeScope(rig)
		if err != nil || got != nil {
			t.Fatalf("loadMergeScope() = %q, %v; want nil, nil", got, err)
		}
	})
	t.Run("configured allowed_paths are returned", func(t *testing.T) {
		rig := t.TempDir()
		writeMQScopeRigSettings(t, rig, map[string]any{"allowed_paths": []string{"apps/**"}})
		got, err := loadMergeScope(rig)
		if err != nil || !reflect.DeepEqual(got, []string{"apps/**"}) {
			t.Fatalf("loadMergeScope() = %q, %v; want [apps/**], nil", got, err)
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
		if _, err := loadMergeScope(rig); err == nil {
			t.Fatal("loadMergeScope() on corrupt settings = nil error, want fail-closed error")
		}
	})
}

func TestCheckMergeScope(t *testing.T) {
	allowed := []string{"apps/**", "contracts/openapi.yaml"}

	t.Run("in-scope diff passes", func(t *testing.T) {
		repo := initMQScopeRepo(t, "apps/mobile/main.dart", "contracts/openapi.yaml")
		g := gitpkg.NewGit(repo)
		n, err := checkMergeScope(g, allowed, "origin/main", "HEAD")
		if err != nil {
			t.Fatalf("checkMergeScope() = %v, want nil", err)
		}
		if n != 2 {
			t.Fatalf("checkMergeScope() checked %d paths, want 2", n)
		}
	})

	t.Run("out-of-scope diff is rejected with offending paths", func(t *testing.T) {
		repo := initMQScopeRepo(t, ".brief-reviews/kp-4az-r1/mobile-consumer.md", "apps/mobile/main.dart")
		g := gitpkg.NewGit(repo)
		_, err := checkMergeScope(g, allowed, "origin/main", "HEAD")
		var scopeErr *MergeScopeError
		if !errors.As(err, &scopeErr) {
			t.Fatalf("checkMergeScope() = %v, want *MergeScopeError", err)
		}
		if !reflect.DeepEqual(scopeErr.Paths, []string{".brief-reviews/kp-4az-r1/mobile-consumer.md"}) {
			t.Fatalf("violations = %q", scopeErr.Paths)
		}
		if !strings.Contains(err.Error(), ".brief-reviews/kp-4az-r1/mobile-consumer.md") {
			t.Fatalf("error message %q does not name the offending path", err)
		}
		if !strings.Contains(err.Error(), "allowed_paths") {
			t.Fatalf("error message %q does not name the config key", err)
		}
	})

	t.Run("rename into scope still reports the removed source path", func(t *testing.T) {
		repo := initMQScopeRepo(t, "apps/mobile/extra.dart")
		runGitForMQSubmitTest(t, repo, "mv", "docs/guide.md", "apps/guide.md")
		runGitForMQSubmitTest(t, repo, "commit", "-m", "move in")
		g := gitpkg.NewGit(repo)
		_, err := checkMergeScope(g, allowed, "origin/main", "HEAD")
		var scopeErr *MergeScopeError
		if !errors.As(err, &scopeErr) || !reflect.DeepEqual(scopeErr.Paths, []string{"docs/guide.md"}) {
			t.Fatalf("checkMergeScope() = %v, want violation [docs/guide.md]", err)
		}
	})

	t.Run("leading-space directory is not confused with apps/", func(t *testing.T) {
		repo := initMQScopeRepo(t, " apps/evil.txt")
		g := gitpkg.NewGit(repo)
		_, err := checkMergeScope(g, allowed, "origin/main", "HEAD")
		var scopeErr *MergeScopeError
		if !errors.As(err, &scopeErr) || !reflect.DeepEqual(scopeErr.Paths, []string{" apps/evil.txt"}) {
			t.Fatalf("checkMergeScope() = %v, want violation [\" apps/evil.txt\"]", err)
		}
	})

	t.Run("empty allowed list is unrestricted and does not touch git", func(t *testing.T) {
		g := gitpkg.NewGit(t.TempDir()) // not a repo: any git call would fail
		if _, err := checkMergeScope(g, nil, "origin/main", "HEAD"); err != nil {
			t.Fatalf("checkMergeScope(nil allowed) = %v, want nil", err)
		}
	})

	t.Run("diff failure fails closed", func(t *testing.T) {
		g := gitpkg.NewGit(t.TempDir())
		if _, err := checkMergeScope(g, allowed, "origin/main", "HEAD"); err == nil {
			t.Fatal("checkMergeScope() with broken git = nil, want error")
		}
	})
}

func TestMergeScopeErrorTruncatesLongLists(t *testing.T) {
	paths := make([]string, 0, mergeScopeMaxListed+5)
	for i := 0; i < mergeScopeMaxListed+5; i++ {
		paths = append(paths, filepath.ToSlash(filepath.Join("out", string(rune('a'+i%26)), "f")))
	}
	msg := (&MergeScopeError{Paths: paths, Base: "origin/main", Head: "HEAD"}).Error()
	if !strings.Contains(msg, "and 5 more") {
		t.Fatalf("message %q does not summarise the remainder", msg)
	}
}

type fakeMQScopeManager struct {
	mr  *refinery.MergeRequest
	err error
}

func (f fakeMQScopeManager) FindMRForPostMerge(string) (*refinery.MergeRequest, error) {
	return f.mr, f.err
}

func TestRunMQCheckScopeFor(t *testing.T) {
	allowed := []string{"apps/**", "contracts/openapi.yaml"}
	mr := &refinery.MergeRequest{ID: "gt-mr-1", Branch: "polecat/nux/gt-1", TargetBranch: "main"}

	t.Run("in-scope MR passes", func(t *testing.T) {
		repo := initMQScopeRepo(t, "apps/mobile/main.dart")
		rig := t.TempDir()
		writeMQScopeRigSettings(t, rig, map[string]any{"allowed_paths": allowed})
		out, err := runMQCheckScopeFor(fakeMQScopeManager{mr: mr}, rig, gitpkg.NewGit(repo), "gt-mr-1")
		if err != nil {
			t.Fatalf("runMQCheckScopeFor() = %v", err)
		}
		if !strings.Contains(out, "within allowed_paths") {
			t.Fatalf("output %q missing pass summary", out)
		}
	})

	t.Run("branch re-pushed out of scope after submit is rejected", func(t *testing.T) {
		repo := initMQScopeRepo(t, "apps/mobile/main.dart")
		// Re-push after submit: add an out-of-scope file on the remote branch,
		// then rewind the local tracking ref so only a fetch reveals it.
		before := runGitForMQSubmitTest(t, repo, "rev-parse", "origin/polecat/nux/gt-1")
		writeMQScopeFile(t, repo, ".brief-reviews/x.md", "review\n")
		runGitForMQSubmitTest(t, repo, "add", "-A")
		runGitForMQSubmitTest(t, repo, "commit", "-m", "review notes")
		runGitForMQSubmitTest(t, repo, "push", "origin", "polecat/nux/gt-1")
		runGitForMQSubmitTest(t, repo, "update-ref", "refs/remotes/origin/polecat/nux/gt-1", before)

		rig := t.TempDir()
		writeMQScopeRigSettings(t, rig, map[string]any{"allowed_paths": allowed})
		_, err := runMQCheckScopeFor(fakeMQScopeManager{mr: mr}, rig, gitpkg.NewGit(repo), "gt-mr-1")
		var scopeErr *MergeScopeError
		if !errors.As(err, &scopeErr) || !reflect.DeepEqual(scopeErr.Paths, []string{".brief-reviews/x.md"}) {
			t.Fatalf("runMQCheckScopeFor() = %v, want violation [.brief-reviews/x.md]", err)
		}
	})

	t.Run("unrestricted rig passes without git", func(t *testing.T) {
		rig := t.TempDir()
		out, err := runMQCheckScopeFor(fakeMQScopeManager{mr: mr}, rig, gitpkg.NewGit(t.TempDir()), "gt-mr-1")
		if err != nil {
			t.Fatalf("runMQCheckScopeFor() = %v", err)
		}
		if !strings.Contains(out, "unrestricted") {
			t.Fatalf("output %q missing unrestricted notice", out)
		}
	})

	t.Run("lookup failure fails closed", func(t *testing.T) {
		rig := t.TempDir()
		writeMQScopeRigSettings(t, rig, map[string]any{"allowed_paths": allowed})
		_, err := runMQCheckScopeFor(fakeMQScopeManager{err: refinery.ErrMRNotFound}, rig, gitpkg.NewGit(t.TempDir()), "gt-mr-x")
		if err == nil {
			t.Fatal("runMQCheckScopeFor() with missing MR = nil, want error")
		}
	})

	t.Run("MR without branch or target fails closed", func(t *testing.T) {
		rig := t.TempDir()
		writeMQScopeRigSettings(t, rig, map[string]any{"allowed_paths": allowed})
		for _, bad := range []*refinery.MergeRequest{
			{ID: "gt-mr-2", TargetBranch: "main"},
			{ID: "gt-mr-3", Branch: "polecat/nux/gt-1"},
		} {
			if _, err := runMQCheckScopeFor(fakeMQScopeManager{mr: bad}, rig, gitpkg.NewGit(t.TempDir()), bad.ID); err == nil {
				t.Fatalf("runMQCheckScopeFor(%+v) = nil, want error", bad)
			}
		}
	})

	t.Run("fetch failure fails closed", func(t *testing.T) {
		rig := t.TempDir()
		writeMQScopeRigSettings(t, rig, map[string]any{"allowed_paths": allowed})
		if _, err := runMQCheckScopeFor(fakeMQScopeManager{mr: mr}, rig, gitpkg.NewGit(t.TempDir()), "gt-mr-1"); err == nil {
			t.Fatal("runMQCheckScopeFor() without a repo = nil, want error")
		}
	})
}

func TestEnforceMergeScope(t *testing.T) {
	allowed := []string{"apps/**", "contracts/openapi.yaml"}

	t.Run("rejects out-of-scope branch before MR creation", func(t *testing.T) {
		repo := initMQScopeRepo(t, ".brief-reviews/kp-4az-r1/mobile-consumer.md")
		rig := t.TempDir()
		writeMQScopeRigSettings(t, rig, map[string]any{"allowed_paths": allowed})
		head := runGitForMQSubmitTest(t, repo, "rev-parse", "HEAD")
		err := enforceMergeScope(rig, gitpkg.NewGit(repo), "origin/main", head)
		var scopeErr *MergeScopeError
		if !errors.As(err, &scopeErr) {
			t.Fatalf("enforceMergeScope() = %v, want *MergeScopeError", err)
		}
	})

	t.Run("accepts in-scope branch", func(t *testing.T) {
		repo := initMQScopeRepo(t, "contracts/openapi.yaml")
		rig := t.TempDir()
		writeMQScopeRigSettings(t, rig, map[string]any{"allowed_paths": allowed})
		if err := enforceMergeScope(rig, gitpkg.NewGit(repo), "origin/main", "HEAD"); err != nil {
			t.Fatalf("enforceMergeScope() = %v, want nil", err)
		}
	})

	t.Run("rig without allowed_paths keeps old behaviour", func(t *testing.T) {
		repo := initMQScopeRepo(t, ".brief-reviews/x.md")
		rig := t.TempDir()
		writeMQScopeRigSettings(t, rig, map[string]any{"run_tests": true})
		if err := enforceMergeScope(rig, gitpkg.NewGit(repo), "origin/main", "HEAD"); err != nil {
			t.Fatalf("enforceMergeScope() = %v, want nil (unrestricted)", err)
		}
	})

	t.Run("gt mq submit gate checks the submitted commit", func(t *testing.T) {
		repo := initMQScopeRepo(t, ".brief-reviews/kp-4az-r1/mobile-consumer.md")
		rig := t.TempDir()
		writeMQScopeRigSettings(t, rig, map[string]any{"allowed_paths": allowed})
		head := runGitForMQSubmitTest(t, repo, "rev-parse", "HEAD")
		runGitForMQSubmitTest(t, repo, "checkout", "main") // --branch submit: HEAD differs
		g := gitpkg.NewGit(repo)
		err := enforceSubmitMergeScope(rig, g, "main", "main", "polecat/nux/gt-1", head)
		var scopeErr *MergeScopeError
		if !errors.As(err, &scopeErr) {
			t.Fatalf("enforceSubmitMergeScope(sha) = %v, want *MergeScopeError", err)
		}
		err = enforceSubmitMergeScope(rig, g, "main", "main", "polecat/nux/gt-1", "")
		if !errors.As(err, &scopeErr) {
			t.Fatalf("enforceSubmitMergeScope(branch fallback) = %v, want *MergeScopeError", err)
		}
	})

	t.Run("gt mq submit gate is a no-op without allowed_paths", func(t *testing.T) {
		rig := t.TempDir()
		g := gitpkg.NewGit(t.TempDir())
		if err := enforceSubmitMergeScope(rig, g, "main", "main", "b", ""); err != nil {
			t.Fatalf("enforceSubmitMergeScope() = %v, want nil", err)
		}
	})
}
